# Veyloom

[English](README.md) | [简体中文](README_ZH.md)

Run your coding agents (Claude Code, Codex, Pi and more) as one team on your projects.

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8.svg)

## Features

- **One chat per project.** Add agents as members, @ them to hand out work; each task gets its own topic, and agents hand work to one another within limits you set.
- **Your agents, as they are.** Veyloom drives the CLIs you already use. Their sandboxes, approvals, plugins, MCP servers and context compaction stay theirs; anything that needs you shows up as a card in the chat.
- **Shared memory.** A project wiki (plain Markdown under git, [OKF](https://github.com/GoogleCloudPlatform/open-knowledge-format) v0.2) that agents search, read and write, kept tidy by a maintainer agent, plus project and personal memory.
- **Skills that improve.** A skill library shared across projects, installed per agent, which agents refine while they work, with a trial period to fall back on.
- **Real parallel work.** A leader sets the project up; every other member works in a git worktree of its own and you merge from the UI. Task board, usage page and attachment previews included.
- **Built for long runs.** Sessions resume across turns, you can steer a running turn, agents set reminders, quota and sign-in failures pause instead of failing over and over, and stuck turns are flagged.

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
