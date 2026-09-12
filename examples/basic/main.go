// Command basic demonstrates a one-shot Query against the local codebuddy CLI.
//
// Run:
//
//	CODEBUDDY_SDK_E2E=1 go run ./examples/basic "explain what this repo does"
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	codebuddy "github.com/godeps/codebuddy-agent-sdk-go"
	"github.com/godeps/codebuddy-agent-sdk-go/protocol"
)

func main() {
	prompt := "Reply with a one-line greeting."
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	opts := codebuddy.NewOptions().
		WithCWD(mustWd()).
		WithPermissionMode(protocol.PermissionBypassPermissions)

	fmt.Fprintf(os.Stderr, ">> querying codebuddy: %q\n", prompt)
	res, err := codebuddy.QueryOnce(ctx, prompt, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("session: %s\n", res.SessionID)
	fmt.Printf("turns:   %d   cost: $%.4f\n", res.NumTurns, res.TotalCostUSD)
	fmt.Printf("reply:\n%s\n", res.Text)
}

func mustWd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
