// Command permissions demonstrates the can_use_tool callback: the SDK decides
// at runtime whether each tool call may run.
//
// Run:
//
//	CODEBUDDY_SDK_E2E=1 go run ./examples/permissions
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	opts := codebuddy.NewOptions().
		WithCWD(cwd()).
		WithPermissionMode(protocol.PermissionDefault).
		WithCanUseTool(func(ctx context.Context, req *protocol.CanUseToolRequest) (protocol.PermissionResult, error) {
			input := req.InputMap()
			cmd, _ := input["command"].(string)
			fmt.Fprintf(os.Stderr, ">> permission request: %s %v\n", req.ToolName, cmd)

			// Policy: allow read-only commands, deny anything destructive.
			if req.ToolName == "Bash" {
				if strings.Contains(cmd, "rm ") || strings.Contains(cmd, "sudo") {
					return protocol.Deny("destructive command blocked by SDK policy"), nil
				}
				return protocol.Allow(nil), nil
			}
			return protocol.Allow(nil), nil
		})

	res, err := codebuddy.QueryOnce(ctx,
		"Run `echo hello` then run `rm -rf /tmp/does-not-exist`. Report what happened.", opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("reply:\n%s\n", res.Text)
}

func cwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
