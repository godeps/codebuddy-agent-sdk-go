package codebuddy_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
	cbprotocol "github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

// ---------------------------------------------------------------------------
// Hooks (CLI -> SDK hook_callback control requests)
// ---------------------------------------------------------------------------

func TestE2E_Hooks_PreToolUse_Observes(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	var mu sync.Mutex
	var seen []string

	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithPermissionMode(cbprotocol.PermissionBypassPermissions).
		WithHooks(map[cbprotocol.HookEvent][]cbprotocol.HookCallbackMatcher{
			cbprotocol.HookPreToolUse: {{
				Matcher: "Bash",
				Hooks:   []cbprotocol.HookCallback{{Type: "callback", CallbackID: "audit-1"}},
			}},
		}).
		WithHookCallback(func(ctx context.Context, req *cbprotocol.HookCallbackRequest, in *cbprotocol.HookInput) (cbprotocol.HookJSONOutput, error) {
			mu.Lock()
			seen = append(seen, string(in.HookEventName)+":"+in.ToolName+":"+req.CallbackID)
			mu.Unlock()
			return cbprotocol.HookJSONOutput{}, nil // empty = proceed
		})

	res, err := codebuddy.QueryOnce(ctx, "Run exactly this bash command: echo HOOK-E2E-OK", opts)
	if err != nil {
		t.Fatalf("QueryOnce: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatalf("hook callback never fired (result=%q)", res.Text)
	}
	for _, s := range got {
		if !strings.HasPrefix(s, "PreToolUse:Bash:") {
			t.Errorf("unexpected hook event: %q", s)
		}
	}
	t.Logf("hook fired %d time(s): %v", len(got), got)
}

func TestE2E_Hooks_PreToolUse_Blocks(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	dir := t.TempDir()
	canary := filepath.Join(dir, "canary.txt")

	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(dir).
		WithPermissionMode(cbprotocol.PermissionBypassPermissions).
		WithHooks(map[cbprotocol.HookEvent][]cbprotocol.HookCallbackMatcher{
			cbprotocol.HookPreToolUse: {{
				Matcher: "", // match ALL tools: the model may fall back from Bash to
				// Write/Edit after a block, so the policy must cover everything.
				Hooks: []cbprotocol.HookCallback{{Type: "callback", CallbackID: "blocker"}},
			}},
		}).
		WithHookCallback(func(ctx context.Context, req *cbprotocol.HookCallbackRequest, in *cbprotocol.HookInput) (cbprotocol.HookJSONOutput, error) {
			return cbprotocol.HookJSONOutput{
				Decision: "block",
				Reason:   "all tools are disabled by E2E policy; do not retry, reply exactly BLOCKED-BY-HOOK",
			}, nil
		})

	res, err := codebuddy.QueryOnce(ctx,
		"Create a file named canary.txt in the current directory containing exactly CANARY. If every attempt is blocked, reply exactly BLOCKED-BY-HOOK and stop.", opts)
	if err != nil {
		t.Fatalf("QueryOnce: %v", err)
	}
	if _, statErr := os.Stat(canary); statErr == nil {
		t.Errorf("hook block did NOT prevent file creation: %s exists", canary)
	} else {
		t.Logf("canary file correctly absent (hook blocked the tool)")
	}
	t.Logf("result=%q", res.Text)
}

// ---------------------------------------------------------------------------
// In-process SDK MCP server (CLI -> SDK mcp_message control requests)
// ---------------------------------------------------------------------------

func TestE2E_SdkMCP_ToolCall(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	var mu sync.Mutex
	methods := map[string]int{}

	const (
		serverName = "saker-e2e"
		magic      = "E2E-MCP-7F3A"
	)

	handler := func(ctx context.Context, server string, msg json.RawMessage) (json.RawMessage, error) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(msg, &req); err != nil {
			return nil, err
		}
		mu.Lock()
		methods[req.Method]++
		mu.Unlock()

		rpcResult := func(result any) (json.RawMessage, error) {
			return json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(req.ID), "result": result})
		}
		switch req.Method {
		case "initialize":
			return rpcResult(map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]any{"name": serverName, "version": "0.0.1"},
			})
		case "tools/list":
			return rpcResult(map[string]any{"tools": []map[string]any{{
				"name":        "echo_magic",
				"description": "Returns a fixed magic string. Call this tool when asked for the magic value.",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
			}}})
		case "tools/call":
			return rpcResult(map[string]any{
				"content": []map[string]any{{"type": "text", "text": magic}},
				"isError": false,
			})
		default:
			return nil, nil // notifications
		}
	}

	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithPermissionMode(cbprotocol.PermissionBypassPermissions).
		WithSdkMcpServers(serverName).
		WithMcpServers(map[string]cbprotocol.McpServerConfig{
			serverName: cbprotocol.NewMcpSdkConfig(serverName),
		}).
		WithMcpMessageHandler(handler)

	res, err := codebuddy.QueryOnce(ctx,
		"Call the echo_magic tool on the saker-e2e MCP server exactly once, then reply with only the value it returned.", opts)
	if err != nil {
		t.Fatalf("QueryOnce: %v", err)
	}

	mu.Lock()
	calls := map[string]int{}
	for k, v := range methods {
		calls[k] = v
	}
	mu.Unlock()
	t.Logf("mcp methods seen: %v", calls)

	if calls["tools/call"] == 0 {
		t.Fatalf("echo_magic was never called via mcp_message (methods=%v, result=%q)", calls, res.Text)
	}
	if !strings.Contains(res.Text, magic) {
		t.Errorf("result does not contain the tool output %q: %q", magic, res.Text)
	}
}

// ---------------------------------------------------------------------------
// Interrupt
// ---------------------------------------------------------------------------

func TestE2E_Interrupt(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithPermissionMode(cbprotocol.PermissionBypassPermissions).
		WithPartialMessages(true). // interrupt as early as possible
		WithTools([]string{})

	sess, err := codebuddy.NewSession(ctx, opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	// A long generation task so the interrupt lands mid-turn.
	if err := sess.Send("Count from 1 to 400, one number per line. Do not stop early."); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Wait for the first partial output, then interrupt mid-generation.
	interrupted := false
	deadline := time.After(60 * time.Second)
	for !interrupted {
		select {
		case m, ok := <-sess.Stream():
			if !ok {
				t.Fatal("stream closed before first output")
			}
			switch m.(type) {
			case *cbprotocol.PartialAssistantMessage, *cbprotocol.AssistantMessage:
				if err := sess.Interrupt(); err != nil {
					t.Logf("Interrupt returned error (may still abort): %v", err)
				}
				interrupted = true
			}
		case <-deadline:
			t.Fatal("no assistant output within 60s; cannot test interrupt")
		}
	}

	res, _, err := sess.ReceiveResponse(120 * time.Second)
	if err != nil {
		t.Fatalf("no result after interrupt: %v", err)
	}
	t.Logf("post-interrupt result: subtype=%s is_error=%v turns=%d", res.Subtype, res.IsError, res.NumTurns)
	// The turn must terminate early: either a non-success subtype or a
	// truncated count (nowhere near 400 lines).
	if res.Subtype == "success" && !res.IsError && strings.Count(res.Result, "\n") > 300 {
		t.Errorf("interrupt appears ineffective: got %d lines", strings.Count(res.Result, "\n")+1)
	}
}

// ---------------------------------------------------------------------------
// Rewind (workspace file rollback)
// ---------------------------------------------------------------------------

func TestE2E_Rewind(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	dir := t.TempDir()
	canary := filepath.Join(dir, "canary.txt")

	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(dir).
		WithPermissionMode(cbprotocol.PermissionBypassPermissions)

	sess, err := codebuddy.NewSession(ctx, opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	if err := sess.Send("Create a file named canary.txt in the current directory containing exactly HELLO, using your Write file tool (not bash). Then reply DONE."); err != nil {
		t.Fatalf("Send: %v", err)
	}
	res1, seen, err := sess.ReceiveResponse(180 * time.Second)
	if err != nil {
		t.Fatalf("ReceiveResponse: %v", err)
	}
	if !res1.IsSuccess() {
		t.Fatalf("turn 1 failed: %s", res1.Subtype)
	}

	// Collect the file-history snapshot anchor.
	var anchor string
	for _, m := range seen {
		if snap, ok := m.(*cbprotocol.FileHistorySnapshot); ok && snap.MessageID != "" {
			anchor = snap.MessageID
		}
	}
	if _, err := os.Stat(canary); err != nil {
		t.Skipf("model did not create canary file (%v); cannot test rewind", err)
	}
	if anchor == "" {
		t.Skip("CLI emitted no file-history-snapshot; rewind anchors unavailable")
	}

	// Dry run first.
	dry, err := sess.Rewind(anchor, codebuddy.RewindScopeCode, true)
	if err != nil {
		t.Fatalf("Rewind dry-run: %v", err)
	}
	t.Logf("dry-run: canRewind=%v files=%v", dry.CanRewind, dry.FilesChanged)
	if _, err := os.Stat(canary); err != nil {
		t.Error("dry-run mutated the workspace: canary gone")
	}
	if dry.Error != "" {
		t.Skipf("CLI reports rewind unavailable: %s", dry.Error)
	}

	// Real rewind.
	real, err := sess.Rewind(anchor, codebuddy.RewindScopeCode, false)
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	t.Logf("rewind: canRewind=%v files=%v error=%q", real.CanRewind, real.FilesChanged, real.Error)
	if real.Error == "" && real.CanRewind {
		if _, err := os.Stat(canary); err == nil {
			t.Errorf("canary.txt still exists after rewinding to pre-turn snapshot")
		} else {
			t.Logf("canary.txt correctly removed by rewind")
		}
	} else {
		t.Skipf("CLI declined the rewind: %s", real.Error)
	}
}
