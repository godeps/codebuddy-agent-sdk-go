// Package codebuddy is a Go SDK for the CodeBuddy Code CLI agent runtime.
//
// It spawns the `codebuddy` (or `cbc`) binary as a child process and speaks
// its stream-json wire protocol over stdin/stdout, exposing agent message
// events as a Go channel and CLI-initiated control requests (tool permission
// prompts, hooks, in-process MCP) as host callbacks.
//
// Two usage shapes are provided:
//
//   - Query: one-shot prompt -> message channel that closes at the terminal
//     result. Simplest; good for scripting and single tasks.
//   - Session: a long-lived bidirectional session supporting multiple turns,
//     mid-session control (interrupt, set model / permission mode, rewind),
//     and background-task events. Mirrors the TypeScript Session API.
//
// Example (one-shot):
//
//	msgs, err := codebuddy.Query(ctx, "explain this repo", codebuddy.NewOptions().
//	    WithCWD("/path/to/repo").
//	    WithPermissionMode(protocol.PermissionBypassPermissions))
//	if err != nil { log.Fatal(err) }
//	for m := range msgs {
//	    if r, ok := m.(*protocol.ResultMessage); ok { fmt.Println(r.Result) }
//	}
//
// The CLI authenticates through its own persisted login (~/.codebuddy); see
// the auth package for API-key (headless) mode.
package codebuddy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godeps/codebuddy-agent-sdk-go/auth"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
	"github.com/godeps/codebuddy-agent-sdk-go/runtime"
	"github.com/godeps/codebuddy-agent-sdk-go/transport"
)

// Query starts a one-shot session: spawns codebuddy, performs the initialize
// handshake, writes the prompt as the first user message, and returns a
// channel of protocol.Message events. The channel closes after the terminal
// result message (or when the CLI exits). The session is torn down when ctx
// is done.
//
// Background tasks are disabled by default for Query (it stops at the first
// result and cannot receive cross-turn task events). Set
// Options.DisableBackgroundTasks to a false pointer to override.
func Query(ctx context.Context, prompt string, opts *Options) (<-chan protocol.Message, error) {
	if opts == nil {
		opts = NewOptions()
	}
	if err := checkAuth(opts); err != nil {
		return nil, err
	}
	r, err := newRunner(ctx, opts, true)
	if err != nil {
		return nil, err
	}
	if err := r.sendUserText(prompt); err != nil {
		r.shutdown()
		return nil, err
	}
	return r.out, nil
}

// QueryResult is a convenience aggregate of a completed one-shot Query.
type QueryResult struct {
	Text             string          // final assistant text (result.Result, or concatenated assistant blocks)
	StructuredOutput json.RawMessage // populated when Options.JSONSchema was set
	Usage            protocol.Usage  // token usage from the result message
	ModelUsage       map[string]protocol.ModelUsage
	NumTurns         int
	DurationMs       int
	TotalCostUSD     float64
	SessionID        string
	IsError          bool
	Subtype          string
}

// QueryOnce runs a one-shot Query and blocks until the terminal result,
// returning an aggregated QueryResult. It returns a *ResultError when the CLI
// reports a result-level failure.
func QueryOnce(ctx context.Context, prompt string, opts *Options) (*QueryResult, error) {
	msgs, err := Query(ctx, prompt, opts)
	if err != nil {
		return nil, err
	}
	res := &QueryResult{}
	var textParts []string
	for m := range msgs {
		switch v := m.(type) {
		case *protocol.AssistantMessage:
			for _, b := range v.Message.Content {
				if b.Type == "text" && b.Text != "" {
					textParts = append(textParts, b.Text)
				}
			}
		case *protocol.ResultMessage:
			res.NumTurns = v.NumTurns
			res.DurationMs = v.DurationMs
			res.TotalCostUSD = v.TotalCostUSD
			res.SessionID = v.SessionID
			res.IsError = v.IsError
			res.Subtype = v.Subtype
			res.ModelUsage = v.ModelUsage
			if v.Usage != nil {
				res.Usage = *v.Usage
			}
			if len(v.StructuredOutput) > 0 {
				res.StructuredOutput = v.StructuredOutput
			}
			res.Text = v.Result
		case *protocol.SystemMessage:
			if res.SessionID == "" && v.SessionID != "" {
				res.SessionID = v.SessionID
			}
		}
	}
	if res.Text == "" {
		res.Text = strings.Join(textParts, "")
	}
	if res.IsError {
		return res, &ResultError{Subtype: res.Subtype, Result: res.Text, SessionID: res.SessionID}
	}
	return res, nil
}

// checkAuth verifies an auth path exists (API key or persisted CLI login).
func checkAuth(opts *Options) error {
	if opts.Auth.Configured() {
		return nil
	}
	if auth.LoggedIn() {
		return nil
	}
	// An env-provided key also counts.
	for _, k := range []string{"CODEBUDDY_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"} {
		if v := envGet(opts, k); v != "" {
			return nil
		}
	}
	return ErrNotLoggedIn
}

func envGet(opts *Options, key string) string {
	if opts.Env != nil {
		return opts.Env[key]
	}
	return os.Getenv(key)
}

// runner drives one session: transport + control bookkeeping + dispatch loop.
type runner struct {
	ctx       context.Context
	opts      *Options
	tr        *transport.ProcessTransport
	out       chan protocol.Message
	runDone   chan struct{}
	pendingMu sync.Mutex
	pending   map[string]chan protocol.ControlResponse
	closeOnce sync.Once
	closed    atomic.Bool
	oneShot   bool

	sessionMu sync.RWMutex
	sessionID string

	initMu   sync.RWMutex
	initResp *protocol.InitializeResponse
}

func newRunner(ctx context.Context, opts *Options, oneShot bool) (*runner, error) {
	// Merge auth env into the transport env.
	env := opts.Env
	authEnv := opts.Auth.Env()
	if len(authEnv) > 0 {
		if env == nil {
			// transport treats nil Env as "inherit"; build a full copy so the
			// auth overlay does not wipe the inherited environment.
			env = map[string]string{}
			for _, kv := range os.Environ() {
				if i := strings.IndexByte(kv, '='); i >= 0 {
					env[kv[:i]] = kv[i+1:]
				}
			}
		}
		for k, v := range authEnv {
			env[k] = v
		}
	}

	disableBg := oneShot // Query default
	if opts.DisableBackgroundTasks != nil {
		disableBg = *opts.DisableBackgroundTasks
	}

	topts := transport.Options{
		PathToCLI:                  opts.PathToCLI,
		CWD:                        opts.CWD,
		Env:                        env,
		Model:                      opts.Model,
		FallbackModel:              opts.FallbackModel,
		MaxTurns:                   opts.MaxTurns,
		Effort:                     opts.Effort,
		SystemPrompt:               opts.SystemPrompt,
		AppendSystemPrompt:         opts.AppendSystemPrompt,
		PermissionMode:             opts.PermissionMode,
		DangerouslySkipPermissions: opts.DangerouslySkipPermissions,
		AllowedTools:               opts.AllowedTools,
		DisallowedTools:            opts.DisallowedTools,
		Tools:                      opts.Tools,
		SessionID:                  opts.SessionID,
		Continue:                   opts.Continue,
		Resume:                     opts.Resume,
		ForkSession:                opts.ForkSession,
		PersistSession:             opts.PersistSession,
		McpServers:                 opts.McpServers,
		McpConfigFile:              opts.McpConfigFile,
		StrictMcpConfig:            opts.StrictMcpConfig,
		Settings:                   opts.Settings,
		SettingsFile:               opts.SettingsFile,
		SettingSources:             opts.SettingSources,
		AdditionalDirs:             opts.AdditionalDirs,
		IncludePartialMessages:     opts.IncludePartialMessages,
		JSONSchema:                 opts.JSONSchema,
		ReplayUserMessages:         true,
		RequestTimeoutMs:           opts.RequestTimeoutMs,
		Verbose:                    opts.Debug,
		ExtraArgs:                  opts.ExtraArgs,
		Args:                       opts.Args,
		DisableBackgroundTasks:     disableBg,
		CloseGraceMs:               opts.CloseGraceMs,
		StderrHandler:              opts.StderrHandler,
	}
	if opts.Debug {
		topts.Args = append([]string{"--debug"}, topts.Args...)
	}

	tr := transport.NewProcessTransport(topts)
	r := &runner{
		ctx:     ctx,
		opts:    opts,
		tr:      tr,
		out:     make(chan protocol.Message, 64),
		runDone: make(chan struct{}),
		pending: make(map[string]chan protocol.ControlResponse),
		oneShot: oneShot,
	}
	if err := tr.Initialize(ctx); err != nil {
		if errors.Is(err, runtime.ErrNotFound) {
			return nil, ErrCLINotFound
		}
		return nil, err
	}
	go r.runLoop()
	// initialize handshake (best-effort: some CLI versions do not require it,
	// but sending it registers hooks/agents/sdk-mcp and is harmless).
	if err := r.handshake(); err != nil {
		// Non-fatal: log via stderr handler; the agent stream still works for
		// plain prompts. We surface it only if the caller has no CanUseTool.
		if opts.StderrHandler != nil {
			opts.StderrHandler("initialize handshake: " + err.Error())
		}
	}
	return r, nil
}

// runLoop dispatches messages until the CLI exits or a terminal result arrives.
func (r *runner) runLoop() {
	defer r.shutdown()
	defer close(r.runDone)
	defer close(r.out)
	for msg := range r.tr.Messages() {
		if r.closed.Load() {
			break
		}
		stop := r.dispatch(msg)
		if stop {
			break
		}
	}
}

// dispatch routes one message. Returns true when the loop should stop (a
// terminal result for one-shot runners).
func (r *runner) dispatch(msg protocol.Message) bool {
	switch m := msg.(type) {
	case *protocol.SystemMessage:
		if m.Subtype == protocol.SubtypeInit && m.SessionID != "" {
			r.sessionMu.Lock()
			r.sessionID = m.SessionID
			r.sessionMu.Unlock()
		}
		r.forward(m)
		return false
	case *protocol.ControlResponse:
		r.routeResponse(m)
		return false
	case *protocol.ControlRequest:
		go r.handleControlRequest(m)
		return false
	case *protocol.ControlCancel:
		return false
	case *protocol.ResultMessage:
		r.forward(m)
		return r.oneShot
	default:
		r.forward(m)
		return false
	}
}

func (r *runner) forward(m protocol.Message) {
	if r.closed.Load() {
		return
	}
	select {
	case r.out <- m:
	case <-r.ctx.Done():
	}
}

func (r *runner) routeResponse(m *protocol.ControlResponse) {
	body, err := m.Body()
	if err != nil {
		return
	}
	r.pendingMu.Lock()
	ch, ok := r.pending[body.RequestID]
	if ok {
		delete(r.pending, body.RequestID)
	}
	r.pendingMu.Unlock()
	if ok {
		select {
		case ch <- *m:
		default:
		}
	}
}

// handshake sends the initialize control request and captures the response
// (which carries the model list, slash commands, etc.). The agent stream
// proceeds regardless; a handshake failure is non-fatal for plain prompts.
func (r *runner) handshake() error {
	req := protocol.InitializeRequest{
		Subtype:            protocol.ControlInitialize,
		Hooks:              r.opts.Hooks,
		SystemPrompt:       r.opts.SystemPrompt,
		AppendSystemPrompt: r.opts.AppendSystemPrompt,
		Agents:             r.opts.Agents,
		SdkMcpServers:      r.opts.SdkMcpServers,
	}
	if len(req.SdkMcpServers) > 0 {
		req.Capabilities = &protocol.InitializeCapabilities{}
	}
	resp, err := r.sendControlRequest(req, 30*time.Second)
	if err != nil {
		return err
	}
	body, berr := resp.Body()
	if berr != nil || body.Subtype != "success" || len(body.Response) == 0 {
		return nil
	}
	var initResp protocol.InitializeResponse
	if err := json.Unmarshal(body.Response, &initResp); err != nil {
		return nil
	}
	r.initMu.Lock()
	r.initResp = &initResp
	r.initMu.Unlock()
	return nil
}

// sendControlRequest sends a control_request envelope and awaits the matching
// control_response by request_id.
func (r *runner) sendControlRequest(inner any, timeout time.Duration) (protocol.ControlResponse, error) {
	id := newRequestID()
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return protocol.ControlResponse{}, err
	}
	req := protocol.ControlRequest{
		Type:      protocol.TypeControlRequest,
		RequestID: id,
		Request:   innerJSON,
	}
	ch := make(chan protocol.ControlResponse, 1)
	r.pendingMu.Lock()
	r.pending[id] = ch
	r.pendingMu.Unlock()
	defer func() {
		r.pendingMu.Lock()
		delete(r.pending, id)
		r.pendingMu.Unlock()
	}()
	if err := r.tr.WriteJSON(req); err != nil {
		return protocol.ControlResponse{}, err
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return protocol.ControlResponse{}, fmt.Errorf("%w (request %s)", ErrInitializeTimeout, id)
	case <-r.ctx.Done():
		return protocol.ControlResponse{}, r.ctx.Err()
	case <-r.runDone:
		return protocol.ControlResponse{}, ErrSessionClosed
	}
}

// handleControlRequest dispatches a CLI->SDK control request to callbacks.
func (r *runner) handleControlRequest(req *protocol.ControlRequest) {
	switch req.Subtype() {
	case protocol.ControlCanUseTool:
		r.handleCanUseTool(req)
	case protocol.ControlHookCallback:
		r.handleHook(req)
	case protocol.ControlMcpMessage:
		r.handleMcpMessage(req)
	default:
		// Unknown control requests (e.g. elicitation) are acked as success so
		// the CLI is never left waiting on a Deferred with no timeout.
		r.respondSuccess(req.RequestID, map[string]any{})
	}
}

func (r *runner) handleCanUseTool(req *protocol.ControlRequest) {
	var inner protocol.CanUseToolRequest
	if err := json.Unmarshal(req.Request, &inner); err != nil {
		r.respondError(req.RequestID, "decode can_use_tool: "+err.Error())
		return
	}
	if r.opts.CanUseTool == nil {
		// No callback -> deny with a clear reason (mirrors the TS SDK).
		r.respondSuccess(req.RequestID, protocol.DenyTool(inner.ToolUseID, "No permission handler provided"))
		return
	}
	res, err := r.opts.CanUseTool(r.ctx, &inner)
	if err != nil {
		r.respondSuccess(req.RequestID, protocol.DenyTool(inner.ToolUseID, err.Error()))
		return
	}
	var wire protocol.CanUseToolResponse
	if res.Behavior == protocol.PermissionAllow {
		var updated json.RawMessage
		if res.UpdatedInput != nil {
			updated, _ = json.Marshal(res.UpdatedInput)
		} else {
			updated = inner.Input // echo original input
		}
		wire = protocol.AllowTool(inner.ToolUseID, updated)
	} else {
		wire = protocol.DenyTool(inner.ToolUseID, res.Message)
		if res.Interrupt {
			b := true
			wire.Interrupt = &b
		}
	}
	r.respondSuccess(req.RequestID, wire)
}

func (r *runner) handleHook(req *protocol.ControlRequest) {
	var inner protocol.HookCallbackRequest
	if err := json.Unmarshal(req.Request, &inner); err != nil {
		r.respondError(req.RequestID, "decode hook_callback: "+err.Error())
		return
	}
	if r.opts.HookCallback == nil {
		r.respondSuccess(req.RequestID, protocol.HookJSONOutput{})
		return
	}
	var input protocol.HookInput
	if len(inner.HookInput) > 0 {
		_ = json.Unmarshal(inner.HookInput, &input)
	}
	out, err := r.opts.HookCallback(r.ctx, &inner, &input)
	if err != nil {
		r.respondError(req.RequestID, err.Error())
		return
	}
	r.respondSuccess(req.RequestID, out)
}

func (r *runner) handleMcpMessage(req *protocol.ControlRequest) {
	var inner protocol.McpMessageRequest
	if err := json.Unmarshal(req.Request, &inner); err != nil {
		r.respondError(req.RequestID, "decode mcp_message: "+err.Error())
		return
	}
	if r.opts.McpMessageHandler == nil {
		r.respondError(req.RequestID, "SDK MCP server not found: "+inner.ServerName)
		return
	}
	resp, err := r.opts.McpMessageHandler(r.ctx, inner.ServerName, inner.Message)
	if err != nil {
		r.respondError(req.RequestID, err.Error())
		return
	}
	body := map[string]any{}
	if len(resp) > 0 {
		body["mcp_response"] = json.RawMessage(resp)
	} else {
		// Notification: CLI still requires a non-null mcp_response; send an
		// id-less ack to avoid colliding with the MCP initialize request id.
		body["mcp_response"] = map[string]any{"jsonrpc": "2.0", "result": map[string]any{}}
	}
	r.respondSuccess(req.RequestID, body)
}

func (r *runner) respondSuccess(id string, response any) {
	_ = r.tr.WriteJSON(protocol.NewSuccessResponse(id, response))
}

func (r *runner) respondError(id, message string) {
	_ = r.tr.WriteJSON(protocol.NewErrorResponse(id, message))
}

// sendUserText writes a user message (text content) to the CLI stdin.
func (r *runner) sendUserText(text string) error {
	content := []map[string]any{{"type": "text", "text": text}}
	return r.sendUserContent(content)
}

func (r *runner) sendUserContent(content any) error {
	user := map[string]any{
		"type":               protocol.TypeUser,
		"message":            map[string]any{"role": "user", "content": content},
		"parent_tool_use_id": nil,
	}
	return r.tr.WriteJSON(user)
}

func (r *runner) shutdown() {
	r.closeOnce.Do(func() {
		r.closed.Store(true)
		if r.tr != nil {
			_ = r.tr.Close()
		}
	})
}

// lastSessionID returns the session id captured from system/init.
func (r *runner) lastSessionID() string {
	r.sessionMu.RLock()
	defer r.sessionMu.RUnlock()
	return r.sessionID
}

// initializeResponse returns the captured initialize handshake response.
func (r *runner) initializeResponse() *protocol.InitializeResponse {
	r.initMu.RLock()
	defer r.initMu.RUnlock()
	return r.initResp
}

func newRequestID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
