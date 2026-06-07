# CCLimitPing (`limitping`)

[English](README.md) | **中文**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![CI](https://github.com/wavever/CCLimitPing/actions/workflows/ci.yml/badge.svg)](https://github.com/wavever/CCLimitPing/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/wavever/CCLimitPing?include_prereleases&sort=semver)](https://github.com/wavever/CCLimitPing/releases)
![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)

让你的 **Claude Code**、**Codex** 和 **GLM**(智谱 / Z.ai Coding Plan)的限额窗口
背靠背、不留空档。

这些 Provider 都按 **5 小时滚动窗口**(外加周限额)计费,而且 **5h 窗口从你发出的
第一条消息开始计时**。如果窗口重置后你没有立刻发消息,这段空档就被浪费了——下一个
窗口要等你下次用时才起算,于是窗口和你的作息逐渐错位。

`limitping` 会盯着每个 Provider,**在 5h 窗口重置的那一刻自动发一条最小消息,立即起算
下一个窗口**——让你的窗口连续、可预测。

```
claude  ✓ pinged (6.6s)
codex   ✓ pinged (13.6s, 16,862 tok (in 16,814 / out 48), $0.0098)
```

## 亮点

- 让 5 小时 Provider 窗口连续接上,避免空档把你的使用节奏越拖越偏。
- 用零消耗用量端点读取状态,并尽量通过官方 Provider 工具触发新窗口。
- 支持 Claude Code、Codex,以及可选开启的 GLM/Z.ai Coding Plan 监控。
- 内置 dry-run、周限额保护、重置缓冲、本地配置,且不带遥测。

## 快速开始

```sh
curl -fsSL https://raw.githubusercontent.com/wavever/CCLimitPing/main/install.sh | sh
limitping config init
limitping ping --dry-run
limitping status
limitping watch
```

如果你想先确认会发生什么、但不消耗 Provider 额度,先运行
`limitping ping --dry-run` 或 `limitping watch --dry-run`。

## 支持的 Provider

| Provider | 读取用量(零消耗) | 触发方式 | 鉴权 |
|---|---|---|---|
| **Claude Code** | `…/api/oauth/usage` | 交互式 Claude Code CLI | OAuth(钥匙串 / `~/.claude`) |
| **Codex** | `…/backend-api/wham/usage` | `codex exec` | OAuth(`~/.codex/auth.json`) |
| **GLM**(智谱 / Z.ai) | `…/api/monitor/usage/quota/limit` | 最小 chat 请求 | API Key(配置 / 环境变量) |

> [!NOTE]
> GLM **默认关闭**,且尚未在真实套餐上验证——启用前请先看
> [GLM 说明](#glm智谱--zai-coding-plan)。

## 工作原理

两件事职责完全分离:

| 任务 | 机制 | 代价 |
|------|------|------|
| **触发**新窗口 | 官方 CLI(交互式 Claude Code / `codex exec`),或一次最小 API 调用(GLM) | 消耗一点额度(这正是功能本身) |
| **读取**用量与重置时刻 | 零消耗用量端点(和 CodexBar / 社区插件用的是同一批) | 不消耗,也绝不会起算窗口 |

当 `watch` 发现 5h 窗口已经重置时,会先检查是否已有 Claude/Codex CLI 任务正在运行。
如果有,`limitping` 会等待并重新读取用量,而不是自己发 ping,因为这个任务的下一次模型
请求会自然起算新窗口。

- **Claude**:用 macOS 钥匙串(`Claude Code-credentials`)或 `~/.claude/.credentials.json`
  里的 OAuth token,读 `GET https://api.anthropic.com/api/oauth/usage`。触发使用带
  TTY 的交互式 `claude "<prompt>"` 会话,因此在 headless print 命令改走 Agent
  SDK/API credits 后仍会起算 Claude 订阅窗口。
- **Codex**:用 `~/.codex/auth.json` 里的 OAuth token,读
  `GET https://chatgpt.com/backend-api/wham/usage`。
- **GLM**:用你的 Coding Plan API Key,读 `GET …/api/monitor/usage/quota/limit`
  (`api.z.ai` 或 `open.bigmodel.cn`)。GLM 没有独立 CLI,所以**触发**改成直接发一条
  最小 chat 请求到 `…/api/coding/paas/v4/chat/completions`,而不是调命令行。

Claude/Codex 的 token 直接复用官方工具(无需另外登录),遇到 401 会自动刷新。GLM 用
静态 API Key(来自配置或环境变量)——见下文。

## 安装

`limitping` 是一个自包含的单文件二进制——**普通用户无需安装 Go**。

**一行脚本**(macOS / Linux):

```sh
curl -fsSL https://raw.githubusercontent.com/wavever/CCLimitPing/main/install.sh | sh
```

会从[最新 Release](https://github.com/wavever/CCLimitPing/releases/latest)下载对应
平台的预编译二进制,装到 `/usr/local/bin`(或 `~/.local/bin`)。可用
`LIMITPING_INSTALL_DIR` 覆盖安装目录。

**升级** —— 用最新 Release 替换已安装的二进制:

```sh
limitping upgrade
```

`limitping update` 是别名。

**卸载** —— 删除已安装的二进制以及配置/缓存:

```sh
limitping uninstall
```

使用 `limitping uninstall --keep-config` 可保留 `~/.config/limitping`(或
`$XDG_CONFIG_HOME/limitping`)。

**手动下载** —— 从 [Releases](https://github.com/wavever/CCLimitPing/releases) 页面
下载对应平台的压缩包(macOS/Linux 是 `.tar.gz`,Windows 是 `.zip`):

```sh
tar -xzf limitping_darwin_arm64.tar.gz
sudo mv limitping /usr/local/bin/
```

**Homebrew**(macOS / Linux)—— `brew install wavever/tap/limitping`
_(配好 Homebrew tap 后可用;见 `.goreleaser.yaml`)。_

**从源码**(开发者,需要 Go 1.25+):

```sh
go install github.com/wavever/CCLimitPing/cmd/limitping@latest
# 或在克隆后:
go build -o bin/limitping ./cmd/limitping
```

你启用的每个 Provider 各自需要凭据:登录好的 `claude` / `codex` CLI(Claude /
Codex),或一个 Coding Plan 的 API Key(GLM)。

## 使用

```sh
limitping config init          # 生成 ~/.config/limitping/config.toml
limitping status               # 查看 5h/周 用量百分比 + 重置倒计时(零消耗)
limitping status -v            # 额外打印原始 JSON
limitping ping                 # 立即触发所有已启用的 Provider
limitping ping claude          # 只触发 Claude
limitping ping codex           # 只触发 Codex
limitping ping glm             # 只触发 GLM
limitping ping --dry-run       # 只打印将执行的命令,不真正发送
limitping watch                # 前台守护:在每个窗口重置时自动 ping
limitping watch claude         # 只监测某一个 Provider(claude|codex|glm)
limitping watch --dry-run      # 只记录何时会触发,不真正发送
limitping upgrade              # 更新到最新 GitHub Release(update 是别名)
limitping uninstall            # 删除 limitping 以及配置/缓存
```

`ping` 会显示具体命令、实时计时(终端下是 spinner)、本次 ping 消耗的 **token 数**
(在 `codex --json` / GLM API 返回里解析),以及在可获取时显示 **美元费用**:

```
claude  → claude --model haiku .
claude  ✓ pinged (6.6s)
codex   → codex exec --skip-git-repo-check --json -c model_reasoning_effort=low -m gpt-5.4-mini ok
codex   ✓ pinged (13.6s, 16,862 tok (in 16,814 / out 48), $0.0098)
```

费用来源:
- **Claude** 交互式模式没有逐次 machine-readable 的用量/费用输出,所以不会显示
  token/cost 后缀。
- **Codex**(订阅)不返回美元费用,因此——和 CodexBar/ccusage 一样——我们用
  [LiteLLM 定价数据集](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json)
  按等价 API 单价折算(`费用 = 非缓存输入 × input + 缓存输入 × cache-read + 输出 × output`)。
  数据集缓存在 `~/.config/limitping/litellm_prices.json`(24h TTL),支持模型别名/日期
  后缀回退。需要设置 `[codex].model` 才能查到单价。
- **GLM** 是按 prompt 计的订阅,没有逐次的美元费用,因此只显示 token 数。

Claude 触发仍会消耗少量 Claude 订阅额度,但交互式 CLI 不暴露本次 ping 的精确 token 数。

`status` 示例:

```
claude
  5h     [█████░░░░░]  51.0%  resets in 3h14m    (Sun 00:10)
  weekly [█████░░░░░]  54.0%  resets in 7h04m    (Sun 04:00)

codex (plus)
  5h     [██░░░░░░░░]  24.0%  resets in 3h15m    (Sun 00:11)
  weekly [████░░░░░░]  37.0%  resets in 111h57m  (Thu 12:53)
```

## 配置

`~/.config/limitping/config.toml`(支持 `$XDG_CONFIG_HOME`):

```toml
weekly_threshold = 0.99   # 周用量 >= 此值(0..1)就跳过 ping,直到周窗口重置
reset_buffer     = "10s"  # 到达重置时刻后再等这么久才 ping(确保窗口已翻篇)
notify           = true   # 在 ping/跳过/失败 时弹 macOS 通知

[claude]
enabled    = true
prompt     = "."
model      = "haiku"      # 最便宜的档位;触发并不需要 SOTA 模型
extra_args = []           # 额外 Claude CLI 参数;print/headless-only 参数会被忽略
align_start = ""          # 可选 RFC3339:首个窗口的相位锚点;留空 = 尽快开始

[codex]
enabled          = true
prompt           = "ok"
model            = "gpt-5.4-mini"  # 用于触发的最便宜 Codex 模型
reasoning_effort = "low"  # 启用 web_search/image_gen 工具时,"minimal" 会被拒绝
extra_args       = []
align_start      = ""

[glm]
enabled  = false          # 选择性开启:开通套餐 + 拿到 API Key 后再启用
prompt   = "ok"
model    = "glm-4.6"      # 最便宜的标准模型;旗舰 GLM-5/5.1 按倍率扣额度
platform = "global"       # "global" = api.z.ai,"cn" = open.bigmodel.cn(智谱)
api_key  = ""             # 留空则从 $ZAI_API_KEY(global)/ $ZHIPU_API_KEY(cn)读取
align_start = ""
```

顶层配置项:

- **`weekly_threshold`** —— 周窗口到/超过此值时,`watch` 停止 ping 并等到周重置
  (除非还有可用 credits)。
- **`reset_buffer`** —— 在窗口重置时刻之后再等待多久才 ping,确保窗口确实已翻篇。
- **`align_start`**(每个 Provider)—— 固定窗口相位:设为一个未来的 RFC3339 时间,
  把第一次 ping 推迟到那时;之后窗口每 ~5h 自动接龙。

### 为什么用便宜模型

触发窗口和用哪个模型无关——**任何**计费请求都会起算 5h 计时——所以 ping 用每家最便宜
的模型,尽量少吃额度:

- **Claude → `haiku`**:同时避开单独的周 Opus 额度池。
- **Codex → `gpt-5.4-mini`**:mini 变体(你的套餐有哪些见 `~/.codex/models_cache.json`)。
- **GLM → `glm-4.6`**:标准模型;旗舰 GLM-5/5.1 按 2–3× 倍率扣额度,只为触发不值得用。

Claude/Codex 运行时都拿不到每个模型的价格(Anthropic 本地价格缓存是空的;Codex 的模型
缓存没有价格字段),所以这里用"最便宜模型"作为合理默认,而不是实时查价。需要的话可
按 Provider 覆盖 `model`。

### GLM(智谱 / Z.ai Coding Plan)

GLM 和 Claude/Codex 是同一套 **5h + 周** 结构,但有两点不同:

- **鉴权是静态 API Key**,不是 OAuth。填到 `[glm].api_key`,或留空并导出
  `ZAI_API_KEY`(global)/ `ZHIPU_API_KEY`(CN)。用量读取打 `…/api/monitor/usage/quota/limit`;
  Key 放在 `Authorization` 头里,**不加** `Bearer` 前缀(该端点就是这么要的)。
- **触发是直接 API 调用**,因为 GLM 没有独立 CLI。它发一条 max_tokens=1 的 chat 请求到
  `…/api/coding/paas/v4/chat/completions`。

> [!WARNING]
> **尚未在真实套餐上验证。** GLM 默认关闭。端点形状来自社区插件;请在你自己的套餐上确认:
> (a) 监控端点能返回你真实的 5h/周窗口;(b) 5h 窗口是按"首条消息"起算的(这样在重置点
> 补刀才真的填上空档)。如果 GLM 的窗口是固定时钟窗或逐请求滑动窗,补刀就没有意义。

## 后台运行 `watch`(macOS,可选)

`watch` 默认前台运行。要用 `launchd` 常驻,创建
`~/Library/LaunchAgents/com.limitping.watch.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.limitping.watch</string>
  <key>ProgramArguments</key>
  <array>
    <string>/ABSOLUTE/PATH/TO/limitping</string>
    <string>watch</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/tmp/limitping.log</string>
  <key>StandardErrorPath</key><string>/tmp/limitping.err</string>
</dict>
</plist>
```

```sh
launchctl load ~/Library/LaunchAgents/com.limitping.watch.plist
```

## 成本与注意事项

- 本地数据处理和网络行为见 [PRIVACY.md](PRIVACY.md)。
- 漏洞报告和凭据处理说明见 [SECURITY.md](SECURITY.md)。
- 触发会**消耗一点额度**(约每 5h 一次 ≈ 每周 33 次)。ping 用最小 prompt + 低 reasoning,
  成本很小但非零。
- **用量端点是非官方接口**,可能变更;它们都是只读的,并按 Provider 隔离,方便单独热修。
- 以 macOS 为主:钥匙串读取和通知仅限 macOS。Codex 的 `auth.json` 跨平台;Claude 在
  Linux 上用 `~/.claude/.credentials.json`;非 macOS 上通知为空操作。

## 目录结构

```
cmd/limitping            CLI 入口
internal/config          TOML 配置
internal/usage           归一化的用量模型
internal/auth            Claude(钥匙串)+ Codex(auth.json)token;GLM API Key
internal/provider        各 Provider 的 ReadUsage(端点)+ Trigger(CLI / API)
internal/pricing         基于 LiteLLM 的美元费用查询(Codex)
internal/scheduler       watch 引擎(sleep 到重置、尊重周限额、退避重试)
internal/notify          macOS osascript 通知
internal/cli             cobra 命令:status、ping、watch、config、upgrade、uninstall、version
```

## 贡献

欢迎提 Issue 和 PR。请先阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 和
[CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。提交前请先跑:

```sh
gofmt -l .        # 应当无输出
go build ./...
go vet ./...
go test ./...
```

Provider 都隔离在 `internal/provider`(每家一个文件),只需实现一个很小的 `Provider`
接口(`ReadUsage` + `Trigger`),所以新增一个 Provider 基本就是一个自包含文件,加上在
`internal/cli` 和 `internal/config` 里接一下线。

**发版**是自动的:打一个 tag 并推送,GitHub Actions 会跑 GoReleaser 交叉编译各平台
二进制并发布 Release。

```sh
git tag v0.2.0 && git push origin v0.2.0
```

## 许可证

[MIT](LICENSE) © wavever
