package protocol

// PermissionMode is the tool-permission policy mode (mirrors the CLI's
// --permission-mode values).
type PermissionMode string

const (
	// PermissionDefault requires confirmation for all tool operations.
	PermissionDefault PermissionMode = "default"
	// PermissionAcceptEdits auto-approves file edits; other ops still confirm.
	PermissionAcceptEdits PermissionMode = "acceptEdits"
	// PermissionPlan allows only read-only tools (planning mode).
	PermissionPlan PermissionMode = "plan"
	// PermissionBypassPermissions skips all permission checks (use with care).
	PermissionBypassPermissions PermissionMode = "bypassPermissions"
	// PermissionDontAsk never prompts (CLI-specific).
	PermissionDontAsk PermissionMode = "dontAsk"
	// PermissionAuto lets the CLI auto-classify (CLI-specific).
	PermissionAuto PermissionMode = "auto"
)

// PermissionBehavior is the outcome of a permission decision.
type PermissionBehavior string

const (
	PermissionAllow PermissionBehavior = "allow"
	PermissionDeny  PermissionBehavior = "deny"
	PermissionAsk   PermissionBehavior = "ask"
)

// PermissionResult is the host-facing decision returned from a CanUseTool
// callback. The SDK translates it to the wire-level CanUseToolResponse
// ({allowed, updatedInput, reason, tool_use_id}).
type PermissionResult struct {
	Behavior     PermissionBehavior
	UpdatedInput map[string]any `json:"-"` // allow: optionally rewritten input
	Message      string         `json:"-"` // deny: human-readable reason
	Interrupt    bool           `json:"-"` // deny: abort the whole session
}

// Allow returns an approving PermissionResult. updatedInput may be nil to
// keep the model's original tool input unchanged.
func Allow(updatedInput map[string]any) PermissionResult {
	return PermissionResult{Behavior: PermissionAllow, UpdatedInput: updatedInput}
}

// Deny returns a rejecting PermissionResult with a reason shown to the model.
func Deny(reason string) PermissionResult {
	return PermissionResult{Behavior: PermissionDeny, Message: reason}
}
