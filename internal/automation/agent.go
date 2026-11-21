package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Decision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

const resultSchema = `{"type":"object","properties":{"approved":{"type":"boolean"},"reason":{"type":"string"}},"required":["approved","reason"],"additionalProperties":false}`

// Read-only Git commands that both the proposer and the reviewer may run.
var claudeGitReads = []string{"Bash(git diff *)", "Bash(git diff)", "Bash(git status *)", "Bash(git status)", "Bash(git log *)", "Bash(git show *)"}

// CheckLogin confirms the agent's saved subscription login; API keys are never used.
func CheckLogin(ctx context.Context, x Executor, agent, dir string) error {
	if agent == "claude" {
		out, err := x.Run(ctx, dir, "claude", []string{"auth", "status", "--json"}, "")
		var status struct {
			LoggedIn   bool   `json:"loggedIn"`
			AuthMethod string `json:"authMethod"`
		}
		if err != nil || json.Unmarshal([]byte(out), &status) != nil || !status.LoggedIn || status.AuthMethod != "claude.ai" {
			return fmt.Errorf("saved claude.ai Claude Code login is required; no API key is used")
		}
		return nil
	}
	auth, err := x.Run(ctx, dir, "codex", []string{"login", "status"}, "")
	if err != nil || !strings.Contains(auth, "ChatGPT") {
		return fmt.Errorf("saved ChatGPT Codex login is required; no API key is used")
	}
	return nil
}

func Agent(ctx context.Context, x Executor, agent, dir, prompt string, review bool) (Decision, error) {
	var d Decision
	var err error
	if agent == "claude" {
		d, err = claudeAgent(ctx, x, dir, prompt, review)
	} else {
		d, err = codexAgent(ctx, x, dir, prompt, review)
	}
	if err != nil {
		return d, err
	}
	if d.Reason == "" {
		return d, fmt.Errorf("agent did not provide review evidence")
	}
	return d, nil
}

func codexAgent(ctx context.Context, x Executor, dir, prompt string, review bool) (Decision, error) {
	var d Decision
	outDir, err := os.MkdirTemp("", "dev-standards-agent-")
	if err != nil {
		return d, err
	}
	defer os.RemoveAll(outDir)
	schema := filepath.Join(outDir, "schema.json")
	result := filepath.Join(outDir, "result.json")
	if err = os.WriteFile(schema, []byte(resultSchema), 0600); err != nil {
		return d, err
	}
	sandbox := "workspace-write"
	if review {
		sandbox = "read-only"
	}
	args := []string{"exec", "--ignore-user-config", "--ephemeral", "--sandbox", sandbox, "-c", "approval_policy=\"never\"", "-c", "features.hooks=false", "--output-schema", schema, "--output-last-message", result, "-"}
	if _, err = x.Run(ctx, dir, "codex", args, prompt); err != nil {
		return d, err
	}
	b, err := os.ReadFile(result)
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(b, &d)
	return d, err
}

// claudeAgent mirrors the Codex sandbox with tool restrictions: dontAsk denies
// every tool call outside the allow list, and no user, project or MCP settings
// are loaded so the checkout cannot widen permissions or start hooks.
func claudeAgent(ctx context.Context, x Executor, dir, prompt string, review bool) (Decision, error) {
	var d Decision
	tools := "Read,Glob,Grep,Bash"
	allowed := append([]string{"Read", "Glob", "Grep"}, claudeGitReads...)
	if !review {
		tools = "Read,Edit,Write,Glob,Grep,Bash"
		allowed = append(allowed, "Edit", "Write")
	}
	args := []string{"-p", "--output-format", "json", "--json-schema", resultSchema, "--no-session-persistence", "--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--strict-mcp-config", "--permission-mode", "dontAsk", "--tools", tools, "--allowedTools"}
	out, err := x.Run(ctx, dir, "claude", append(args, allowed...), prompt)
	if err != nil {
		return d, err
	}
	var result struct {
		IsError          bool            `json:"is_error"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		return d, fmt.Errorf("claude output: %w", err)
	}
	if result.IsError || len(result.StructuredOutput) == 0 {
		return d, fmt.Errorf("claude did not return a structured result: %s", result.Result)
	}
	err = json.Unmarshal(result.StructuredOutput, &d)
	return d, err
}
