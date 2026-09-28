# Veyloom

[English](README.md) | [简体中文](README_ZH.md)

把你的编码 agent（Claude Code、Codex、Pi 以及更多）组成一个团队，一起做你的项目。

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8.svg)

![Veyloom 里的 Acme 笔记项目：组长把活交给 Coder 和 Tester，最后汇总](.github/assets/readme/zh/chat.webp)

Veyloom 把你已经在用的编码 agent 拉进同一个项目群。你像带一个团队那样跟它们说话：@ 谁就是把活派给谁；不点名，就由项目的组长接下来或者分给合适的成员。每个 agent 有自己的会话，大家共用一个 wiki：一个 agent 学到的，下一个也知道。

## 亮点

### 一个 agent 团队

- 把 Claude Code、Codex、Pi 加进项目当成员，各有各的角色。
- @ 一个成员就是派活给它；不点名的话，由组长接下或转给合适的成员，输入框下面会写明这条消息会交给谁。
- 成员之间在话题里互相转交，次数由你设上限；大家都做完后，你问的那个成员会给你汇总。
- 组长在你的仓库里干活，其他成员各在自己的 git 工作区里，做完的活在群里点一下卡片就合进主线。
- 任务板按成员和话题跟踪每一件事。

![任务板](.github/assets/readme/zh/tasks.webp)

### 每个项目一个 LLM Wiki

- 每个项目都有一个 wiki，agent 用工具搜、读、写：git 管着的普通 Markdown，遵循 [OKF](https://github.com/GoogleCloudPlatform/open-knowledge-format) v0.2。
- 每一轮都带上标为常驻的页面，以及 agent 上次看过之后的变动；其余的需要时再读。
- 维护员 agent 定期整理：把群里说过、发过的东西写成页面；页面提到的文件改了、或者放得太久，就标成待复核；过时的内容换掉。
- 页面之间互相链接，关系图能看出它们怎么连在一起；每次改动都是一个能撤回的提交。
- 项目记忆和全局记忆（每个 agent 都该知道的事）每一轮都会带上。

![项目 wiki 的一页](.github/assets/readme/zh/wiki.webp)

![wiki 的关系图](.github/assets/readme/zh/wiki-graph.webp)

### 跨项目共享的 Skill Wiki

- 所有项目共用一个技能库。每个技能是一个带 `SKILL.md` 的文件夹，导入即可，由一个项目团队负责看管。
- 把技能装给需要它的 agent，Veyloom 按各家运行时自己加载的方式交给它。
- agent 用的时候会改进技能。改动先进入试用：正常用过几次就转正，人或这个技能的维护员也可以退回。
- 每个技能都留着历史：谁在哪里改了什么、为什么改。

![技能库里试用中的技能](.github/assets/readme/zh/skill.webp)

### 还有

- **agent 保持原样。** Veyloom 驱动的就是你已经在用的 CLI，沙箱、审批、插件、MCP 服务、上下文压缩都还是它们自己的；需要你决定的事，会变成群里的一张卡片。
- **经得起长时间运行。** 会话跨轮次续接；一轮进行中可以插话；agent 能定时唤醒自己；额度用完、登录失效时暂停而不是一次次失败；可能卡住的轮次会被标出来。
- 用量页、附件预览，以及汇集待你处理事项的收件箱。

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
git clone https://github.com/J0EY0/Veyloom.git
cd Veyloom
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
