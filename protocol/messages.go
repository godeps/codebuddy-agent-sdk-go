package protocol

import "encoding/json"

// Message is a decoded JSONL line from codebuddy stdout. Each concrete type
// reports its wire "type" discriminant via MessageType().
type Message interface {
	MessageType() string
}

// ParseMessage decodes one JSONL line into the matching concrete Message type.
// Unknown types yield an *UnknownMessage that preserves the raw bytes.
func ParseMessage(line []byte) (Message, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		return nil, err
	}
	switch probe.Type {
	case TypeAssistant:
		return decode[AssistantMessage](line)
	case TypeUser:
		return decode[UserMessage](line)
	case TypeResult:
		return decode[ResultMessage](line)
	case TypeSystem:
		return decode[SystemMessage](line)
	case TypeStreamEvent:
		return decode[PartialAssistantMessage](line)
	case TypeFileHistorySnapshot:
		return decode[FileHistorySnapshot](line)
	case TypeControlRequest:
		return decode[ControlRequest](line)
	case TypeControlResponse:
		return decode[ControlResponse](line)
	case TypeControlCancel:
		return decode[ControlCancel](line)
	case TypeKeepAlive:
		return decode[KeepAlive](line)
	default:
		return &UnknownMessage{typeStr: probe.Type, Raw: append([]byte(nil), line...)}, nil
	}
}

func decode[T Message](line []byte) (*T, error) {
	var m T
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// UnknownMessage preserves an unrecognized JSONL line verbatim for forward
// compatibility.
type UnknownMessage struct {
	typeStr string
	Raw     json.RawMessage
}

// MessageType returns the wire type discriminant.
func (m UnknownMessage) MessageType() string { return m.typeStr }

// RawMessage returns the original JSON line.
func (m UnknownMessage) RawMessage() json.RawMessage { return m.Raw }

// KeepAlive is a transport-level keep-alive frame.
type KeepAlive struct {
	Type string `json:"type"` // "keep_alive"
}

// MessageType returns "keep_alive".
func (m KeepAlive) MessageType() string { return TypeKeepAlive }

// ControlCancel asks the SDK to abandon an in-flight control request.
type ControlCancel struct {
	Type      string `json:"type"` // "control_cancel_request"
	RequestID string `json:"request_id"`
}

// MessageType returns "control_cancel_request".
func (m ControlCancel) MessageType() string { return TypeControlCancel }

// --- Content blocks (Anthropic-style) ---

// ContentBlock is a generic content block (text, tool_use, tool_result,
// image, thinking, ...). The wire shape is open-ended; unknown fields are
// ignored on decode.
type ContentBlock struct {
	Type      string          `json:"type"` // text | tool_use | tool_result | image | thinking
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Source    *ImageSource    `json:"source,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// ImageSource is a base64-encoded image input.
type ImageSource struct {
	Type      string `json:"type"` // "base64"
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// --- Usage ---

// Usage is the token-usage block. All fields are nullable on the wire, so
// pointers distinguish "absent" from "zero".
type Usage struct {
	InputTokens              *int    `json:"input_tokens,omitempty"`
	OutputTokens             *int    `json:"output_tokens,omitempty"`
	CacheCreationInputTokens *int    `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     *int    `json:"cache_read_input_tokens,omitempty"`
	ServiceTier              *string `json:"service_tier,omitempty"`
}

// ModelUsage is per-model cumulative usage for a session (result message).
type ModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	ContextWindow            int     `json:"contextWindow,omitempty"`
	MaxOutputTokens          int     `json:"maxOutputTokens,omitempty"`
	CostUSD                  float64 `json:"costUSD,omitempty"`
}

// --- Agent messages ---

// BetaMessage is the assistant message payload (Anthropic-style).
type BetaMessage struct {
	ID           string         `json:"id,omitempty"`
	Type         string         `json:"type,omitempty"` // "message"
	Role         string         `json:"role"`           // "assistant"
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model,omitempty"`
	StopReason   *string        `json:"stop_reason,omitempty"`
	StopSequence *string        `json:"stop_sequence,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
}

// AssistantMessage is a model response turn.
type AssistantMessage struct {
	Type            string      `json:"type"` // "assistant"
	Message         BetaMessage `json:"message"`
	ParentToolUseID *string     `json:"parent_tool_use_id"`
	UUID            string      `json:"uuid"`
	SessionID       string      `json:"session_id"`
}

// MessageType returns "assistant".
func (m AssistantMessage) MessageType() string { return TypeAssistant }

// UserMessage is a user-side message (input echo or tool_result replay).
type UserMessage struct {
	Type            string          `json:"type"` // "user"
	Message         MessageParam    `json:"message"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	IsSynthetic     bool            `json:"isSynthetic,omitempty"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
	ToolUseResult   json.RawMessage `json:"toolUseResult,omitempty"`
}

// MessageType returns "user".
func (m UserMessage) MessageType() string { return TypeUser }

// MessageParam is a user-side message envelope. Content is either a plain
// string or []ContentBlock.
type MessageParam struct {
	Role    string          `json:"role"` // "user"
	Content json.RawMessage `json:"content"`
}

// ContentBlocks decodes a []ContentBlock from MessageParam.Content. Returns
// nil when Content is a plain string.
func (p MessageParam) ContentBlocks() []ContentBlock {
	var blocks []ContentBlock
	if err := json.Unmarshal(p.Content, &blocks); err == nil {
		return blocks
	}
	return nil
}

// ContentText decodes a plain-string Content. Returns "" for block arrays.
func (p MessageParam) ContentText() string {
	var s string
	if err := json.Unmarshal(p.Content, &s); err == nil {
		return s
	}
	return ""
}

// ResultMessage is the terminal result of a task round.
type ResultMessage struct {
	Type              string                `json:"type"`    // "result"
	Subtype           string                `json:"subtype"` // success | error_max_turns | error_during_execution
	IsError           bool                  `json:"is_error"`
	Result            string                `json:"result,omitempty"`
	DurationMs        int                   `json:"duration_ms"`
	DurationAPIMs     int                   `json:"duration_api_ms"`
	NumTurns          int                   `json:"num_turns"`
	TotalCostUSD      float64               `json:"total_cost_usd"`
	Usage             *Usage                `json:"usage,omitempty"`
	ModelUsage        map[string]ModelUsage `json:"modelUsage,omitempty"`
	PermissionDenials []PermissionDenial    `json:"permission_denials,omitempty"`
	UUID              string                `json:"uuid"`
	SessionID         string                `json:"session_id"`
	StructuredOutput  json.RawMessage       `json:"structured_output,omitempty"`
}

// MessageType returns "result".
func (m ResultMessage) MessageType() string { return TypeResult }

// IsSuccess reports whether this is a success result.
func (m ResultMessage) IsSuccess() bool { return m.Subtype == "success" && !m.IsError }

// PermissionDenial records a denied tool call.
type PermissionDenial struct {
	ToolName  string          `json:"tool_name"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

// SystemMessage is a "system"-typed message discriminated by Subtype. It is a
// flattened union: set Subtype selects the meaningful fields; unknown subtypes
// are preserved in Raw for forward compatibility.
type SystemMessage struct {
	Type    string          `json:"type"` // "system"
	Subtype string          `json:"subtype"`
	Raw     json.RawMessage `json:"-"`

	// init
	SessionID      string          `json:"session_id"`
	APIKeySource   string          `json:"apiKeySource,omitempty"`
	CWD            string          `json:"cwd,omitempty"`
	Tools          []string        `json:"tools,omitempty"`
	McpServers     []McpServerInit `json:"mcp_servers,omitempty"`
	Model          string          `json:"model,omitempty"`
	PermissionMode PermissionMode  `json:"permissionMode,omitempty"`
	SlashCommands  []string        `json:"slash_commands,omitempty"`
	OutputStyle    string          `json:"output_style,omitempty"`

	// status / task_*
	Status string `json:"status,omitempty"`

	// task events
	TaskID       string          `json:"task_id,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Description  string          `json:"description,omitempty"`
	TaskType     string          `json:"task_type,omitempty"`
	Summary      string          `json:"summary,omitempty"`
	OutputFile   string          `json:"output_file,omitempty"`
	TaskUsage    *TaskUsage      `json:"usage,omitempty"`
	LastToolName string          `json:"last_tool_name,omitempty"`
	TaskPatch    json.RawMessage `json:"patch,omitempty"`

	// session_title_changed
	Title string `json:"title,omitempty"`

	// api_retry
	Attempt    int    `json:"attempt,omitempty"`
	MaxRetries int    `json:"max_retries,omitempty"`
	RetryError string `json:"error,omitempty"`

	UUID string `json:"uuid"`
}

// UnmarshalJSON populates Raw alongside the struct fields.
func (m *SystemMessage) UnmarshalJSON(data []byte) error {
	type alias SystemMessage
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*m = SystemMessage(a)
	m.Raw = append(json.RawMessage(nil), data...)
	return nil
}

// MessageType returns "system".
func (m SystemMessage) MessageType() string { return TypeSystem }

// McpServerInit reports an MCP server's startup state in system/init.
type McpServerInit struct {
	Name   string `json:"name"`
	Status string `json:"status"` // connected | failed | pending | needs_auth
}

// TaskUsage is cumulative usage for a background task event.
type TaskUsage struct {
	TotalTokens *int `json:"total_tokens,omitempty"`
	ToolUses    int  `json:"tool_uses"`
	DurationMs  int  `json:"duration_ms"`
}

// PartialAssistantMessage is an incremental stream event
// (--include-partial-messages).
type PartialAssistantMessage struct {
	Type            string          `json:"type"` // "stream_event"
	Event           json.RawMessage `json:"event"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	UUID            string          `json:"uuid"`
	SessionID       string          `json:"session_id"`
}

// MessageType returns "stream_event".
func (m PartialAssistantMessage) MessageType() string { return TypeStreamEvent }

// FileHistorySnapshot marks a rewind anchor: the workspace state before the
// referenced user message. snapshot.messageId is the user-message id usable
// with the rewind control request.
type FileHistorySnapshot struct {
	Type             string          `json:"type"` // "file-history-snapshot"
	MessageID        string          `json:"messageId"`
	Timestamp        int64           `json:"timestamp"`
	IsSnapshotUpdate bool            `json:"isSnapshotUpdate,omitempty"`
	Snapshot         json.RawMessage `json:"snapshot,omitempty"`
	UUID             string          `json:"uuid,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
}

// MessageType returns "file-history-snapshot".
func (m FileHistorySnapshot) MessageType() string { return TypeFileHistorySnapshot }
