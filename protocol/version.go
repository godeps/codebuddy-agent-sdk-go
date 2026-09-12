// Package protocol contains the JSONL wire types spoken between the CodeBuddy
// CLI (codebuddy / cbc) and the SDK.
//
// The CLI writes one JSON object per line to stdout; the SDK writes one JSON
// object per line to stdin. stderr carries diagnostics only and is not part of
// the protocol. Each line is either an agent Message (assistant/user/result/
// system/stream_event/file-history-snapshot/...), a control request/response,
// or a keep_alive frame.
//
// The wire format mirrors the CodeBuddy Agent SDK (@tencent-ai/agent-sdk)
// stream-json protocol, documented at
// https://www.codebuddy.cn/docs/cli/headless and /docs/cli/workflow-stdio-protocol.
package protocol

// Agent message type discriminants on the wire.
const (
	TypeSystem              = "system"
	TypeAssistant           = "assistant"
	TypeUser                = "user"
	TypeResult              = "result"
	TypeStreamEvent         = "stream_event"
	TypeFileHistorySnapshot = "file-history-snapshot"
	TypeControlRequest      = "control_request"
	TypeControlResponse     = "control_response"
	TypeControlCancel       = "control_cancel_request"
	TypeKeepAlive           = "keep_alive"
)

// System message subtypes.
const (
	SubtypeInit                = "init"
	SubtypeStatus              = "status"
	SubtypeTaskStarted         = "task_started"
	SubtypeTaskProgress        = "task_progress"
	SubtypeTaskUpdated         = "task_updated"
	SubtypeTaskNotification    = "task_notification"
	SubtypeCompactBoundary     = "compact_boundary"
	SubtypeAPIRetry            = "api_retry"
	SubtypeSessionTitle        = "session_title_changed"
	SubtypePermissionDenied    = "permission_denied"
	SubtypeModelQueueStatus    = "model_queue_status"
	SubtypeSessionStateChanged = "session_state_changed"
)

// Control request subtypes the SDK sends to the CLI.
const (
	ControlInitialize        = "initialize"
	ControlInterrupt         = "interrupt"
	ControlSetPermissionMode = "set_permission_mode"
	ControlSetModel          = "set_model"
	ControlRewind            = "rewind"
	ControlRewindFiles       = "rewind_files"
	ControlEndSession        = "end_session"
	ControlGetContextUsage   = "get_context_usage"
	ControlSetConfig         = "set_config"
)

// Control request subtypes the CLI sends to the SDK.
const (
	ControlCanUseTool         = "can_use_tool"
	ControlHookCallback       = "hook_callback"
	ControlMcpMessage         = "mcp_message"
	ControlElicitation        = "elicitation_create"
	ControlGetPermission      = "get_permission_update"
	ControlInitializeUpstream = "initialize_upstream"
)
