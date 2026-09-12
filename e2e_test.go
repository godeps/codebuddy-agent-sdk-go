package codebuddy_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
	"github.com/godeps/codebuddy-agent-sdk-go/auth"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

// requireRealCLI skips unless a logged-in codebuddy CLI is available.
// Enable explicitly with CODEBUDDY_SDK_E2E=1.
func requireRealCLI(t *testing.T) string {
	t.Helper()
	if os.Getenv("CODEBUDDY_SDK_E2E") != "1" {
		t.Skip("set CODEBUDDY_SDK_E2E=1 to run real-CLI integration tests")
	}
	path, err := exec.LookPath("codebuddy")
	if err != nil {
		if p, err2 := exec.LookPath("cbc"); err2 == nil {
			path = p
		} else {
			t.Skip("codebuddy CLI not on PATH")
		}
	}
	if !auth.LoggedIn() && os.Getenv("CODEBUDDY_API_KEY") == "" {
		t.Skip("codebuddy CLI not logged in and CODEBUDDY_API_KEY unset")
	}
	return path
}

func TestE2E_QueryOnce(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithPermissionMode(protocol.PermissionBypassPermissions).
		WithTools([]string{}) // no tools: pure text round-trip
	res, err := codebuddy.QueryOnce(ctx, "Reply with exactly: SDK-E2E-OK", opts)
	if err != nil {
		t.Fatalf("QueryOnce: %v", err)
	}
	if res.SessionID == "" {
		t.Error("missing session id")
	}
	if res.Text == "" {
		t.Error("empty result text")
	}
	t.Logf("result=%q turns=%d cost=%.4f session=%s", res.Text, res.NumTurns, res.TotalCostUSD, res.SessionID)
	if !strings.Contains(res.Text, "SDK-E2E-OK") {
		t.Errorf("model did not follow instruction: %q", res.Text)
	}
}

func TestE2E_Session_MultiTurn(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	opts := codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithPermissionMode(protocol.PermissionBypassPermissions).
		WithTools([]string{})
	sess, err := codebuddy.NewSession(ctx, opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	if err := sess.Send("Remember the number 73. Reply exactly: STORED"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	res1, _, err := sess.ReceiveResponse(90 * time.Second)
	if err != nil {
		t.Fatalf("ReceiveResponse 1: %v", err)
	}
	if !res1.IsSuccess() {
		t.Fatalf("turn 1: %s", res1.Subtype)
	}

	if err := sess.Send("What number did I ask you to remember? Reply with only the number."); err != nil {
		t.Fatalf("Send 2: %v", err)
	}
	res2, _, err := sess.ReceiveResponse(90 * time.Second)
	if err != nil {
		t.Fatalf("ReceiveResponse 2: %v", err)
	}
	if !strings.Contains(res2.Result, "73") {
		t.Errorf("session lost context: %q", res2.Result)
	}
	t.Logf("turn2=%q", res2.Result)
}
