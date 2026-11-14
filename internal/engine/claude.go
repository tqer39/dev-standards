package engine

import (
	"fmt"

	bundle "github.com/tqer39/dev-standards"
)

// Claude loads project rules without requiring a personal CLAUDE.md or skill.
const claudeRulePath = ".claude/rules/dev-standards-agent-harness.md"

func (t *Tree) claudeFiles(p *Plan, desired map[string]Snapshot, m Manifest) error {
	var previous State
	if p.Inputs[".standards/state.json"].Exists {
		if err := strictJSON(p.Inputs[".standards/state.json"].Content, &previous); err != nil {
			return err
		}
	}
	if p.Inputs[claudeRulePath].Exists {
		if _, owned := previous.Files[claudeRulePath]; !owned {
			return fmt.Errorf("existing %s needs ownership resolution", claudeRulePath)
		}
	}
	template := "templates/base/claude-agent-harness.md"
	if m.AI.Language == "en" {
		template = "templates/base/claude-agent-harness.en.md"
	}
	desired[claudeRulePath] = file(bundle.Read(template))
	return nil
}
