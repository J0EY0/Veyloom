package runtime

import (
	"net"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolServerName is how the turn's MCP server is known to the CLIs: its
// tools appear there as mcp__veyloom__<tool>.
const toolServerName = "veyloom"

// toolEndpoint is the loopback HTTP endpoint a runner keeps for its turns:
// where the agent's CLI reaches the tools Veyloom gives it. One listener
// serves every turn; the path carries a random token that selects the turn,
// so a turn reaches its own tools and nobody else's.
//
//	/turns/{token}/mcp   the turn's MCP server (Claude Code and Codex, which
//	                     reach it through the stdio proxy, internal/mcpproxy)
//	/turns/{token}/room  the same tools as plain JSON, for runtimes without
//	                     MCP (Pi's extension)
type toolEndpoint struct {
	once     sync.Once
	startErr error

	mu    sync.Mutex
	srv   *http.Server
	base  string
	turns map[string]*turnTools // by token
}

// turnTools is what one turn is served.
type turnTools struct {
	mcp  *mcp.Server
	room http.Handler
}

func newToolEndpoint() *toolEndpoint {
	return &toolEndpoint{turns: make(map[string]*turnTools)}
}

// start binds the listener on first use, so a runner whose turns never need
// tools never opens a port.
func (e *toolEndpoint) start() error {
	e.once.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			e.startErr = err
			return
		}
		mux := http.NewServeMux()
		mux.Handle("/turns/{token}/mcp", mcp.NewStreamableHTTPHandler(e.serverFor, nil))
		mux.HandleFunc("/turns/{token}/room", func(w http.ResponseWriter, r *http.Request) {
			if t := e.lookup(r.PathValue("token")); t != nil {
				t.room.ServeHTTP(w, r)
				return
			}
			http.Error(w, "no such turn", http.StatusNotFound)
		})
		e.mu.Lock()
		e.srv = &http.Server{Handler: mux}
		e.base = "http://" + ln.Addr().String()
		e.mu.Unlock()
		go e.srv.Serve(ln)
	})
	return e.startErr
}

func (e *toolEndpoint) lookup(token string) *turnTools {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.turns[token]
}

// serverFor picks the turn named by the request path; nil makes the
// handler answer 400.
func (e *toolEndpoint) serverFor(r *http.Request) *mcp.Server {
	if t := e.lookup(r.PathValue("token")); t != nil {
		return t.mcp
	}
	return nil
}

// turnEndpoint is where one turn's tools are reached.
type turnEndpoint struct {
	token string
	// MCP and Room are the URLs of the two routes.
	MCP, Room string
}

// register gives a turn its tools: when it has a host to ask, every turn's
// and the optional ones extra names; and whatever more adds to its MCP
// server (nil for nothing).
func (e *toolEndpoint) register(host TurnHost, extra []string, more func(*mcp.Server)) (turnEndpoint, error) {
	if err := e.start(); err != nil {
		return turnEndpoint{}, err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: toolServerName, Version: "dev"}, nil)
	if host != nil {
		addRoomTools(server, host, extra)
	}
	if more != nil {
		more(server)
	}
	token := randomHex(16)
	e.mu.Lock()
	e.turns[token] = &turnTools{mcp: server, room: serveRoomTool(host, extra)}
	base := e.base + "/turns/" + token
	e.mu.Unlock()
	return turnEndpoint{token: token, MCP: base + "/mcp", Room: base + "/room"}, nil
}

// unregister forgets a turn's tools and drops the CLI's sessions on them.
func (e *toolEndpoint) unregister(token string) {
	e.mu.Lock()
	t := e.turns[token]
	delete(e.turns, token)
	e.mu.Unlock()
	if t == nil {
		return
	}
	for session := range t.mcp.Sessions() {
		_ = session.Close()
	}
}

// close stops the listener. Turns still running lose their tools.
func (e *toolEndpoint) close() error {
	e.mu.Lock()
	srv := e.srv
	e.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

// mcpProxyServer is the stdio MCP server entry that runs the proxy pointed
// at a turn's endpoint, in the shape both CLIs' configs take.
func mcpProxyServer(proxy, url string) (command string, args []string) {
	return proxy, []string{"mcp-proxy", "--url", url}
}
