// Package runtime resolves the codebuddy CLI executable.
package runtime

import (
	"errors"
	"os"
	"os/exec"
)

// ErrNotFound indicates the CLI executable could not be located.
var ErrNotFound = errors.New("codebuddy: CLI executable not found (set CODEBUDDY_CODE_PATH or install codebuddy/cbc in PATH)")

// BinaryNames are the CLI binary names, preferred order first.
var BinaryNames = []string{"codebuddy", "cbc"}

// ResolvePath resolves the CLI executable path.
//
// Precedence: explicit override -> CODEBUDDY_CODE_PATH env var -> PATH lookup
// over BinaryNames.
func ResolvePath(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if p := os.Getenv("CODEBUDDY_CODE_PATH"); p != "" {
		return p, nil
	}
	for _, name := range BinaryNames {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}
