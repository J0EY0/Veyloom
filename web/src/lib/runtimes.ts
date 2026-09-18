// What people call each runtime. The id ("claude") is what agents, the
// runner protocol and the CLI binary use; the name is the product someone
// installed ("Claude Code"). Claude alone would be the model, not the
// tool. A runtime we do not know, such as the test runtime, shows its id.
const names: Record<string, string> = {
  claude: 'Claude Code',
  codex: 'Codex',
  pi: 'Pi',
}

export function runtimeName(runtime: string): string {
  return names[runtime] ?? runtime
}

// What to run on a machine to install a runtime, shown where a machine has
// none so the fix is one copy away.
const installCommands: Record<string, string> = {
  claude: 'npm i -g @anthropic-ai/claude-code',
  codex: 'npm i -g @openai/codex',
  pi: 'npm i -g @mariozechner/pi-coding-agent',
}

export function installCommand(runtime: string): string | undefined {
  return installCommands[runtime]
}

// The runtimes Veyloom knows how to install, for a machine that has none.
export function installableRuntimes(): string[] {
  return Object.keys(installCommands)
}

// The order runtimes are listed in when nothing else ranks them.
export function runtimeRank(runtime: string): number {
  const index = Object.keys(names).indexOf(runtime)
  return index === -1 ? Object.keys(names).length : index
}

// The fake runtime is there for tests: the end-to-end run and API-driven
// checks put agents on it. Nobody installs it or picks it, so lists of
// what a machine can run leave it out. The hub still reports and accepts
// it, and an agent that already names it keeps showing it.
export function visibleRuntimes<T extends { name: string }>(runtimes: readonly T[] | null | undefined): T[] {
  return (runtimes ?? []).filter((runtime) => runtime.name !== 'fake')
}
