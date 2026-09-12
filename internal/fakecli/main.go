// fake-codebuddy is a scripted stand-in for the codebuddy CLI used by tests.
//
// Wire behavior mirrors the real CLI's stream-json mode (verified against
// codebuddy 2.150):
//   - control_request initialize -> control_response success (with commands)
//   - user message -> system/init (first turn only), assistant message,
//     optional can_use_tool round-trip when TOOL_PROMPT=1, then result
//   - stdin close -> exit
//
// Environment knobs:
//   - FAKE_TEXT: assistant reply text (default "Hello from fake codebuddy")
//   - FAKE_TOOL_PROMPT=1: emit a tool_use and a can_use_tool control request
//     before the result; records the SDK's decision to stderr as
//     "TOOL_DECISION allow|deny".
package main

import (
	"bufio"
	"encoding/json"
	"os"
)

func writeLine(v any) {
	data, _ := json.Marshal(v)
	_, _ = os.Stdout.Write(append(data, '\n'))
}

func main() {
	text := os.Getenv("FAKE_TEXT")
	if text == "" {
		text = "Hello from fake codebuddy"
	}
	toolPrompt := os.Getenv("FAKE_TOOL_PROMPT") == "1"

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	inited := false
	turn := 0
	for scanner.Scan() {
		var msg map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		switch msg["type"] {
		case "control_request":
			reqID, _ := msg["request_id"].(string)
			resp := map[string]any{"subtype": "success", "request_id": reqID}
			req, _ := msg["request"].(map[string]any)
			if st, _ := req["subtype"].(string); st == "initialize" {
				resp["response"] = map[string]any{
					"commands": []map[string]any{{"name": "/help", "description": "fake"}},
				}
			}
			if st, _ := req["subtype"].(string); st == "end_session" {
				writeLine(map[string]any{"type": "control_response", "session_id": "fake-session", "response": resp})
				return
			}
			writeLine(map[string]any{"type": "control_response", "session_id": "fake-session", "response": resp})
		case "user":
			turn++
			if !inited {
				inited = true
				writeLine(map[string]any{
					"type": "system", "subtype": "init",
					"session_id":     "fake-session",
					"apiKeySource":   "copilot.tencent.com",
					"cwd":            ".",
					"tools":          []string{"Bash", "Read"},
					"mcp_servers":    []any{},
					"model":          "fake-model",
					"permissionMode": "default",
					"slash_commands": []string{"/help"},
					"output_style":   "default",
					"uuid":           "init-uuid",
				})
			}
			if toolPrompt && turn == 1 {
				// assistant emits a tool_use; CLI asks the SDK for permission.
				writeLine(map[string]any{
					"type": "assistant", "uuid": "asst-tool", "session_id": "fake-session",
					"message": map[string]any{
						"role": "assistant",
						"content": []map[string]any{{
							"type": "tool_use", "id": "call_1", "name": "Bash",
							"input": map[string]any{"command": "echo hi"},
						}},
					},
					"parent_tool_use_id": nil,
				})
				writeLine(map[string]any{
					"type": "control_request", "request_id": "perm_1",
					"request": map[string]any{
						"subtype": "can_use_tool", "tool_name": "Bash",
						"input":       map[string]any{"command": "echo hi"},
						"tool_use_id": "call_1",
					},
				})
			}
			writeLine(map[string]any{
				"type": "assistant", "uuid": "asst-1", "session_id": "fake-session",
				"message": map[string]any{
					"role": "assistant",
					"content": []map[string]any{{
						"type": "text", "text": text,
					}},
					"usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
				},
				"parent_tool_use_id": nil,
			})
			writeLine(map[string]any{
				"type": "result", "subtype": "success", "is_error": false,
				"result": text, "duration_ms": 10, "duration_api_ms": 5,
				"num_turns": 1, "total_cost_usd": 0,
				"usage":      map[string]any{"input_tokens": 10, "output_tokens": 5},
				"uuid":       "res-1",
				"session_id": "fake-session",
			})
		}
	}
}
