// Package auth resolves CodeBuddy CLI authentication for SDK use.
//
// Unlike token-payload SDKs, the codebuddy CLI authenticates through its own
// persisted login state (~/.codebuddy, created by `codebuddy login` or the
// IDE OAuth flow). The SDK therefore does not inject credentials; it only
// verifies that a usable login exists and surfaces actionable errors.
//
// API-key style auth is also supported for headless/CI use: when the host
// supplies an API key, it is exported into the child environment as
// CODEBUDDY_API_KEY (plus the ANTHROPIC_* aliases the CLI honors).
package auth

import (
	"os"
	"os/exec"
	"path/filepath"
)

// HomeDir returns the CLI config directory (~/.codebuddy).
func HomeDir() string {
	if d := os.Getenv("CODEBUDDY_HOME"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".codebuddy")
	}
	return filepath.Join(home, ".codebuddy")
}

// LoggedIn reports whether the CLI has persisted credentials. It checks for
// the config directory's credential artifacts; a missing state simply means
// `codebuddy login` has not run on this machine.
func LoggedIn() bool {
	dir := HomeDir()
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	// The CLI keeps its session under local_storage/ or a credentials file;
	// treat the presence of settings or any credential artifact as logged in.
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "local_storage")); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		n := e.Name()
		if n == "auth.json" || n == "credentials.json" || n == ".credentials.json" {
			return true
		}
	}
	return len(entries) > 0
}

// APIKeyAuth holds an API key for headless/CI use.
type APIKeyAuth struct {
	APIKey  string
	BaseURL string
}

// Configured reports whether an API key was provided.
func (a APIKeyAuth) Configured() bool { return a.APIKey != "" }

// Env returns the environment variables that carry the credential to the CLI
// child process. Empty when no key is configured (the CLI then uses its own
// persisted login).
func (a APIKeyAuth) Env() map[string]string {
	if !a.Configured() {
		return nil
	}
	env := map[string]string{
		"CODEBUDDY_API_KEY":    a.APIKey,
		"ANTHROPIC_AUTH_TOKEN": a.APIKey,
	}
	if a.BaseURL != "" {
		env["CODEBUDDY_BASE_URL"] = a.BaseURL
		env["ANTHROPIC_BASE_URL"] = a.BaseURL
	}
	return env
}

// CLIVersion shells out to `codebuddy --version` (best-effort).
func CLIVersion(pathToCLI string) (string, bool) {
	path := pathToCLI
	if path == "" {
		if p := os.Getenv("CODEBUDDY_CODE_PATH"); p != "" {
			path = p
		} else {
			for _, name := range []string{"codebuddy", "cbc"} {
				if p, err := exec.LookPath(name); err == nil {
					path = p
					break
				}
			}
		}
	}
	if path == "" {
		return "", false
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}
