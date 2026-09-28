# Veyloom

[English](README.md) | [简体中文](README_ZH.md)

Run your coding agents (Claude Code, Codex, Pi and more) as one team on your projects.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8.svg)

![Acme Notes in Veyloom: Lead hands the work to Coder and Tester, then sums it up](.github/assets/readme/en/chat.webp)

Veyloom puts the coding agents you already use into one group chat per project. You talk to them as you would to a team: @ one to hand it work, or say what you need and the project's leader splits it up. Each agent keeps its own session, and all of them read and write one wiki, so what one agent learns, the next one knows.

## Highlights

### A team of agents

- Add Claude Code, Codex and Pi to a project as members, each with a role of its own.
- @ a member to hand it work. Say something to no one and the project's leader takes it on or hands it on; the box you type in tells you who will get it.
- Members hand work to one another in the topic, within limits you set, and when they are done the member you asked sums it up for you.
- The leader works in your checkout; every other member works in a git worktree of its own, and its work goes onto your main line from a card in the chat.
- The task board follows each piece of work across members and topics.

![The task board](.github/assets/readme/en/tasks.webp)

### An LLM wiki for every project

- Each project has a wiki that the agents search, read and write with their tools: plain Markdown under git, following [OKF](https://github.com/GoogleCloudPlatform/open-knowledge-format) v0.2.
- Every turn brings the pages marked resident and what changed since the agent last looked; the rest it reads when it needs to.
- A maintainer agent keeps it up to date: it turns what was said and sent in the chat into pages, marks a page to check again when a file it names changes or it gets old, and replaces what went out of date.
- Pages link to one another, and a graph shows how they hang together. Every change is a commit you can revert.
- Project memory and global memory, the things every agent should know, come with every turn.

![A page of the project wiki](.github/assets/readme/en/wiki.webp)

![The wiki's relation graph](.github/assets/readme/en/wiki-graph.webp)

### A skill wiki shared across projects

- One skill library for all your projects. Each skill is a folder with a `SKILL.md` you import, and a project team looks after it.
- Install a skill on the agents that need it; Veyloom hands it to each runtime in the form that runtime loads.
- Agents improve a skill while they use it. The change goes on trial: after a few good uses it stays, and a person or the skill's maintainer can roll it back.
- Every skill keeps its history: who changed what, where and why.

![A skill on trial in the skill library](.github/assets/readme/en/skill.webp)

### And more

- **Your agents, as they are.** Veyloom drives the CLIs you already use. Their sandboxes, approvals, plugins, MCP servers and context compaction stay theirs; anything that needs you shows up as a card in the chat.
- **Built for long runs.** Sessions resume across turns, you can steer a running turn, agents set reminders, quota and sign-in failures pause instead of failing over and over, and stuck turns are flagged.
- A usage page, attachment previews, and an inbox of what needs you.

## How it works

`veyloom serve` runs the **hub** (HTTP API, event streams, Postgres) and a **local machine** that discovers the agent CLIs installed here and starts one per turn. The **web client** is a separate static app that talks to the hub through `/api`. Agents read and write the chat and the wiki through tools Veyloom hands each turn (MCP for Claude Code and Codex, an extension for Pi).

## Requirements

- macOS or Linux (Windows is untested)
- Go 1.26+ and Node.js 22+ to build
- Postgres 17 (a Docker Compose file is included)
- git 2.38+ for member worktrees
- At least one supported agent CLI installed and signed in on this machine. Supported today:
  [Claude Code](https://github.com/anthropics/claude-code), [Codex](https://github.com/openai/codex) and [Pi](https://github.com/badlogic/pi-mono), with more to come
  (tested with Claude Code 2.1.85 and 2.1.281, Codex 0.155.1, Pi 0.73.1)

## Deploy

### 1. Get the code and start Postgres

```bash
git clone https://github.com/J0EY0/Veyloom.git
cd Veyloom
docker compose up -d
```

### 2. Build and start the hub

```bash
make build
./bin/veyloom serve
```

On first run it writes `./.env` with the defaults, applies the database migrations and listens on `127.0.0.1:7788`. `./bin/veyloom discover` lists the agent CLIs it found.

### 3. Build and serve the web client

```bash
make web
make web-preview
```

Open <http://127.0.0.1:7790>. The preview server proxies `/api` (HTTP and WebSocket) to the hub.

### 4. First steps

1. Create your account on the first visit. There is exactly one.
2. **Agents → New agent**: pick a runtime and a permission preset.
3. Create a project (give it the path of a local repository for code work), add agents as members, and @ one in the chat.

### Keeping it running

Run `veyloom serve` under a process manager (launchd, systemd, tmux, …). Instead of `make web-preview`, any static server can host `web/dist` if it falls back to `index.html` and proxies `/api` to the hub. With [Caddy](https://caddyserver.com):

```caddy
:8080 {
	handle /api/* {
		reverse_proxy 127.0.0.1:7788
	}
	handle {
		root * /path/to/veyloom/web/dist
		try_files {path} /index.html
		file_server
	}
}
```

The hub has a single password-protected account and rate-limits sign-in, but it is not hardened for the open internet: keep it on localhost, or behind HTTPS on a network you trust.

## Configuration

Settings come from command-line flags, `VEYLOOM_*` environment variables, `./.env` and an optional `veyloom.yaml`, highest first. The ones you are most likely to change:

| Variable | Default | What it is |
|---|---|---|
| `VEYLOOM_DATABASE_URL` | `postgres://veyloom:veyloom@localhost:5432/veyloom?sslmode=disable` | Postgres |
| `VEYLOOM_SERVER_ADDR` | `127.0.0.1:7788` | Where the API listens |
| `VEYLOOM_STATE_DIR` | `~/.veyloom` | Account, machine identity, transcripts, attachments, wikis, worktrees |
| `VEYLOOM_API` | `http://127.0.0.1:7788` | Where the web dev and preview servers proxy `/api` |
| `VEYLOOM_SERVER_ALLOWED_ORIGINS` | *(empty)* | Only when the web client is served from another origin |

Everything else, explained: [`.env.example`](.env.example) and [`veyloom.example.yaml`](veyloom.example.yaml).

## Data and backups

Postgres holds the chats, turns and settings. The state directory holds the account, transcripts, attachments, the wikis (Markdown folders under git) and member worktrees. `make db-backup` dumps the database to `~/.veyloom/backups`; back up the state directory too.

Until 1.0, updating may mean recreating the database: back up, stop the hub, recreate it, then start the new build.

```bash
docker compose exec postgres dropdb -U veyloom veyloom
docker compose exec postgres createdb -U veyloom veyloom
```

## Development

```bash
make test       # Go unit tests
make test-db    # every Go test, including those that need Postgres
make web-dev    # web client with hot reload on :7789
make web-test   # lint, type-check and unit-test the web client
make web-e2e    # Playwright end-to-end tests
```

## License

[MIT](LICENSE)
