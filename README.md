# easy-llm-router

`easy-llm-router` 是一个 Go TUI 本地反向代理，用于在多个 LLM API Key 之间进行粘性路由。当上游明确返回额度不足或认证失败时，路由器会在响应尚未对客户端可见的前提下切换到下一个可用 Key，并透明重发原请求。

## 功能

- 单个本地端口，通过 `/pools/{name}` 暴露多个路由池
- OpenCode Go、OpenCode Zen、OpenAI-compatible、Anthropic-compatible 内置预设
- 自定义 HTTP 服务、验证请求和结构化响应规则
- 五态凭证模型：未知、可用、额度不足、凭证无效、已禁用
- 固定周期验证额度不足凭证，默认每五分钟
- 手动切换、排序、禁用、标记额度不足、单个验证和全局验证
- OpenAI/Anthropic 流式响应透传
- 请求重放内存与临时文件分层，以及大型请求安全降级
- 持久状态、结构化轮转日志和 TUI 实时日志视图

## 安装与运行

需要 Go 1.25 或更高版本。

```bash
go install github.com/yeck/easy-llm-router/cmd/easy-llm-router@latest
easy-llm-router
```

在源码目录运行：

```bash
go run ./cmd/easy-llm-router
```

首次启动会进入向导，创建一个 OpenCode Go 路由池和首个凭证。向导完成后代理默认监听 `127.0.0.1:8787`。

## 客户端配置

假设池名为 `main`，将客户端 Base URL 指向：

```text
http://127.0.0.1:8787/pools/main
```

路由器会把池前缀之后的路径拼接到服务配置的上游 Base URL。例如客户端请求：

```text
POST /pools/main/chat/completions
```

OpenCode Go 默认会被转发到：

```text
POST https://opencode.ai/zen/go/v1/chat/completions
```

OpenCode 可以在 `opencode.json` 中覆盖对应 Provider 的 Base URL：

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "opencode-go": {
      "options": {
        "baseURL": "http://127.0.0.1:8787/pools/main"
      }
    }
  }
}
```

客户端发送的认证头会被移除，路由器按当前凭证和服务配置重新注入认证信息。

## TUI 操作

数字键 `1` 到 `4` 切换路由池、服务、日志和设置视图。各视图底部会显示当前可用操作。

路由池视图支持：

- `[` / `]`：切换路由池
- `j` / `k`：选择凭证
- `a` / `e` / `x`：添加、编辑、删除凭证
- `J` / `K`：调整凭证顺序
- `m`：将可用凭证设为当前凭证
- `d`：禁用或恢复凭证
- `z`：手动标记额度不足
- `v` / `V`：验证单个凭证或确认后验证全部非禁用凭证
- `p` / `n` / `X`：添加、重命名或删除路由池，并修改验证间隔

服务视图支持服务配置的增删改、响应规则的添加和删除，以及使用响应样例测试分类结果。设置中的端口、日志级别和资源上限在下一次启动时生效。

## 状态与安全

Linux 默认文件位置：

```text
~/.config/easy-llm-router/config.yaml
~/.config/easy-llm-router/credentials.yaml
~/.local/state/easy-llm-router/state.json
~/.local/state/easy-llm-router/router.jsonl
```

程序遵循 `XDG_CONFIG_HOME` 和 `XDG_STATE_HOME`。API Key 明文保存在独立凭证文件中；Linux 和 macOS 上该文件必须为 `0600`，否则程序拒绝加载。日志不会记录完整 Key、认证头或成功请求/响应正文。

代理只允许监听回环地址。退出 TUI 时停止接收新请求，并默认等待最多 60 秒完成现有请求；再次退出会强制终止。

## 错误分类

普通 `429` 不会被默认视为额度耗尽。OpenCode Go 预设只匹配公开样例中的 `GoUsageLimitError`，OpenCode Zen 预设匹配 `FreeUsageLimitError`。供应方瞬时限速、超时和 `5xx` 保持无结论，不改变凭证持久状态。

未知错误可以在 `debug` 日志中查看清理后的响应头和最多 4 KiB 错误体，再通过 TUI 调整服务规则。

## 开发

```bash
go test -race ./...
go vet ./...
go build ./cmd/easy-llm-router
```

领域模型和架构决策见 [CONTEXT.md](./CONTEXT.md)、[产品需求](./docs/requirements.md) 和 [首版架构](./docs/architecture.md)。
