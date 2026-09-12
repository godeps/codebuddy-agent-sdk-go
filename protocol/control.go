package protocol

import "encoding/json"

// ControlRequest is the wire envelope exchanged between the CLI and the SDK
// for bidirectional control operations outside the agentic loop.
type ControlRequest struct {
	Type      string          `json:"type"` // "control_request"
	RequestID string          `json:"request_id"`
	SessionID string          `json:"session_id,omitempty"`
	Request   json.RawMessage `json:"request"`
	Meta      json.RawMessage `json:"_meta,omitempty"`
}

// MessageType returns "control_request".
func (m ControlRequest) MessageType() string { return TypeControlRequest }

// Subtype returns the inner request's "subtype" discriminant.
func (m ControlRequest) Subtype() string {
	var probe struct {
		Subtype string `json:"subtype"`
	}
	_ = json.Unmarshal(m.Request, &probe)
	return probe.Subtype
}

// ControlResponse is the wire envelope replying to a ControlRequest.
type ControlResponse struct {
	Type      string          `json:"type"` // "control_response"
	SessionID string          `json:"session_id,omitempty"`
	Response  json.RawMessage `json:"response"`
}

// MessageType returns "control_response".
func (m ControlResponse) MessageType() string { return TypeControlResponse }

// ControlResponseBody is the inner payload of ControlResponse.Response.
type ControlResponseBody struct {
	Subtype   string          `json:"subtype"` // "success" | "error"
	RequestID string          `json:"request_id"`
	Response  json.RawMessage `json:"response,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// Body decodes the response envelope body.
func (m ControlResponse) Body() (ControlResponseBody, error) {
	var b ControlResponseBody
	err := json.Unmarshal(m.Response, &b)
	return b, err
}

// IsSuccess reports whether this response carries subtype "success".
func (m ControlResponse) IsSuccess() bool {
	b, err := m.Body()
	return err == nil && b.Subtype == "success"
}

// NewSuccessResponse builds a control_response envelope with a success body.
func NewSuccessResponse(requestID string, response any) ControlResponse {
	var respJSON json.RawMessage
	if response != nil {
		respJSON, _ = json.Marshal(response)
	}
	body, _ := json.Marshal(ControlResponseBody{
		Subtype:   "success",
		RequestID: requestID,
		Response:  respJSON,
	})
	return ControlResponse{Type: TypeControlResponse, Response: body}
}

// NewErrorResponse builds a control_response envelope with an error body.
func NewErrorResponse(requestID, message string) ControlResponse {
	body, _ := json.Marshal(ControlResponseBody{
		Subtype:   "error",
		RequestID: requestID,
		Error:     message,
	})
	return ControlResponse{Type: TypeControlResponse, Response: body}
}

// --- SDK -> CLI control requests ---

// InitializeRequest is sent by the SDK as the first control_request. The CLI
// replies with a control_response carrying slash commands, agents, MCP
// servers, and (importantly) starts emitting system/init on the agent stream.
//
// The canUseTool capability is implicit: the CLI sends can_use_tool control
// requests whenever the permission mode requires approval and the SDK has an
// open stdin channel; hosts that cannot answer should run with
// PermissionMode == bypassPermissions or --dangerously-skip-permissions.
type InitializeRequest struct {
	Subtype            string                              `json:"subtype"` // "initialize"
	Hooks              map[HookEvent][]HookCallbackMatcher `json:"hooks,omitempty"`
	SystemPrompt       string                              `json:"systemPrompt,omitempty"`
	AppendSystemPrompt string                              `json:"appendSystemPrompt,omitempty"`
	Agents             map[string]AgentDefinition          `json:"agents,omitempty"`
	SdkMcpServers      []string                            `json:"sdkMcpServers,omitempty"`
	Capabilities       *InitializeCapabilities             `json:"capabilities,omitempty"`
}

// InitializeCapabilities declares optional protocol capabilities.
type InitializeCapabilities struct {
	AskUserQuestion bool `json:"askUserQuestion,omitempty"`
}

// InitializeResponse is the payload of the CLI's initialize control_response.
// Verified against CLI 2.150: it carries the available model list, the
// current model id, slash commands, output styles, and account info.
type InitializeResponse struct {
	Commands []SlashCommand `json:"commands,omitempty"`
	// Models is the account's available model list.
	Models                []ModelInfo  `json:"models,omitempty"`
	CurrentModelID        string       `json:"currentModelId,omitempty"`
	OutputStyle           string       `json:"output_style,omitempty"`
	AvailableOutputStyles []string     `json:"available_output_styles,omitempty"`
	Account               *AccountInfo `json:"account,omitempty"`
}

// SlashCommand describes one CLI slash command.
type SlashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ModelInfo is one entry of the initialize response's model list.
type ModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// AccountInfo is the logged-in account summary (token fields intentionally
// not modeled; never log or persist this struct).
type AccountInfo struct {
	UID      string `json:"uid,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Type     string `json:"type,omitempty"`
	UserName string `json:"userName,omitempty"`
}

// InterruptRequest aborts the current turn.
type InterruptRequest struct {
	Subtype string `json:"subtype"` // "interrupt"
}

// SetPermissionModeRequest switches the permission mode mid-session.
// SessionID must be the CLI-assigned session id (from system/init); the CLI
// rejects requests that omit it or reference an unestablished session.
type SetPermissionModeRequest struct {
	Subtype   string         `json:"subtype"` // "set_permission_mode"
	SessionID string         `json:"session_id,omitempty"`
	Mode      PermissionMode `json:"mode"`
}

// SetModelRequest switches the model mid-session. The CLI only accepts it
// once the session is established (i.e. after the first user turn has been
// processed); before that it replies "Session not found". Verified against
// CLI 2.150: the response echoes {session_id, model, previous_model}.
type SetModelRequest struct {
	Subtype   string `json:"subtype"` // "set_model"
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model"`
}

// SetModelResponse is the payload of a successful set_model response.
type SetModelResponse struct {
	SessionID     string `json:"session_id,omitempty"`
	Model         string `json:"model,omitempty"`
	PreviousModel string `json:"previous_model,omitempty"`
}

// RewindRequest rolls back workspace files and/or conversation history to the
// state before a given user message. Scope: "Code" | "Conversation" |
// "CodeAndConversation" (default).
type RewindRequest struct {
	Subtype       string `json:"subtype"` // "rewind"
	UserMessageID string `json:"user_message_id"`
	DryRun        bool   `json:"dry_run,omitempty"`
	Scope         string `json:"scope,omitempty"`
}

// RewindFilesRequest rolls back workspace files only (Claude Code compatible).
type RewindFilesRequest struct {
	Subtype       string `json:"subtype"` // "rewind_files"
	UserMessageID string `json:"user_message_id"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

// RewindResponse is the payload of a successful rewind/rewind_files response.
type RewindResponse struct {
	CanRewind       bool     `json:"canRewind"`
	Error           string   `json:"error,omitempty"`
	FilesChanged    []string `json:"filesChanged,omitempty"`
	Insertions      *int     `json:"insertions,omitempty"`
	Deletions       *int     `json:"deletions,omitempty"`
	HistoryRewound  *bool    `json:"historyRewound,omitempty"`
	FileRewindError string   `json:"fileRewindError,omitempty"`
}

// EndSessionRequest asks the CLI to close the session gracefully.
type EndSessionRequest struct {
	Subtype string `json:"subtype"` // "end_session"
}

// --- CLI -> SDK control requests ---

// CanUseToolRequest is a permission prompt for one tool call. The CLI expects
// a control_response whose body is a CanUseToolResponse.
type CanUseToolRequest struct {
	Subtype              string          `json:"subtype"` // "can_use_tool"
	ToolName             string          `json:"tool_name"`
	Input                json.RawMessage `json:"input"`
	ToolUseID            string          `json:"tool_use_id,omitempty"`
	AgentID              string          `json:"agent_id,omitempty"`
	PermissionSuggestion json.RawMessage `json:"permission_suggestions,omitempty"`
	BlockedPath          string          `json:"blocked_path,omitempty"`
	DecisionReason       string          `json:"decision_reason,omitempty"`
}

// InputMap decodes Input into a generic map.
func (r CanUseToolRequest) InputMap() map[string]any {
	var m map[string]any
	if err := json.Unmarshal(r.Input, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// CanUseToolResponse is the wire body the CLI expects for can_use_tool.
// NOTE: this is the CodeBuddy-specific shape (verified against CLI 2.150):
// {allowed, updatedInput|reason, tool_use_id} — NOT the Claude-style
// {behavior: "allow"} envelope.
type CanUseToolResponse struct {
	Allowed      bool            `json:"allowed"`
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"` // when allowed
	Reason       string          `json:"reason,omitempty"`       // when denied
	Interrupt    *bool           `json:"interrupt,omitempty"`    // deny + abort session
	ToolUseID    string          `json:"tool_use_id,omitempty"`
}

// AllowTool builds an approval response echoing (optionally rewritten) input.
func AllowTool(toolUseID string, updatedInput json.RawMessage) CanUseToolResponse {
	return CanUseToolResponse{Allowed: true, UpdatedInput: updatedInput, ToolUseID: toolUseID}
}

// DenyTool builds a rejection response.
func DenyTool(toolUseID, reason string) CanUseToolResponse {
	return CanUseToolResponse{Allowed: false, Reason: reason, ToolUseID: toolUseID}
}

// HookCallbackRequest invokes a host hook registered via initialize.
type HookCallbackRequest struct {
	Subtype   string          `json:"subtype"` // "hook_callback"
	HookID    string          `json:"hook_id"`
	HookEvent string          `json:"hook_event_name,omitempty"`
	HookInput json.RawMessage `json:"input,omitempty"`
}

// McpMessageRequest proxies a JSON-RPC frame to an in-process SDK MCP server.
type McpMessageRequest struct {
	Subtype    string          `json:"subtype"` // "mcp_message"
	ServerName string          `json:"server_name"`
	Message    json.RawMessage `json:"message"`
}

// ElicitationRequest asks the host to collect structured user input.
type ElicitationRequest struct {
	Subtype string          `json:"subtype"` // "elicitation_create"
	Params  json.RawMessage `json:"params,omitempty"`
}

// AgentDefinition declares a custom agent injected at initialize time.
type AgentDefinition struct {
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools,omitempty"`
	Model       string   `json:"model,omitempty"`
}

// HookCallbackMatcher is one hook registration: an optional matcher plus the
// callback IDs to invoke. Callbacks run in the host via hook_callback control
// requests.
type HookCallbackMatcher struct {
	Matcher string         `json:"matcher,omitempty"`
	Hooks   []HookCallback `json:"hooks"`
}

// HookCallback identifies a host-side hook callback.
type HookCallback struct {
	Type       string `json:"type"` // "prompt" | "command" | "callback"
	CallbackID string `json:"callback_id,omitempty"`
}
