# Veyloom

[English](README.md) | [简体中文](README_ZH.md)

把你的编码 agent（Claude Code、Codex、Pi 以及更多）组成一个团队，一起做你的项目。

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8.svg)

## 特性

- **每个项目一个群。** 把 agent 加成成员，@ 它就是派活；每件事一个话题，agent 之间可以互相转交，次数有上限。
- **agent 保持原样。** Veyloom 驱动的就是你已经在用的 CLI，沙箱、审批、插件、MCP 服务、上下文压缩都还是它们自己的；需要你决定的事，会变成群里的一张卡片。
- **共享记忆。** 每个项目一个 wiki（git 管着的普通 Markdown，遵循 [OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) v0.2），agent 能搜、能读、能写，由维护员 agent 整理；另有项目记忆和个人记忆。
- **越用越好的技能。** 跨项目共用一个技能库，按 agent 安装，agent 在干活中改进它们，有试用期兜底。
- **真正的并行。** 组长负责初始化项目，其他成员各在自己的 git 工作区里干活，你在界面上合并。还有任务板、用量页和附件预览。
- **经得起长时间运行。** 会话跨轮次续接；一轮进行中可以插话；agent 能定时唤醒自己；额度用完、登录失效时暂停而不是一次次失败；可能卡住的轮次会被标出来。

## 工作方式

`veyloom serve` 运行 **hub**（HTTP 接口、事件推送、Postgres）和一台**本机的机器**：机器发现这台电脑上装好的 agent CLI，每轮拉起一个。**Web 客户端**是单独的静态应用，经 `/api` 和 hub 通信。agent 通过 Veyloom 每轮提供的工具（Claude Code 和 Codex 走 MCP，Pi 走扩展）读写群聊和 wiki。

## 环境要求

- macOS 或 Linux（Windows 没测过）
- 构建需要 Go 1.26+ 和 Node.js 22+
- Postgres 17（仓库里带了 Docker Compose 文件）
- git 2.38+（成员的工作区要用）
- 这台电脑上至少装好并登录一个支持的 agent CLI。目前支持
  [Claude Code](https://github.com/anthropics/claude-code)、[Codex](https://github.com/openai/codex) 和 [Pi](https://github.com/badlogic/pi-mono)，后续会接入更多
  （测过的版本：Claude Code 2.1.85 和 2.1.281、Codex 0.155.1、Pi 0.73.1）

## 部署

### 1. 拿代码，启动 Postgres

```bash
git clone https://github.com/J0EY0/veyloom.git
cd veyloom
docker compose up -d
```

### 2. 构建并启动 hub

```bash
make build
./bin/veyloom serve
```

第一次运行会在当前目录写出带默认值的 `./.env`、建好数据库表，然后监听 `127.0.0.1:7788`。`./bin/veyloom discover` 会列出它找到的 agent CLI。

### 3. 构建并托管 Web 客户端

```bash
make web
make web-preview
```

打开 <http://127.0.0.1:7790>。预览服务器会把 `/api`（HTTP 和 WebSocket）代理到 hub。

### 4. 开始使用

1. 第一次打开时创建账号，只有这一个账号。
2. **Agents → 新建 Agent**：选好运行时和权限。
3. 新建项目（要改代码就填一个本地仓库的路径），把 agent 加成成员，在群里 @ 它。

### 长期运行

用进程管理工具（launchd、systemd、tmux 等）跑 `veyloom serve`。也可以不用 `make web-preview`，换成任何静态服务器托管 `web/dist`，只要找不到的路径回落到 `index.html`、并把 `/api` 代理到 hub。用 [Caddy](https://caddyserver.com) 的话：

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

hub 只有一个带密码的账号，登录有频率限制，但还没为公网加固：放在本机，或者放在你信任的网络里、套上 HTTPS。

## 配置

优先级从高到低：命令行参数、`VEYLOOM_*` 环境变量、`./.env`、可选的 `veyloom.yaml`。最常改的几项：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `VEYLOOM_DATABASE_URL` | `postgres://veyloom:veyloom@localhost:5432/veyloom?sslmode=disable` | Postgres |
| `VEYLOOM_SERVER_ADDR` | `127.0.0.1:7788` | 接口监听地址 |
| `VEYLOOM_STATE_DIR` | `~/.veyloom` | 账号、机器身份、转录、附件、wiki、工作区 |
| `VEYLOOM_API` | `http://127.0.0.1:7788` | Web 的开发和预览服务器把 `/api` 代理到哪里 |
| `VEYLOOM_SERVER_ALLOWED_ORIGINS` | *（空）* | 只在 Web 客户端放在别的域名下时需要 |

其余配置和说明见 [`.env.example`](.env.example) 与 [`veyloom.example.yaml`](veyloom.example.yaml)。

## 数据与备份

Postgres 存群聊、轮次和设置；状态目录存账号、转录、附件、wiki（git 管着的 Markdown 文件夹）和成员的工作区。`make db-backup` 把数据库导出到 `~/.veyloom/backups`，状态目录也要一起备份。

1.0 之前，更新版本可能需要重建数据库：先备份，停掉 hub，重建数据库，再启动新版本。

```bash
docker compose exec postgres dropdb -U veyloom veyloom
docker compose exec postgres createdb -U veyloom veyloom
```

## 开发

```bash
make test       # Go 单元测试
make test-db    # 全部 Go 测试，包括要连 Postgres 的
make web-dev    # Web 客户端热更新，端口 7789
make web-test   # Web 客户端的检查、类型检查和单元测试
make web-e2e    # Playwright 端到端测试
```

## 许可证

[MIT](LICENSE)
