# codebuddy-agent-sdk-go

A Go SDK for driving the [CodeBuddy Code](https://www.codebuddy.cn/docs/cli) agent runtime (`codebuddy` / `cbc`) over its bidirectional **stdin/stdout stream-json wire protocol**. A Go counterpart of `@tencent-ai/agent-sdk` (TypeScript) / `codebuddy_agent_sdk` (Python), intended for embedding the CodeBuddy agent engine as an in-process kernel in Go applications.

- **Wire protocol**: stream-json (verified against CodeBuddy CLI **2.150.0**)
- **License**: MIT (protocol shape mirrors `@tencent-ai/agent-sdk`, MIT)
- **Zero dependencies** — stdlib only

## Installation

```bash
go get github.com/godeps/codebuddy-agent-sdk-go
```

The SDK does **not** bundle the CLI binary. It resolves the executable via:
1. `Options.PathToCLI`, or
2. `CODEBUDDY_CODE_PATH` env var, or
3. `PATH` lookup (`codebuddy`, then `cbc`)

Install the CLI per the [official guide](https://www.codebuddy.cn/docs/cli/install) and log in once with `codebuddy login`.

## Quick start

```go
package main

import (
    "context"
    "fmt"
    "time"

    codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
    "github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    res, err := codebuddy.QueryOnce(ctx, "explain what this repo does",
        codebuddy.NewOptions().
            WithCWD("/path/to/repo").
            WithPermissionMode(protocol.PermissionBypassPermissions))
    if err != nil {
        panic(err)
    }
    fmt.Println(res.Text)          // final assistant text
    fmt.Println(res.SessionID)     // resumable session id
    fmt.Println(res.NumTurns)
}
```

### Streaming events (one-shot)

```go
msgs, err := codebuddy.Query(ctx, "refactor auth.go", opts)
for m := range msgs {
    switch v := m.(type) {
    case *protocol.SystemMessage:      // init / status / task_*
    case *protocol.AssistantMessage:   // text + tool_use blocks
    case *protocol.ResultMessage:      // terminal result
    }
}
```

### Multi-turn sessions

`Session` keeps one CLI process alive across turns (context persists
in-process; no `--resume` needed):

```go
sess, _ := codebuddy.NewSession(ctx, opts)
defer sess.Close()

sess.Send("remember the number 73")
res1, _, _ := sess.ReceiveResponse(60 * time.Second)

sess.Send("what number did I say?")
res2, _, _ := sess.ReceiveResponse(60 * time.Second)  // "73"
```

Mid-session control: `sess.Interrupt()`, `sess.SetPermissionMode(...)`,
`sess.Rewind(msgID, scope, dryRun)` (files / conversation / both —
CodeBuddy extension over Claude Code).

Model management (verified against the real CLI):

```go
models, current := sess.Models()   // from the initialize handshake
for _, m := range models {
    fmt.Println(m.ID, m.Name)      // hy4-preview, hy3, glm-5.3, kimi-k3-2, ...
}

// NOTE: set_model only works once the session is established (after the
// first turn); before that the SDK returns ErrSessionNotEstablished.
// The switch takes effect from the NEXT turn.
resp, err := sess.SetModel("hy3")  // resp: {Model, PreviousModel}
```

### Runtime tool approval (can_use_tool)

The CLI routes every permission prompt to your callback over the control
channel and **blocks on your answer**:

```go
opts := codebuddy.NewOptions().
    WithPermissionMode(protocol.PermissionDefault).
    WithCanUseTool(func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error) {
        if req.ToolName == "Bash" && strings.Contains(req.InputMap()["command"].(string), "rm -rf") {
            return protocol.Deny("destructive command blocked"), nil
        }
        return protocol.Allow(nil), nil // nil = keep the model's input
    })
```

> **Wire note**: CodeBuddy's permission response shape is
> `{allowed, updatedInput | reason, tool_use_id}` — *not* Claude Code's
> `{behavior: "allow"}` envelope. The SDK translates `protocol.Allow` /
> `protocol.Deny` for you.

> **Hook note (CLI 2.150, probe-verified)**: a `PreToolUse` block only takes
> effect when `continue` is false — a bare `decision: "block"` is ignored by
> the CLI despite the docs. The SDK normalizes this: returning
> `HookJSONOutput{Decision: "block", Reason: ...}` automatically sets
> `continue: false` on the wire, so the documented behavior is what you get.

### Hooks & in-process MCP servers

```go
opts := codebuddy.NewOptions().
    WithHooks(map[protocol.HookEvent][]protocol.HookCallbackMatcher{
        protocol.HookPreToolUse: {{Matcher: "Bash", Hooks: []protocol.HookCallback{{Type: "callback", CallbackID: "audit"}}}},
    }).
    WithHookCallback(func(ctx context.Context, req *protocol.HookCallbackRequest, in *protocol.HookInput) (protocol.HookJSONOutput, error) {
        return protocol.HookJSONOutput{Decision: "approve"}, nil
    }).
    // in-process MCP: declare names here, serve frames via the handler
    WithSdkMcpServers("saker-tools").
    WithMcpServers(map[string]protocol.McpServerConfig{
        "saker-tools": protocol.NewMcpSdkConfig("saker-tools"),
    }).
    WithMcpMessageHandler(func(ctx context.Context, server string, msg json.RawMessage) (json.RawMessage, error) {
        return handleJSONRPC(msg), nil
    })
```

## Features

| Capability | API |
|---|---|
| One-shot query | `Query`, `QueryOnce` |
| Multi-turn session | `NewSession` + `Send` / `ReceiveResponse` / `Stream` |
| Tool permission callback | `WithCanUseTool` (allow / deny / rewrite input / interrupt) |
| Hooks | `WithHooks` + `WithHookCallback` |
| In-process MCP servers | `WithSdkMcpServers` + `WithMcpMessageHandler` |
| External MCP servers | `WithMcpServers` (stdio/http/sse) / `WithMcpConfigFile` |
| Structured output | `WithJSONSchema` → `QueryResult.StructuredOutput` |
| Session resume / fork | `WithResume` / `WithContinue` / `WithForkSession` / `WithSessionID` |
| Rewind (files/history) | `Session.Rewind` (Code | Conversation | CodeAndConversation, dry-run) |
| Partial-message streaming | `WithPartialMessages` |
| Model / turn limits | `WithModel` / `WithFallbackModel` / `WithMaxTurns` / `WithEffort` |
| Model list + mid-session switch | `Session.Models()` / `Session.SetModel()` |
| Tool restriction | `WithAllowedTools` / `WithDisallowedTools` / `WithTools` |
| Settings isolation | `WithSettingSources` (SDK default: `none`) |
| Background task events | `system/task_*` messages (`Session` keeps reading past results) |

## Authentication

The CLI owns its login state (`~/.codebuddy`, via `codebuddy login`); the SDK
reuses it by default. For headless/CI use, supply an API key:

```go
opts := codebuddy.NewOptions().
    WithAuth(auth.APIKeyAuth{APIKey: os.Getenv("CODEBUDDY_API_KEY")})
```

(`CODEBUDDY_API_KEY` / `ANTHROPIC_AUTH_TOKEN` in the ambient environment are
also honored.)

## Protocol notes (vs. the TypeScript SDK)

Verified against CLI 2.150.0:

- Launch argv mirrors the TS transport: `--input-format=stream-json
  --output-format=stream-json --verbose` (no `-p`; long-lived stream mode).
- Env markers set by this SDK: `CODEBUDDY_CODE_ENTRYPOINT=sdk-go`,
  `DISABLE_AUTOUPDATER=1`, `CODEBUDDY_DISABLE_AUTO_MEMORY=1`,
  `CODEBUDDY_CUSTOM_HEADERS` (SDK User-Agent). `Query` additionally sets
  `CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS=1` (a one-shot consumer stops at
  the first `result` and cannot receive cross-turn task notifications).
- `initialize` control_request registers hooks / agents / SDK MCP servers;
  the response carries slash commands etc.
- CLI→SDK control requests: `can_use_tool`, `hook_callback`, `mcp_message`,
  `elicitation_create` (auto-cancelled when no handler).
- Every `result` carries `session_id` — persist it to `--resume` later.
- `file-history-snapshot` messages provide rewind anchors
  (`snapshot.messageId`).

## Testing

```bash
go test ./...                       # unit tests (scripted fakecli)
CODEBUDDY_SDK_E2E=1 go test -run TestE2E -v   # against the real CLI
```

E2E coverage: one-shot query, multi-turn session context retention,
can_use_tool **allow** (command really executes) and **deny** (CLI honors the
denial), model list from the initialize handshake, and mid-session
`SetModel` (confirmed by the CLI and by the next turn actually running on
the new model).

## Examples

- [`examples/basic`](examples/basic) — one-shot query
- [`examples/permissions`](examples/permissions) — runtime tool approval policy

## Related

- [godeps/claude-agent-sdk-go](https://github.com/godeps/claude-agent-sdk-go) — same shape for Claude Code
- [godeps/qoder-agent-sdk-go](https://github.com/godeps/qoder-agent-sdk-go) — same shape for Qoder
- [godeps/codex-sdk-go](https://github.com/godeps/codex-sdk-go) — same shape for Codex
- [CodeBuddy Agent SDK docs](https://www.codebuddy.cn/docs/cli/sdk)
