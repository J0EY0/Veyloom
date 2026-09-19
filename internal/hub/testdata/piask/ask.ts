// An extension for the pi smoke test: its tool asks the person to confirm,
// to pick, to write a line and to edit some text, through pi's extension
// UI, then tells them it did, and returns their answers, so the test can see an extension's dialogs
// reach a person and the answers come back.
import { Type } from "@mariozechner/pi-ai";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";

export default function (pi: ExtensionAPI) {
	pi.registerTool({
		name: "ask_person",
		label: "ask_person",
		description: "Asks the person four things and returns their answers. Call it when you are told to.",
		parameters: Type.Object({}),
		async execute(_toolCallId: string, _params: unknown, _signal: AbortSignal | undefined, _onUpdate: unknown, ctx: any) {
			const confirmed = await ctx.ui.confirm("Deploy", "Deploy to staging now?");
			const environment = await ctx.ui.select("Which environment?", ["staging", "production"]);
			const name = await ctx.ui.input("Release name?", "v1.2.3");
			const notes = await ctx.ui.editor("Release notes", "- fixed things\n");
			ctx.ui.notify("Asked the person four things", "info");
			return { content: [{ type: "text", text: JSON.stringify({ confirmed, environment, name, notes }) }], details: {} };
		},
	});
}
