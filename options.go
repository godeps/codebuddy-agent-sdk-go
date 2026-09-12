package codebuddy

import (
	"context"
	"encoding/json"

	"github.com/godeps/codebuddy-agent-sdk-go/auth"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

// Options configures a query or session. Build one with NewOptions and the
// With* builders, or populate the struct directly.
type Options struct {
	// --- Runtime / launch ---
	// PathToCLI overrides CLI resolution (else CODEBUDDY_CODE_PATH / PATH).
	PathToCLI string
	// CWD is the working directory of the CLI child process.
	CWD string
	// Env is a full replacement environment for the child. Nil inherits the
	// current process environment (recommended).
	Env map[string]string
	// Auth optionally supplies an API key for headless/CI use. Zero value
	// means "use the CLI's own login state" (~/.codebuddy).
	Auth auth.APIKeyAuth
	// Debug enables CLI debug logging on stderr.
	Debug bool

	// --- Model / behavior ---
	Model         string
	FallbackModel string
	MaxTurns      *int
	Effort        string
	// SystemPrompt replaces the CLI system prompt.
	SystemPrompt string
	// AppendSystemPrompt adds instructions to the default system prompt.
	AppendSystemPrompt string

	// --- Permissions ---
	PermissionMode             protocol.PermissionMode
	DangerouslySkipPermissions bool
	AllowedTools               []string
	DisallowedTools            []string
	// Tools restricts built-in tools (empty slice disables all; nil = CLI
	// default). Maps to --tools.
	Tools *[]string

	// --- Streaming / output ---
	IncludePartialMessages bool
	// JSONSchema enables structured output; the result message then carries
	// StructuredOutput conforming to this schema.
	JSONSchema json.RawMessage

	// --- MCP / hooks / agents ---
	McpServers      map[string]protocol.McpServerConfig
	McpConfigFile   string
	StrictMcpConfig bool
	Hooks           map[protocol.HookEvent][]protocol.HookCallbackMatcher
	Agents          map[string]protocol.AgentDefinition
	// SdkMcpServers names in-process MCP servers served by this SDK host
	// (declared at initialize; tool calls arrive via mcp_message control
	// requests and are answered by the McpMessageHandler callback).
	SdkMcpServers []string

	// --- Session ---
	SessionID      string
	Continue       bool
	Resume         string
	ForkSession    bool
	PersistSession *bool

	// --- Settings / directories ---
	// SettingSources: nil means SDK default "none" (full isolation).
	SettingSources *[]string
	Settings       map[string]json.RawMessage
	SettingsFile   string
	AdditionalDirs []string

	// --- Tuning ---
	RequestTimeoutMs int
	CloseGraceMs     int
	// DisableBackgroundTasks sets CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS=1.
	// Default true for Query (one-shot stops at the first result and would
	// miss cross-turn task notifications); Session may set it false to keep
	// receiving task events across turns.
	DisableBackgroundTasks *bool

	// ExtraArgs are raw "--flag [value]" pairs appended to argv.
	ExtraArgs map[string]*string
	// Args are raw passthrough arguments appended last.
	Args []string

	// --- Callbacks (CLI -> SDK control requests) ---
	// CanUseTool answers can_use_tool permission prompts. Required when
	// PermissionMode is default/acceptEdits and tools may need approval;
	// without it, prompts are denied with "no permission callback".
	CanUseTool func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error)
	// HookCallback answers hook_callback control requests for hooks
	// registered via Hooks.
	HookCallback func(ctx context.Context, req *protocol.HookCallbackRequest, input *protocol.HookInput) (protocol.HookJSONOutput, error)
	// McpMessageHandler proxies JSON-RPC frames for in-process SDK MCP
	// servers. The handler receives one MCP JSON-RPC message and must
	// return the response message (or nil for notifications).
	McpMessageHandler func(ctx context.Context, serverName string, message json.RawMessage) (json.RawMessage, error)
	// StderrHandler receives each CLI stderr line.
	StderrHandler func(line string)
}

// NewOptions returns Options with sensible defaults.
func NewOptions() *Options {
	return &Options{
		CloseGraceMs: 2000,
	}
}

// --- Builders ---

func (o *Options) WithPathToCLI(p string) *Options        { o.PathToCLI = p; return o }
func (o *Options) WithCWD(cwd string) *Options            { o.CWD = cwd; return o }
func (o *Options) WithEnv(env map[string]string) *Options { o.Env = env; return o }
func (o *Options) WithAuth(a auth.APIKeyAuth) *Options    { o.Auth = a; return o }
func (o *Options) WithModel(m string) *Options            { o.Model = m; return o }
func (o *Options) WithFallbackModel(m string) *Options    { o.FallbackModel = m; return o }
func (o *Options) WithMaxTurns(n int) *Options            { o.MaxTurns = &n; return o }
func (o *Options) WithEffort(e string) *Options           { o.Effort = e; return o }
func (o *Options) WithSystemPrompt(s string) *Options     { o.SystemPrompt = s; return o }
func (o *Options) WithAppendSystemPrompt(s string) *Options {
	o.AppendSystemPrompt = s
	return o
}
func (o *Options) WithPermissionMode(m protocol.PermissionMode) *Options {
	o.PermissionMode = m
	return o
}
func (o *Options) WithDangerouslySkipPermissions(b bool) *Options {
	o.DangerouslySkipPermissions = b
	return o
}
func (o *Options) WithAllowedTools(t []string) *Options    { o.AllowedTools = t; return o }
func (o *Options) WithDisallowedTools(t []string) *Options { o.DisallowedTools = t; return o }
func (o *Options) WithTools(t []string) *Options           { o.Tools = &t; return o }
func (o *Options) WithPartialMessages(b bool) *Options     { o.IncludePartialMessages = b; return o }
func (o *Options) WithJSONSchema(schema json.RawMessage) *Options {
	o.JSONSchema = schema
	return o
}
func (o *Options) WithMcpServers(m map[string]protocol.McpServerConfig) *Options {
	o.McpServers = m
	return o
}
func (o *Options) WithMcpConfigFile(path string) *Options { o.McpConfigFile = path; return o }
func (o *Options) WithStrictMcpConfig(b bool) *Options    { o.StrictMcpConfig = b; return o }
func (o *Options) WithHooks(h map[protocol.HookEvent][]protocol.HookCallbackMatcher) *Options {
	o.Hooks = h
	return o
}
func (o *Options) WithAgents(a map[string]protocol.AgentDefinition) *Options {
	o.Agents = a
	return o
}
func (o *Options) WithSdkMcpServers(names ...string) *Options {
	o.SdkMcpServers = append(o.SdkMcpServers, names...)
	return o
}
func (o *Options) WithSessionID(s string) *Options { o.SessionID = s; return o }
func (o *Options) WithContinue(b bool) *Options    { o.Continue = b; return o }
func (o *Options) WithResume(s string) *Options    { o.Resume = s; return o }
func (o *Options) WithForkSession(b bool) *Options { o.ForkSession = b; return o }
func (o *Options) WithPersistSession(b bool) *Options {
	o.PersistSession = &b
	return o
}
func (o *Options) WithSettingSources(s ...string) *Options { o.SettingSources = &s; return o }
func (o *Options) WithSettings(st map[string]json.RawMessage) *Options {
	o.Settings = st
	return o
}
func (o *Options) WithAddDir(dirs ...string) *Options {
	o.AdditionalDirs = append(o.AdditionalDirs, dirs...)
	return o
}
func (o *Options) WithDebug(b bool) *Options { o.Debug = b; return o }

// WithCanUseTool registers the tool-permission callback.
func (o *Options) WithCanUseTool(f func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error)) *Options {
	o.CanUseTool = f
	return o
}

// WithHookCallback registers the hook callback.
func (o *Options) WithHookCallback(f func(ctx context.Context, req *protocol.HookCallbackRequest, input *protocol.HookInput) (protocol.HookJSONOutput, error)) *Options {
	o.HookCallback = f
	return o
}

// WithMcpMessageHandler registers the in-process SDK MCP server proxy.
func (o *Options) WithMcpMessageHandler(f func(ctx context.Context, serverName string, message json.RawMessage) (json.RawMessage, error)) *Options {
	o.McpMessageHandler = f
	return o
}

// WithStderrHandler registers a stderr line handler.
func (o *Options) WithStderrHandler(f func(string)) *Options { o.StderrHandler = f; return o }
