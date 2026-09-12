package codebuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

// Session is a long-lived CodeBuddy session: one CLI child process serving
// multiple turns. It mirrors the TypeScript unstable_v2_createSession API.
//
// A Session keeps reading the agent stream in the background; use Stream to
// receive events, Send to post new user turns, and the control methods
// (Interrupt, SetPermissionMode, SetModel, Rewind) to steer the session.
// Close tears the process down.
type Session struct {
	r        *runner
	events   chan protocol.Message
	sendMu   sync.Mutex
	closedCh chan struct{}
	closeMu  sync.Once
}

// NewSession starts a session WITHOUT sending a prompt. The CLI process
// spawns and the initialize handshake completes before returning; the first
// turn starts on the first Send. Background tasks remain enabled (events
// arrive across turns on Stream).
func NewSession(ctx context.Context, opts *Options) (*Session, error) {
	if opts == nil {
		opts = NewOptions()
	}
	if err := checkAuth(opts); err != nil {
		return nil, err
	}
	disable := false
	if opts.DisableBackgroundTasks == nil {
		opts.DisableBackgroundTasks = &disable
	}
	r, err := newRunner(ctx, opts, false)
	if err != nil {
		return nil, err
	}
	s := &Session{
		r:        r,
		events:   make(chan protocol.Message, 256),
		closedCh: make(chan struct{}),
	}
	// Tee the runner output into the session event channel; the runner's out
	// channel is consumed here for the session's lifetime.
	go func() {
		defer close(s.events)
		for m := range r.out {
			select {
			case s.events <- m:
			case <-s.closedCh:
				return
			}
		}
	}()
	return s, nil
}

// Send posts a user turn (text). Events for the turn (and any background
// tasks) arrive on Stream. Safe for concurrent use; turns are serialized on
// the CLI side.
func (s *Session) Send(text string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	select {
	case <-s.closedCh:
		return ErrSessionClosed
	default:
	}
	return s.r.sendUserText(text)
}

// SendContent posts a user turn with structured content blocks (text/image).
// Each block follows the wire content-block schema, e.g.
// {"type":"text","text":"..."} or
// {"type":"image","source":{"type":"base64","media_type":"image/png","data":"..."}}.
func (s *Session) SendContent(blocks []map[string]any) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	select {
	case <-s.closedCh:
		return ErrSessionClosed
	default:
	}
	return s.r.sendUserContent(blocks)
}

// Stream returns the channel of agent messages. It stays open across turns
// and closes when the session ends. Callers that only care about one turn
// should use ReceiveResponse, which stops at the next ResultMessage.
func (s *Session) Stream() <-chan protocol.Message {
	return s.events
}

// ReceiveResponse reads Stream until the next terminal ResultMessage and
// returns it together with every message seen along the way. Background task
// events arriving after the result remain queued for the next read.
func (s *Session) ReceiveResponse(timeout time.Duration) (*protocol.ResultMessage, []protocol.Message, error) {
	var seen []protocol.Message
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case m, ok := <-s.events:
			if !ok {
				return nil, seen, ErrSessionClosed
			}
			seen = append(seen, m)
			if r, ok := m.(*protocol.ResultMessage); ok {
				return r, seen, nil
			}
		case <-deadline.C:
			return nil, seen, context.DeadlineExceeded
		case <-s.closedCh:
			return nil, seen, ErrSessionClosed
		}
	}
}

// SessionID returns the CLI-assigned session id once system/init has arrived,
// or the value from Options.SessionID if one was pinned. Empty before init.
func (s *Session) SessionID() string {
	// The runner does not retain init state; callers typically capture it
	// from Stream. For convenience we expose the pinned id if set.
	if s.r.opts.SessionID != "" {
		return s.r.opts.SessionID
	}
	return s.r.lastSessionID()
}

// Models returns the account's available model list captured from the
// initialize handshake (id + display name), plus the current model id at
// handshake time. Returns nil when the handshake did not carry a model list
// (older CLIs).
func (s *Session) Models() ([]protocol.ModelInfo, string) {
	ir := s.r.initializeResponse()
	if ir == nil {
		return nil, ""
	}
	return ir.Models, ir.CurrentModelID
}

// SlashCommands returns the CLI slash commands captured at handshake.
func (s *Session) SlashCommands() []protocol.SlashCommand {
	ir := s.r.initializeResponse()
	if ir == nil {
		return nil
	}
	return ir.Commands
}

// Interrupt aborts the current turn. Like set_model, the CLI requires an
// established session; ErrSessionNotEstablished is returned before that.
func (s *Session) Interrupt() error {
	if s.r.lastSessionID() == "" {
		return ErrSessionNotEstablished
	}
	_, err := s.r.sendControlRequest(protocol.InterruptRequest{
		Subtype:   protocol.ControlInterrupt,
		SessionID: s.r.lastSessionID(),
		Reason:    "Interrupted by user",
	}, 30*time.Second)
	if err != nil {
		// Mirror the TS SDK: interrupt errors are non-fatal.
		return err
	}
	return nil
}

// SetPermissionMode switches the permission mode mid-session. The CLI
// requires the session id; it is taken from system/init automatically.
func (s *Session) SetPermissionMode(mode protocol.PermissionMode) error {
	_, err := s.r.sendControlRequest(protocol.SetPermissionModeRequest{
		Subtype:   protocol.ControlSetPermissionMode,
		SessionID: s.r.lastSessionID(),
		Mode:      mode,
	}, 30*time.Second)
	return err
}

// SetModel switches the model mid-session and returns the CLI confirmation
// (new model + previous model).
//
// IMPORTANT (verified against CLI 2.150): the CLI only accepts set_model once
// the session is established — i.e. after at least one user turn has been
// processed. Before that it replies "Session not found"; this SDK maps both
// the local guard and that CLI error to ErrSessionNotEstablished. The switch
// takes effect from the NEXT turn; the in-flight turn keeps its model.
func (s *Session) SetModel(model string) (*protocol.SetModelResponse, error) {
	if s.r.lastSessionID() == "" {
		return nil, ErrSessionNotEstablished
	}
	resp, err := s.r.sendControlRequest(protocol.SetModelRequest{
		Subtype:   protocol.ControlSetModel,
		SessionID: s.r.lastSessionID(),
		Model:     model,
	}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	body, err := resp.Body()
	if err != nil {
		return nil, err
	}
	if body.Subtype != "success" {
		if strings.Contains(body.Error, "Session not found") {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotEstablished, body.Error)
		}
		return nil, &ControlRequestError{RequestID: body.RequestID, Message: body.Error}
	}
	var out protocol.SetModelResponse
	if len(body.Response) > 0 {
		if err := json.Unmarshal(body.Response, &out); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// RewindScope selects what a Rewind rolls back.
type RewindScope string

const (
	RewindScopeCode                RewindScope = "Code"
	RewindScopeConversation        RewindScope = "Conversation"
	RewindScopeCodeAndConversation RewindScope = "CodeAndConversation"
)

// Rewind rolls back workspace files and/or conversation history to the state
// before the given user message (message ids come from file-history-snapshot
// or user message uuids on the stream). DryRun previews without changes.
func (s *Session) Rewind(userMessageID string, scope RewindScope, dryRun bool) (*protocol.RewindResponse, error) {
	req := protocol.RewindRequest{
		Subtype:       protocol.ControlRewind,
		UserMessageID: userMessageID,
		DryRun:        dryRun,
	}
	if scope != "" {
		req.Scope = string(scope)
	}
	resp, err := s.r.sendControlRequest(req, 60*time.Second)
	if err != nil {
		return nil, err
	}
	body, err := resp.Body()
	if err != nil {
		return nil, err
	}
	if body.Subtype != "success" {
		return nil, &ControlRequestError{RequestID: body.RequestID, Message: body.Error}
	}
	var out protocol.RewindResponse
	if len(body.Response) > 0 {
		if err := json.Unmarshal(body.Response, &out); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// Close ends the session and reaps the CLI child process.
func (s *Session) Close() error {
	s.closeMu.Do(func() {
		close(s.closedCh)
		// Best-effort graceful end before transport teardown.
		_, _ = s.r.sendControlRequest(protocol.EndSessionRequest{Subtype: protocol.ControlEndSession}, 3*time.Second)
		s.r.shutdown()
	})
	return nil
}
