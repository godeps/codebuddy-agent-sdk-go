// Package codebuddy is a Go SDK for the CodeBuddy Code CLI agent runtime.
//
// It spawns the `codebuddy` (or `cbc`) binary as a child process and speaks
// its stream-json wire protocol over stdin/stdout, exposing agent message
// events as a Go channel and CLI-initiated control requests (tool-permission
// prompts, hooks, in-process MCP servers) as host callbacks.
//
// Two usage shapes are provided:
//
//   - Query / QueryOnce: one-shot prompt -> message channel (or aggregated
//     result). Simplest; good for scripting and single tasks.
//   - Session: a long-lived bidirectional session supporting multiple turns
//     and mid-session control (interrupt, set model / permission mode,
//     rewind). Mirrors the TypeScript Session API.
//
// Authentication reuses the CLI's own persisted login state (~/.codebuddy,
// created by `codebuddy login`); an API key may be supplied for headless/CI
// use via Options.Auth. See the auth subpackage.
//
// The wire protocol mirrors the CodeBuddy Agent SDK
// (@tencent-ai/agent-sdk), documented at
// https://www.codebuddy.cn/docs/cli/sdk and /docs/cli/headless.
package codebuddy
