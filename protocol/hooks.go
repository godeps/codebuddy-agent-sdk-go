package protocol

import "encoding/json"

// HookEvent enumerates the hook lifecycle events the CLI can fire.
type HookEvent string

const (
	HookPreToolUse       HookEvent = "PreToolUse"
	HookPostToolUse      HookEvent = "PostToolUse"
	HookUserPromptSubmit HookEvent = "UserPromptSubmit"
	HookSessionStart     HookEvent = "SessionStart"
	HookSessionEnd       HookEvent = "SessionEnd"
	HookStop             HookEvent = "Stop"
	HookSubagentStop     HookEvent = "SubagentStop"
	HookPreCompact       HookEvent = "PreCompact"
	HookNotification     HookEvent = "Notification"
)

// HookInput is the wire payload the CLI sends to a host hook callback. It is a
// flattened union: HookEventName selects which fields are meaningful.
type HookInput struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path,omitempty"`
	CWD            string          `json:"cwd,omitempty"`
	HookEventName  HookEvent       `json:"hook_event_name"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	ToolResponse   json.RawMessage `json:"tool_response,omitempty"`
	Prompt         string          `json:"prompt,omitempty"`
	Source         string          `json:"source,omitempty"`
	StopHookActive *bool           `json:"stop_hook_active,omitempty"`
}

// HookJSONOutput is the structured result a host returns from a hook callback.
type HookJSONOutput struct {
	Continue          *bool  `json:"continue,omitempty"`
	StopReason        string `json:"stopReason,omitempty"`
	SuppressOutput    *bool  `json:"suppressOutput,omitempty"`
	Decision          string `json:"decision,omitempty"` // approve | block
	Reason            string `json:"reason,omitempty"`
	SystemMessage     string `json:"systemMessage,omitempty"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}
