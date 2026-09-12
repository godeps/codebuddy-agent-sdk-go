package codebuddy_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
	"github.com/godeps/codebuddy-agent-sdk-go/auth"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

// buildFakeCLI compiles internal/fakecli into a temp binary and returns its path.
func buildFakeCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-codebuddy")
	cmd := exec.Command("go", "build", "-o", bin, "./internal/fakecli")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakecli: %v\n%s", err, out)
	}
	return bin
}

func fakeOpts(cli string, t *testing.T) *codebuddy.Options {
	// Auth: use an API key so checkAuth passes regardless of host login state.
	return codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithAuth(auth.APIKeyAuth{APIKey: "fake-key"})
}

func TestQuery_FakeCLI(t *testing.T) {
	cli := buildFakeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	msgs, err := codebuddy.Query(ctx, "hello", fakeOpts(cli, t))
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	var gotInit, gotAssistant, gotResult bool
	for m := range msgs {
		switch v := m.(type) {
		case *protocol.SystemMessage:
			if v.Subtype == protocol.SubtypeInit {
				gotInit = true
				if v.SessionID == "" {
					t.Error("init missing session_id")
				}
			}
		case *protocol.AssistantMessage:
			gotAssistant = true
			if len(v.Message.Content) == 0 || v.Message.Content[0].Text == "" {
				t.Error("assistant message without text")
			}
		case *protocol.ResultMessage:
			gotResult = true
			if !v.IsSuccess() {
				t.Errorf("result not success: %s", v.Subtype)
			}
			if v.Result == "" {
				t.Error("empty result text")
			}
		}
	}
	if !gotInit || !gotAssistant || !gotResult {
		t.Fatalf("missing messages: init=%v assistant=%v result=%v", gotInit, gotAssistant, gotResult)
	}
}

func TestQueryOnce_FakeCLI(t *testing.T) {
	cli := buildFakeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts := fakeOpts(cli, t).WithModel("fake-model").WithMaxTurns(2)
	res, err := codebuddy.QueryOnce(ctx, "hello", opts)
	if err != nil {
		t.Fatalf("QueryOnce: %v", err)
	}
	if !strings.Contains(res.Text, "fake codebuddy") {
		t.Errorf("unexpected text: %q", res.Text)
	}
	if res.SessionID == "" {
		t.Error("no session id")
	}
	if res.NumTurns != 1 {
		t.Errorf("num_turns = %d, want 1", res.NumTurns)
	}
}

func TestQuery_CanUseTool_Allow(t *testing.T) {
	cli := buildFakeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	opts := fakeOpts(cli, t).
		WithPermissionMode(protocol.PermissionDefault).
		WithCanUseTool(func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error) {
			if req.ToolName != "Bash" {
				t.Errorf("unexpected tool %q", req.ToolName)
			}
			return protocol.Allow(nil), nil
		})
	opts.Env = map[string]string{"FAKE_TOOL_PROMPT": "1", "PATH": os.Getenv("PATH")}

	msgs, err := codebuddy.Query(ctx, "run a command", opts)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var sawToolUse bool
	for m := range msgs {
		if a, ok := m.(*protocol.AssistantMessage); ok {
			for _, b := range a.Message.Content {
				if b.Type == "tool_use" && b.Name == "Bash" {
					sawToolUse = true
				}
			}
		}
	}
	if !sawToolUse {
		t.Error("never saw tool_use block")
	}
}

func TestQuery_CanUseTool_Deny(t *testing.T) {
	cli := buildFakeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	denied := make(chan struct{})
	opts := fakeOpts(cli, t).
		WithPermissionMode(protocol.PermissionDefault).
		WithCanUseTool(func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error) {
			close(denied)
			return protocol.Deny("not allowed in test"), nil
		})
	opts.Env = map[string]string{"FAKE_TOOL_PROMPT": "1", "PATH": os.Getenv("PATH")}

	msgs, err := codebuddy.Query(ctx, "run a command", opts)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for range msgs {
	}
	select {
	case <-denied:
	case <-time.After(2 * time.Second):
		t.Error("CanUseTool callback never invoked")
	}
}

func TestSession_MultiTurn(t *testing.T) {
	cli := buildFakeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	sess, err := codebuddy.NewSession(ctx, fakeOpts(cli, t))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	for i := 0; i < 2; i++ {
		if err := sess.Send("turn"); err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
		res, _, err := sess.ReceiveResponse(20 * time.Second)
		if err != nil {
			t.Fatalf("ReceiveResponse #%d: %v", i, err)
		}
		if !res.IsSuccess() {
			t.Errorf("turn #%d result: %s", i, res.Subtype)
		}
	}
}

func TestSession_SetPermissionMode(t *testing.T) {
	cli := buildFakeCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := codebuddy.NewSession(ctx, fakeOpts(cli, t))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()
	if err := sess.SetPermissionMode(protocol.PermissionAcceptEdits); err != nil {
		t.Fatalf("SetPermissionMode: %v", err)
	}
}

func TestOptions_Builders(t *testing.T) {
	o := codebuddy.NewOptions().
		WithModel("m").WithMaxTurns(3).WithEffort("high").
		WithAllowedTools([]string{"Read"}).WithDisallowedTools([]string{"Bash"}).
		WithResume("sid").WithForkSession(true).WithPersistSession(false).
		WithSettingSources("user", "project").WithAddDir("/tmp/x").
		WithAppendSystemPrompt("be nice").WithPartialMessages(true)
	if o.Model != "m" || *o.MaxTurns != 3 || o.Effort != "high" {
		t.Error("builder mismatch")
	}
	if o.Resume != "sid" || !o.ForkSession || o.PersistSession == nil || *o.PersistSession {
		t.Error("session builder mismatch")
	}
	if o.SettingSources == nil || len(*o.SettingSources) != 2 {
		t.Error("setting sources mismatch")
	}
}

func TestProtocol_ParseWireShapes(t *testing.T) {
	// Real captured lines from codebuddy 2.150 (trimmed).
	initLine := `{"type":"system","subtype":"init","uuid":"u1","session_id":"s1","apiKeySource":"copilot.tencent.com","cwd":"/tmp","tools":["Bash"],"mcp_servers":[],"model":"deepseek-v4.1-flash","permissionMode":"bypassPermissions","slash_commands":["help"],"output_style":"default"}`
	var sys protocol.SystemMessage
	if err := json.Unmarshal([]byte(initLine), &sys); err != nil {
		t.Fatalf("system/init: %v", err)
	}
	if sys.Subtype != "init" || sys.SessionID != "s1" || sys.Model != "deepseek-v4.1-flash" {
		t.Errorf("bad system parse: %+v", sys)
	}
	if sys.PermissionMode != protocol.PermissionBypassPermissions {
		t.Errorf("bad permissionMode: %v", sys.PermissionMode)
	}

	resultLine := `{"type":"result","subtype":"success","is_error":false,"result":"PONG","uuid":"u2","session_id":"s1","duration_ms":2255,"duration_api_ms":2254,"num_turns":2,"total_cost_usd":0,"usage":{"input_tokens":6231,"output_tokens":11},"permission_denials":[],"modelUsage":{"deepseek-v4.1-flash":{"inputTokens":0,"outputTokens":11,"contextWindow":1000000,"maxOutputTokens":128000}}}`
	msg, err := protocol.ParseMessage([]byte(resultLine))
	if err != nil {
		t.Fatalf("ParseMessage: %v", err)
	}
	res, ok := msg.(*protocol.ResultMessage)
	if !ok {
		t.Fatalf("wrong type %T", msg)
	}
	if !res.IsSuccess() || res.Result != "PONG" || res.NumTurns != 2 {
		t.Errorf("bad result parse: %+v", res)
	}
	if mu, ok := res.ModelUsage["deepseek-v4.1-flash"]; !ok || mu.ContextWindow != 1000000 {
		t.Errorf("bad modelUsage parse: %+v", res.ModelUsage)
	}

	asstLine := `{"type":"assistant","uuid":"u3","session_id":"s1","message":{"id":"m1","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"x","usage":{"input_tokens":1,"output_tokens":2}},"parent_tool_use_id":null}`
	msg, err = protocol.ParseMessage([]byte(asstLine))
	if err != nil {
		t.Fatalf("ParseMessage assistant: %v", err)
	}
	asst, ok := msg.(*protocol.AssistantMessage)
	if !ok || len(asst.Message.Content) != 1 || asst.Message.Content[0].Text != "hi" {
		t.Errorf("bad assistant parse: %+v", msg)
	}

	snapLine := `{"type":"file-history-snapshot","messageId":"m9","timestamp":1789221585274,"isSnapshotUpdate":false,"snapshot":{"messageId":"m9","trackedFileBackups":{}}}`
	msg, err = protocol.ParseMessage([]byte(snapLine))
	if err != nil {
		t.Fatalf("ParseMessage snapshot: %v", err)
	}
	if _, ok := msg.(*protocol.FileHistorySnapshot); !ok {
		t.Errorf("wrong snapshot type %T", msg)
	}
}
