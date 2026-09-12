package codebuddy_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

func TestE2E_CanUseTool_Deny(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var mu sync.Mutex
	var prompted []string
	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithPermissionMode(protocol.PermissionDefault).
		WithCanUseTool(func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error) {
			mu.Lock()
			prompted = append(prompted, req.ToolName)
			mu.Unlock()
			return protocol.Deny("denied by SDK test"), nil
		})

	res, err := codebuddy.QueryOnce(ctx,
		"Run the bash command `echo canary`. If it is blocked, just say BLOCKED.", opts)
	if err != nil {
		t.Fatalf("QueryOnce: %v", err)
	}
	mu.Lock()
	gotPrompt := len(prompted) > 0
	names := strings.Join(prompted, ",")
	mu.Unlock()
	if !gotPrompt {
		t.Skip("CLI did not route a permission prompt (auto-denied without callback?)")
	}
	t.Logf("prompted tools: %s; final=%q", names, res.Text)
	// The command must NOT have executed successfully: result mentions BLOCKED
	// or the denial reason; canary output would indicate a leak-through.
	if strings.Contains(res.Text, "canary") && !strings.Contains(res.Text, "BLOCKED") {
		t.Errorf("denied command appears to have run: %q", res.Text)
	}
}

func TestE2E_CanUseTool_Allow(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	dir := t.TempDir()
	var mu sync.Mutex
	var allowed bool
	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(dir).
		WithPermissionMode(protocol.PermissionDefault).
		WithCanUseTool(func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error) {
			mu.Lock()
			allowed = true
			mu.Unlock()
			return protocol.Allow(nil), nil
		})

	res, err := codebuddy.QueryOnce(ctx,
		"Run exactly this bash command: echo E2E-ALLOW-OK", opts)
	if err != nil {
		t.Fatalf("QueryOnce: %v", err)
	}
	mu.Lock()
	wasAllowed := allowed
	mu.Unlock()
	if !wasAllowed {
		t.Skip("no permission prompt routed (allowedTools may auto-approve)")
	}
	t.Logf("final=%q", res.Text)
	if !strings.Contains(res.Text, "E2E-ALLOW-OK") && res.IsError {
		t.Errorf("allow path failed: %q (%s)", res.Text, res.Subtype)
	}
}
