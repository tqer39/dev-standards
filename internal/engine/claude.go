package engine

// 旧版で記録した所有ファイルの移行だけに使う。
const claudeRulePath = ".claude/rules/dev-standards-agent-harness.md"

func (t *Tree) claudeFiles(p *Plan, desired map[string]Snapshot) error {
	var previous State
	if p.Inputs[".standards/state.json"].Exists {
		if err := strictJSON(p.Inputs[".standards/state.json"].Content, &previous); err != nil {
			return inputError(".standards/state.json", p.Inputs[".standards/state.json"].Content, err)
		}
	}
	if !previous.Files[claudeRulePath].Exists {
		// 個人のルールなど、所有していないファイルは移行対象に含めない。
		return nil
	}
	current, err := t.Read(claudeRulePath)
	if err != nil {
		return err
	}
	p.Inputs[claudeRulePath] = current
	desired[claudeRulePath] = Snapshot{}
	return nil
}
