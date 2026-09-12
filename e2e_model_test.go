package codebuddy_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

// TestE2E_Models verifies the initialize handshake exposes the account's
// model list (verified manually against CLI 2.150: response carries
// models[] + currentModelId).
func TestE2E_Models(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
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

	models, current := sess.Models()
	if len(models) == 0 {
		t.Fatal("initialize handshake returned no model list")
	}
	ids := make([]string, 0, len(models))
	foundCurrent := false
	for _, m := range models {
		ids = append(ids, m.ID)
		if m.ID == current {
			foundCurrent = true
		}
	}
	if current == "" {
		t.Error("currentModelId empty")
	} else if !foundCurrent {
		t.Errorf("currentModelId %q not in model list %v", current, ids)
	}
	t.Logf("current=%s; models=%s", current, strings.Join(ids, ","))

	if cmds := sess.SlashCommands(); len(cmds) == 0 {
		t.Error("initialize handshake returned no slash commands")
	}
}

// TestE2E_SetModel verifies a mid-session model switch: the CLI confirms
// {model, previous_model} and the NEXT turn actually runs on the new model.
// SetModel requires an established session (after the first turn).
func TestE2E_SetModel(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
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

	models, current := sess.Models()
	if len(models) < 2 {
		t.Skipf("need >=2 models to test switching, got %d", len(models))
	}
	// Pick a target different from the current model.
	target := ""
	for _, m := range models {
		if m.ID != current {
			target = m.ID
			break
		}
	}
	if target == "" {
		t.Skip("no alternate model available")
	}

	// Turn 1 establishes the session (set_model before this fails with
	// "Session not found: current" — see ErrSessionNotEstablished).
	if err := sess.Send("reply with one word: OK"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, _, err := sess.ReceiveResponse(90 * time.Second); err != nil {
		t.Fatalf("ReceiveResponse 1: %v", err)
	}

	resp, err := sess.SetModel(target)
	if err != nil {
		t.Fatalf("SetModel(%s): %v", target, err)
	}
	if resp.Model != target {
		t.Errorf("SetModel confirmed model %q, want %q", resp.Model, target)
	}
	if resp.PreviousModel != current {
		t.Logf("previous_model=%q (handshake current was %q)", resp.PreviousModel, current)
	}
	t.Logf("switched %s -> %s", resp.PreviousModel, resp.Model)

	// Turn 2 must run on the new model: watch the assistant message's model
	// field on the stream.
	if err := sess.Send("reply with one word: OK"); err != nil {
		t.Fatalf("Send 2: %v", err)
	}
	var seenModel string
	deadline := time.After(90 * time.Second)
	for {
		select {
		case m, ok := <-sess.Stream():
			if !ok {
				t.Fatal("stream closed before result")
			}
			if a, ok := m.(*protocol.AssistantMessage); ok && a.Message.Model != "" {
				seenModel = a.Message.Model
			}
			if _, ok := m.(*protocol.ResultMessage); ok {
				goto done
			}
		case <-deadline:
			t.Fatal("timeout waiting for turn 2 result")
		}
	}
done:
	if seenModel != target {
		t.Errorf("turn 2 ran on model %q, want %q", seenModel, target)
	}
}

// TestE2E_SetModel_BeforeTurn asserts the documented guard: switching before
// the session is established returns ErrSessionNotEstablished instead of a
// confusing CLI error.
func TestE2E_SetModel_BeforeTurn(t *testing.T) {
	cli := requireRealCLI(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sess, err := codebuddy.NewSession(ctx, codebuddy.NewOptions().
		WithPathToCLI(cli).
		WithCWD(t.TempDir()).
		WithPermissionMode(protocol.PermissionBypassPermissions).
		WithTools([]string{}))
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer sess.Close()

	if _, err := sess.SetModel("glm-5.3-flash"); err == nil {
		t.Error("expected ErrSessionNotEstablished before first turn, got nil")
	} else if !errors.Is(err, codebuddy.ErrSessionNotEstablished) {
		t.Errorf("expected ErrSessionNotEstablished, got: %v", err)
	}
}
