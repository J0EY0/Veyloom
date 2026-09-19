package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/account"
	"github.com/J0EY0/veyloom/internal/api"
	"github.com/J0EY0/veyloom/internal/auth"
	"github.com/J0EY0/veyloom/internal/config"
	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/machine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// newServeCmd builds `veyloom serve`: a hub with one in-process machine for
// this machine, exposed over HTTP until the command's context is cancelled.
// Remote machines will connect to the same hub over the network later; the
// in-process one talks over a pipe but is otherwise identical.
func newServeCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the hub with a local machine",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, a.cfg)
		},
	}
	a.addDatabaseFlag(cmd)
	a.addStateDirFlag(cmd)
	a.addAddrFlag(cmd)
	a.addMachineNameFlag(cmd)
	a.addDetectTimeoutFlag(cmd)
	return cmd
}

func runServe(cmd *cobra.Command, cfg config.Config) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))

	s, err := openStore(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer s.Close()

	// The one account lives in the state dir; the directory lays it over
	// the users table so the hub and the API can name them by id.
	accounts, err := account.Open(cfg.AccountPath())
	if err != nil {
		return err
	}
	dir := &account.Directory{Store: s, File: accounts}

	// Turns the last stop cut off would otherwise show as running forever.
	if cut, err := s.FailRunningTurns(ctx, "the hub stopped while the turn was running"); err != nil {
		return err
	} else if len(cut) > 0 {
		logger.Warn("failed turns left running by the last stop", "count", len(cut))
	}

	h := hub.New(dir, cfg.Hub, hub.WithLogger(logger))
	discovery := machine.NewDiscovery(runtime.Builtin(), cfg.Machine.DetectTimeout)
	identity := machine.FileIdentity{Path: cfg.MachineIdentityPath()}
	// The CLIs reach a turn's tools through this very executable, run as
	// `veyloom mcp-proxy`.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate the veyloom executable for the MCP proxy: %w", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: cfg.Machine.SessionDir, ToolDir: cfg.Machine.ToolDir, ProxyBinary: self})
	local := machine.New(cfg.Machine, discovery, identity, runners)

	// Any failure on either side of the pipe stops the whole process; on
	// one machine there is nothing to fall back to.
	hubEnd, machineEnd := protocol.Pipe()
	go func() {
		if err := h.Serve(ctx, hubEnd); err != nil {
			logger.Error("hub connection failed", "err", err)
			cancel()
		}
	}()
	go func() {
		if err := local.Run(ctx, machineEnd); err != nil {
			logger.Error("local machine failed", "err", err)
			cancel()
		}
	}()

	// Sign-in over the account file; expired sessions are swept at startup.
	signIn := auth.New(accounts, auth.DefaultSessionTTL)
	if _, err := signIn.Sweep(ctx); err != nil {
		return err
	}

	// The server is the API alone. The web client is a separate process:
	// the Vite dev server or any static host, proxying /api here (or
	// calling it across origins, see server.allowed_origins).
	mux := http.NewServeMux()
	mux.Handle("/api/", api.NewHandler(api.Deps{
		Runtimes:      discovery,
		Machines:      h,
		Projects:      s,
		Users:         dir,
		Auth:          signIn,
		Messages:      s,
		Agents:        s,
		Turns:         s,
		Approvals:     s,
		Chat:          h,
		Events:        api.EventsOptions{AllowedOrigins: cfg.Server.AllowedOrigins, WriteTimeout: cfg.Server.WriteTimeout},
		TranscriptDir: cfg.Hub.TranscriptDir,
		Attachments:   s,
		AttachmentDir: cfg.Hub.AttachmentDir,
		AvatarDir:     cfg.Hub.AvatarDir,
		Logger:        logger,
	}))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("veyloom api: see /api/v1. The web client runs separately (make web-dev).\n"))
	})

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
	}
	logger.Info("listening", "url", "http://"+cfg.Server.Addr)
	return serveUntilCancelled(ctx, srv, cfg.Server)
}

// serveUntilCancelled runs srv and shuts it down gracefully when ctx is
// cancelled, giving in-flight requests up to the configured timeout.
func serveUntilCancelled(ctx context.Context, srv *http.Server, cfg config.Server) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
