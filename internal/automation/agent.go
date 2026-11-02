package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Decision struct {
	Approved bool   `json:"approved"`
	Reason   string `json:"reason"`
}

const resultSchema = `{"type":"object","properties":{"approved":{"type":"boolean"},"reason":{"type":"string"}},"required":["approved","reason"],"additionalProperties":false}`

func Agent(ctx context.Context, x Executor, dir, prompt string, review bool) (Decision, error) {
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
	if err = json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	if d.Reason == "" {
		return d, fmt.Errorf("agent did not provide review evidence")
	}
	return d, nil
}
