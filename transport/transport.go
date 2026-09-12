// Package transport implements the ProcessTransport that spawns the codebuddy
// CLI as a child process and exchanges JSONL over stdin/stdout.
package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
	"github.com/godeps/codebuddy-agent-sdk-go/runtime"
)

const (
	defaultCloseGraceMs = 2000
	defaultKillGraceMs  = 5000

	// SDKVersion is informational; reported via the CODEBUDDY_CUSTOM_HEADERS
	// User-Agent so Tencent can distinguish SDK traffic.
	SDKVersion = "0.1.0-go"
)

// ErrClosed is returned when writing to a closed transport.
var ErrClosed = errors.New("codebuddy: transport closed")

// Options mirrors the codebuddy launch flags and env. Field names follow the
// TypeScript SDK's ProcessTransportOptions where a counterpart exists.
type Options struct {
	// PathToCLI overrides CLI resolution (else CODEBUDDY_CODE_PATH / PATH).
	PathToCLI string
	// CWD is the child process working directory.
	CWD string
	// Env is extra environment for the child. Nil inherits os.Environ().
	Env map[string]string

	// Model / behavior
	Model         string
	FallbackModel string
	MaxTurns      *int
	Effort        string // minimal|low|medium|high|xhigh|max

	// System prompt
	SystemPrompt       string
	AppendSystemPrompt string

	// Permissions
	PermissionMode             protocol.PermissionMode
	DangerouslySkipPermissions bool
	AllowedTools               []string
	DisallowedTools            []string
	// Tools restricts the built-in toolset ("" disables all, "default" all,
	// or a comma-free list of names). Nil leaves the CLI default untouched.
	Tools *[]string

	// Session
	SessionID      string
	Continue       bool
	Resume         string
	ForkSession    bool
	PersistSession *bool // explicit false -> --no-session-persistence

	// MCP / settings
	McpServers      map[string]protocol.McpServerConfig
	McpConfigFile   string
	StrictMcpConfig bool
	Settings        map[string]json.RawMessage
	SettingsFile    string
	SettingSources  *[]string // nil -> SDK default "none" (isolated)
	AdditionalDirs  []string

	// Output
	IncludePartialMessages bool
	JSONSchema             json.RawMessage // structured output schema

	// Misc
	ReplayUserMessages bool
	RequestTimeoutMs   int
	Verbose            bool
	ExtraArgs          map[string]*string // "--flag" -> value (nil = boolean flag)
	Args               []string           // raw passthrough args

	// DisableBackgroundTasks sets CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS=1 so
	// the CLI never defers work past the final result (correct for one-shot
	// Query usage).
	DisableBackgroundTasks bool

	// CloseGraceMs is how long to wait for a graceful exit before signaling.
	CloseGraceMs int
	// StderrHandler receives each stderr line (diagnostics).
	StderrHandler func(string)
}

// ProcessTransport spawns codebuddy and manages stdin/stdout JSONL I/O.
type ProcessTransport struct {
	opts      Options
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderrBuf bytes.Buffer
	stderrMu  sync.Mutex
	closed    atomic.Bool
	writeMu   sync.Mutex
	msgs      chan protocol.Message
	ArgsBuilt []string // last built argv (diagnostics/tests)
	EnvBuilt  []string
}

// NewProcessTransport creates a transport with the given options.
func NewProcessTransport(opts Options) *ProcessTransport {
	if opts.CloseGraceMs <= 0 {
		opts.CloseGraceMs = defaultCloseGraceMs
	}
	return &ProcessTransport{opts: opts, msgs: make(chan protocol.Message, 64)}
}

// Initialize resolves the executable, builds argv/env, spawns the process,
// and starts the stdout/stderr pumps.
func (t *ProcessTransport) Initialize(ctx context.Context) error {
	path, err := runtime.ResolvePath(t.opts.PathToCLI)
	if err != nil {
		return err
	}
	args := t.buildArgs()
	env := t.buildEnv()
	t.ArgsBuilt = args
	t.EnvBuilt = env

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = t.opts.CWD
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("codebuddy: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codebuddy: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("codebuddy: stderr pipe: %w", err)
	}
	t.cmd, t.stdin, t.stdout = cmd, stdin, stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("codebuddy: spawn %s: %w", path, err)
	}
	go t.readStdout(stdout)
	go t.readStderr(stderr)
	return nil
}

// buildArgs constructs the codebuddy argv, mirroring the TypeScript
// ProcessTransport.buildArgs (--input-format/--output-format/--verbose with
// NO -p: the long-lived stream-json mode).
func (t *ProcessTransport) buildArgs() []string {
	o := t.opts
	args := []string{
		"--input-format=stream-json",
		"--output-format=stream-json",
		"--verbose",
	}
	if o.JSONSchema != nil {
		args = append(args, "--json-schema", string(o.JSONSchema))
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.FallbackModel != "" {
		args = append(args, "--fallback-model", o.FallbackModel)
	}
	if o.MaxTurns != nil {
		args = append(args, "--max-turns", strconv.Itoa(*o.MaxTurns))
	}
	if o.RequestTimeoutMs > 0 {
		args = append(args, "--request-timeout-ms", strconv.Itoa(o.RequestTimeoutMs))
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", string(o.PermissionMode))
	}
	if o.DangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if len(o.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, o.AllowedTools...)
	}
	if len(o.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, o.DisallowedTools...)
	}
	if o.Tools != nil {
		args = append(args, "--tools", strings.Join(*o.Tools, ","))
	}
	if o.SessionID != "" {
		args = append(args, "--session-id", o.SessionID)
	}
	if o.Continue {
		args = append(args, "--continue")
	}
	if o.Resume != "" {
		args = append(args, "--resume", o.Resume)
	}
	if o.ForkSession {
		args = append(args, "--fork-session")
	}
	if o.PersistSession != nil && !*o.PersistSession {
		args = append(args, "--no-session-persistence")
	}
	if o.McpConfigFile != "" {
		args = append(args, "--mcp-config", o.McpConfigFile)
	} else if len(o.McpServers) > 0 {
		mcpJSON, _ := json.Marshal(map[string]any{"mcpServers": o.McpServers})
		args = append(args, "--mcp-config", string(mcpJSON))
	}
	if o.StrictMcpConfig {
		args = append(args, "--strict-mcp-config")
	}
	// Settings: SDK default is full isolation (no filesystem settings).
	if o.SettingsFile != "" {
		args = append(args, "--settings", o.SettingsFile)
	} else if len(o.Settings) > 0 {
		s, _ := json.Marshal(o.Settings)
		args = append(args, "--settings", string(s))
	}
	if o.SettingSources != nil {
		if len(*o.SettingSources) == 0 {
			args = append(args, "--setting-sources", "none")
		} else {
			args = append(args, "--setting-sources", strings.Join(*o.SettingSources, ","))
		}
	} else {
		args = append(args, "--setting-sources", "none")
	}
	for _, dir := range o.AdditionalDirs {
		args = append(args, "--add-dir", dir)
	}
	if o.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	if o.SystemPrompt != "" {
		args = append(args, "--system-prompt", o.SystemPrompt)
	}
	if o.AppendSystemPrompt != "" {
		args = append(args, "--append-system-prompt", o.AppendSystemPrompt)
	}
	if o.Effort != "" {
		args = append(args, "--effort", o.Effort)
	}
	if o.ReplayUserMessages {
		args = append(args, "--replay-user-messages")
	}
	for name, val := range o.ExtraArgs {
		args = append(args, "--"+name)
		if val != nil {
			args = append(args, *val)
		}
	}
	args = append(args, o.Args...)
	return args
}

// buildEnv builds the child environment: base env + SDK markers.
func (t *ProcessTransport) buildEnv() []string {
	base := map[string]string{}
	if t.opts.Env == nil {
		for _, kv := range os.Environ() {
			if i := strings.IndexByte(kv, '='); i >= 0 {
				base[kv[:i]] = kv[i+1:]
			}
		}
	} else {
		for k, v := range t.opts.Env {
			base[k] = v
		}
	}
	// SDK markers (mirror the TS transport).
	if base["CODEBUDDY_CODE_ENTRYPOINT"] == "" {
		base["CODEBUDDY_CODE_ENTRYPOINT"] = "sdk-go"
	}
	// An auto-update mid-session would restart the CLI and break the pipes.
	base["DISABLE_AUTOUPDATER"] = "1"
	// Auto-memory triggers tool_use that needs permission handling; hosts
	// without a CanUseTool callback would corrupt conversation state.
	if base["CODEBUDDY_DISABLE_AUTO_MEMORY"] == "" {
		base["CODEBUDDY_DISABLE_AUTO_MEMORY"] = "1"
	}
	if t.opts.DisableBackgroundTasks {
		base["CODEBUDDY_CODE_DISABLE_BACKGROUND_TASKS"] = "1"
	}
	// User-Agent header so the backend can attribute SDK traffic.
	ua := "User-Agent: CodeBuddy Agent SDK-Go/" + SDKVersion
	if existing := base["CODEBUDDY_CUSTOM_HEADERS"]; existing != "" {
		base["CODEBUDDY_CUSTOM_HEADERS"] = ua + "\n" + existing
	} else {
		base["CODEBUDDY_CUSTOM_HEADERS"] = ua
	}
	env := make([]string, 0, len(base))
	for k, v := range base {
		env = append(env, k+"="+v)
	}
	return env
}

// WriteJSON marshals v and writes it as one JSON line to the CLI stdin.
func (t *ProcessTransport) WriteJSON(v any) error {
	if t.closed.Load() {
		return ErrClosed
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.stdin == nil {
		return ErrClosed
	}
	_, err = t.stdin.Write(append(data, '\n'))
	return err
}

// Messages returns the channel of decoded stdout messages. The channel closes
// when the CLI exits or stdout ends.
func (t *ProcessTransport) Messages() <-chan protocol.Message {
	return t.msgs
}

// StderrTail returns accumulated stderr output (capped).
func (t *ProcessTransport) StderrTail() string {
	t.stderrMu.Lock()
	defer t.stderrMu.Unlock()
	s := t.stderrBuf.String()
	if len(s) > 2000 {
		s = s[len(s)-2000:]
	}
	return s
}

// PID returns the child process PID, or 0 if not started.
func (t *ProcessTransport) PID() int {
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Pid
	}
	return 0
}

// Close performs graceful shutdown: close stdin -> CloseGraceMs -> SIGINT ->
// KillGraceMs -> SIGKILL.
func (t *ProcessTransport) Close() error {
	if !t.closed.CompareAndSwap(false, true) {
		return nil
	}
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = t.cmd.Wait()
			close(done)
		}()
		grace := time.Duration(t.opts.CloseGraceMs) * time.Millisecond
		select {
		case <-done:
		case <-time.After(grace):
			_ = t.cmd.Process.Signal(os.Interrupt)
			select {
			case <-done:
			case <-time.After(defaultKillGraceMs * time.Millisecond):
				_ = t.cmd.Process.Kill()
				<-done
			}
		}
	}
	return nil
}

// readStdout pumps stdout lines, parses each as a Message, and sends to msgs.
// The channel is closed here when stdout ends.
func (t *ProcessTransport) readStdout(r io.Reader) {
	defer close(t.msgs)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		msg, err := protocol.ParseMessage(line)
		if err != nil {
			continue // ignore non-JSON lines (verbose noise)
		}
		if t.closed.Load() {
			return
		}
		t.msgs <- msg // block on full channel (back-pressure)
	}
}

// readStderr pumps stderr into a capped buffer and forwards to the handler.
func (t *ProcessTransport) readStderr(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		t.stderrMu.Lock()
		if t.stderrBuf.Len() > 64*1024 {
			t.stderrBuf.Reset()
		}
		t.stderrBuf.WriteString(line)
		t.stderrBuf.WriteByte('\n')
		t.stderrMu.Unlock()
		if t.opts.StderrHandler != nil {
			t.opts.StderrHandler(line)
		}
	}
}
