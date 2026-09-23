package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Pi has no MCP. It has extensions: a TypeScript file named on the command
// line (-e) may register tools of its own, and the --tools whitelist covers
// those like any built-in. Veyloom's tools reach a Pi turn that way: the
// extension below registers them and posts each call to the plain JSON
// route of the turn's endpoint, whose URL it finds in its environment.
//
// The file is generated from agentToolSpecs, the same list the MCP tools
// are made from, so the two cannot drift apart: each tool's parameters are
// the same JSON Schema, which pi validates plain JSON Schema against as it
// does its own TypeBox schemas. It holds nothing about any one turn and is
// written once, named after its contents.

// piRoomURLEnv names the environment variable the extension reads.
const piRoomURLEnv = "VEYLOOM_ROOM_URL"

// piExtraToolsEnv names the optional tools the turn gets, comma-separated:
// the extension registers those of its optional tools and no others.
const piExtraToolsEnv = "VEYLOOM_EXTRA_TOOLS"

// piExtensionSource is the extension's text.
func piExtensionSource() string {
	type tool struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
		// Optional tools are registered for the turns that name them.
		Optional bool `json:"optional,omitempty"`
	}
	tools := make([]tool, 0, len(agentToolSpecs)+len(optionalToolSpecs))
	for _, spec := range agentToolSpecs {
		tools = append(tools, tool{Name: spec.Name, Description: spec.Description, Parameters: spec.inputSchema()})
	}
	for _, spec := range optionalToolSpecs {
		tools = append(tools, tool{Name: spec.Name, Description: spec.Description, Parameters: spec.inputSchema(), Optional: true})
	}
	list, _ := json.MarshalIndent(tools, "", "\t")
	return fmt.Sprintf(`// Written by Veyloom; do not edit, it is replaced when it changes.
// Gives a Pi turn Veyloom's tools: the project's chat and its wiki.
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

const tools = %s;

export default function (pi: ExtensionAPI) {
	const url = process.env.%s;
	if (!url) return; // not a Veyloom turn
	const extra = (process.env.%s ?? "").split(",");
	for (const tool of tools) {
		if ((tool as { optional?: boolean }).optional && !extra.includes(tool.name)) continue;
		pi.registerTool({
			name: tool.name,
			label: tool.name,
			description: tool.description,
			parameters: tool.parameters as any,
			async execute(_toolCallId: string, params: unknown, signal?: AbortSignal) {
				let text: string;
				try {
					const res = await fetch(url, {
						method: "POST",
						headers: { "content-type": "application/json" },
						body: JSON.stringify({ tool: tool.name, args: params ?? {} }),
						signal,
					});
					text = res.ok ? (await res.json()).text : "error: " + res.status + " " + (await res.text());
				} catch (err) {
					text = "error: " + String(err);
				}
				return { content: [{ type: "text", text }], details: {} };
			},
		});
	}
}
`, list, piRoomURLEnv, piExtraToolsEnv)
}

// piExtensionFile is where the extension lives under dir: named after its
// contents, so a new version never overwrites one a running pi has loaded.
func piExtensionFile(dir string) string {
	sum := sha256.Sum256([]byte(piExtensionSource()))
	return filepath.Join(dir, "veyloom-room-"+hex.EncodeToString(sum[:6])+".ts")
}

// ensurePiExtension writes the extension under dir unless it is there.
func ensurePiExtension(dir string) (string, error) {
	path := piExtensionFile(dir)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// Written whole, then moved into place: a pi starting at the same
	// moment never reads half a file.
	tmp, err := os.CreateTemp(dir, ".veyloom-room-*.ts")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(piExtensionSource()); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
