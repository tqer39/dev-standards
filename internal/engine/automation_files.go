package engine

import (
	"encoding/json"
	"fmt"
	bundle "github.com/tqer39/dev-standards"
	"strings"
)

var automationPaths = []string{"AGENTS.md", ".codex/hooks.json", ".standards/rules.md", ".github/workflows/dev-standards.yml"}
var automationInputs = []string{"AGENTS.md", ".codex/hooks.json", ".standards/agents.local.md", ".standards/hooks.local.json", ".standards/rules.md", ".github/workflows/dev-standards.yml"}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func (t *Tree) automationFiles(p *Plan, desired map[string]Snapshot) error {
	var previous State
	if p.Inputs[".standards/state.json"].Exists {
		if err := strictJSON(p.Inputs[".standards/state.json"].Content, &previous); err != nil {
			return err
		}
	}
	local := p.Inputs[".standards/agents.local.md"]
	if !local.Exists {
		local = file(p.Inputs["AGENTS.md"].Content)
		if _, owned := previous.Files["AGENTS.md"]; owned {
			return fmt.Errorf("AGENTS source missing")
		}
		desired[".standards/agents.local.md"] = local
	}
	desired["AGENTS.md"] = file(strings.TrimRight(local.Content, "\n") + "\n\n# 共通開発ルール\n\n作業開始時に `.standards/rules.md` を読み、`mise run standards:prepare` を実行する。\n作業終了前に `mise run standards:finish` を実行する。\n不足する設定は整備し、共通ルールの問題は `dev-standards feedback` で記録する。\n共通ルールの保守に人間の追加指示・承認を待たず、設定済みの自律保守に引き継ぐ。\n")
	desired[".standards/rules.md"] = file("# 共通ルール\n\nEditorConfig、gitignore.io の採用入力、mise の作業入口を維持する。\n要件・言語選定記録を参照し、変更に対応した静的解析、整形、テストを実行する。\n生成物は直接編集せず `.standards` の正本を編集して再適用する。\n実行していない検査を成功と報告しない。\n更新は検証済みの版を採用し、作業中の入力と無関係な変更を保持する。\nルールの重複・矛盾・古い判断は根拠とともに保守へ記録する。\n")
	hooks := map[string]any{}
	raw := p.Inputs[".standards/hooks.local.json"]
	if !raw.Exists {
		raw = file(p.Inputs[".codex/hooks.json"].Content)
		if _, owned := previous.Files[".codex/hooks.json"]; owned {
			return fmt.Errorf("hook source missing")
		}
		if raw.Content == "" {
			raw.Content = "{}\n"
		}
		desired[".standards/hooks.local.json"] = raw
	}
	if err := json.Unmarshal([]byte(raw.Content), &hooks); err != nil {
		return fmt.Errorf("hook source: %w", err)
	}
	events, ok := hooks["hooks"].(map[string]any)
	if !ok {
		if _, exists := hooks["hooks"]; exists {
			return fmt.Errorf("hooks must be an object")
		}
		events = map[string]any{}
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "\"$(git rev-parse --show-toplevel)/.standards/cache/dev-standards\" hook", "commandWindows": "powershell -NoProfile -Command \"& ((git rev-parse --show-toplevel) + '/.standards/cache/dev-standards.exe') hook\"", "timeout": 30}}}
		old, exists := events[event]
		if exists {
			groups, ok := old.([]any)
			if !ok {
				return fmt.Errorf("hook event must be an array")
			}
			events[event] = append(groups, group)
		} else {
			events[event] = []any{group}
		}
	}
	hooks["hooks"] = events
	desired[".codex/hooks.json"] = file(string(JSON(hooks)))
	rules, err := bundle.Catalog()
	if err != nil {
		return err
	}
	text := desired[".standards/rules.md"].Content
	for _, r := range rules {
		text += "\n## " + r.ID + "\n\n" + bundle.Read(r.Guidance)
	}
	desired[".standards/rules.md"] = file(text)
	old := p.Inputs[".github/workflows/dev-standards.yml"]
	if old.Exists {
		if _, owned := previous.Files[".github/workflows/dev-standards.yml"]; !owned {
			return fmt.Errorf("existing dev-standards workflow needs ownership resolution")
		}
	}
	desired[".github/workflows/dev-standards.yml"] = file(projectWorkflow)
	return nil
}
func automationTasks() string {
	return `
["standards:prepare"]
run = "./.standards/cache/dev-standards prepare"

["standards:finish"]
run = "./.standards/cache/dev-standards finish"
`
}

func automatedTasks() string {
	raw := strings.ReplaceAll(tasks()+automationTasks(), "run = \"dev-standards ", "run = \"./.standards/cache/dev-standards ")
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		lines = append(lines, line)
		if command, ok := strings.CutPrefix(line, `run = "./.standards/cache/dev-standards `); ok {
			command = strings.TrimSuffix(command, `"`)
			lines = append(lines, `run_windows = '.\.standards\cache\dev-standards.exe `+command+`'`)
		}
	}
	return strings.Join(lines, "\n")
}

const projectWorkflow = `name: Dev Standards
on: [push, pull_request]
permissions:
  contents: read
jobs:
  workflow-result:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
      - uses: jdx/mise-action@6be3c7ed4c21d4f76b208d351d95aac273ebb76d
        with:
          version: 2026.10.20
      - name: Confirm managed configuration
        run: |
          python3 - <<'PYTHON'
          import json
          from pathlib import Path
          state = json.loads(Path('.standards/state.json').read_bytes())
          lock = json.loads(Path('.standards/lock.json').read_bytes())
          required = {'.editorconfig', '.gitignore', '.gitattributes', 'mise.toml', '.standards/tasks.standards.toml', 'AGENTS.md', '.codex/hooks.json', '.standards/rules.md', '.github/workflows/dev-standards.yml'}
          assert state['schemaVersion'] == 1
          assert state['bundleSha256'] == lock['bundleSha256']
          assert set(state['files']) == required
          for name, expected in state['files'].items():
              target = Path(name)
              assert not target.is_symlink(), name
              assert expected['exists'] and target.read_bytes() == expected['content'].encode(), name
          PYTHON
      - run: mise run lint
      - run: gofmt -l . > /tmp/formatting && test ! -s /tmp/formatting
      - run: mise run test
      - run: mise run build
`
