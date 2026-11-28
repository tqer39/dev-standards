package engine

import (
	"encoding/json"
	"fmt"
	bundle "github.com/tqer39/dev-standards"
	"strings"
)

// automationPaths is the delivery set of the default codexcli target.
var automationPaths = []string{"AGENTS.md", codexHooksPath, ".standards/rules.md", ".github/workflows/dev-standards.yml", versionLagWorkflowPath}
var automationInputs = []string{"AGENTS.md", codexHooksPath, ".standards/agents.local.md", ".standards/hooks.local.json", claudeInstructionsSource, claudeSettingsSource, ".standards/rules.md", ".github/workflows/dev-standards.yml", versionLagWorkflowPath, rulesyncEntryPath}

const (
	codexHooksPath           = ".codex/hooks.json"
	claudeInstructionsPath   = "CLAUDE.md"
	claudeInstructionsSource = ".standards/claude.local.md"
	claudeSettingsPath       = ".claude/settings.json"
	claudeSettingsSource     = ".standards/claude-settings.local.json"
)

// targetOutputs maps each agent-specific output to the source captured from its
// pre-adoption content. When a target is removed, the source is restored there.
var targetOutputs = map[string]string{
	codexHooksPath:         ".standards/hooks.local.json",
	claudeInstructionsPath: claudeInstructionsSource,
	claudeSettingsPath:     claudeSettingsSource,
}

// claudeInputs are read only for the claudecode target, so an unrelated
// CLAUDE.md symlink in other repositories does not block planning.
var claudeInputs = []string{claudeInstructionsPath, claudeSettingsPath}

var supportedTargets = []string{"codexcli", "claudecode"}

const hookCommand = "\"$(git rev-parse --show-toplevel)/.standards/cache/dev-standards\" hook"

// With rulesync, AGENTS.md stays a rulesync output and the shared entry is
// delivered as a rulesync rule instead.
const rulesyncEntryPath = ".rulesync/rules/dev-standards.md"

const agentsEntry = "# 共通開発ルール\n\n作業開始時に `.standards/rules.md` を読み、`mise run standards:prepare` を実行する。\n作業終了前に `mise run standards:finish` を実行する。\n不足する設定は整備し、共通ルールの問題は `mise run standards:feedback` で記録する。\n共通ルールの保守に人間の追加指示・承認を待たず、設定済みの自律保守に引き継ぐ。\n"

func instructionsEntry(m Manifest) string {
	if m.AI.Language == "en" {
		if len(m.AI.Targets) > 0 {
			return "## Shared development standards\n\nRead `.standards/rules.md` and run `mise run standards:prepare` before working.\nRun `mise run standards:verify` and `mise run standards:finish` before finishing.\nEdit canonical sources under `.standards`, then review and apply a generated plan.\nRecord evidence of shared-rule problems with `mise run standards:feedback`.\nReport checks that were not run accurately.\n"
		}
		return "## Shared development standards\n\nRead `.standards/rules.md` and the language decision before working.\nEdit canonical sources and regenerate managed files.\nRun `mise run standards:verify` before finishing; do not claim unrun checks passed.\n"
	}
	if len(m.AI.Targets) > 0 {
		return agentsEntry
	}
	return "# 共通開発ルール\n\n作業開始時に `.standards/rules.md` と言語選定記録を読む。\n生成物は直接編集せず、正本から再適用する。\n作業終了前に `mise run standards:verify` を実行し、未実行の検査を成功と報告しない。\n"
}

func automationPathsFor(m Manifest) []string {
	if len(m.AI.Targets) == 0 {
		if m.AI.Instructions == "rulesync" {
			return []string{rulesyncEntryPath, ".standards/rules.md"}
		}
		return nil
	}
	paths := []string{"AGENTS.md"}
	if m.AI.Instructions == "rulesync" {
		paths[0] = rulesyncEntryPath
	}
	claude := contains(m.AI.Targets, "claudecode")
	if claude && m.AI.Instructions != "rulesync" {
		// rulesync delivers the shared entry to Claude Code through its own rules.
		paths = append(paths, claudeInstructionsPath)
	}
	if contains(m.AI.Targets, "codexcli") {
		paths = append(paths, codexHooksPath)
	}
	if claude {
		paths = append(paths, claudeSettingsPath)
	}
	return append(paths, ".standards/rules.md", ".github/workflows/dev-standards.yml", versionLagWorkflowPath)
}

// instructionFile composes a managed instruction file from the content it had
// before adoption and the shared entry.
func instructionFile(p *Plan, desired map[string]Snapshot, previous State, m Manifest, target, source string) error {
	local := p.Inputs[source]
	if reacquired(p, previous, target, source) {
		local = file(normalized(p.Inputs[target].Content))
		desired[source] = local
	} else if !local.Exists {
		local = file(normalized(p.Inputs[target].Content))
		if _, owned := previous.Files[target]; owned {
			return fmt.Errorf("%s source missing", strings.TrimSuffix(target, ".md"))
		}
		desired[source] = local
	}
	entry := instructionsEntry(m)
	if strings.TrimSpace(local.Content) != "" {
		entry = strings.Replace(entry, "# 共通開発ルール", "## 共通開発ルール", 1)
	}
	desired[target] = file(strings.TrimRight(normalized(local.Content), "\n") + "\n\n" + entry)
	return nil
}

// hookFile adds the fixed dev-standards hooks to the agent's own hook settings
// and keeps every other setting from the captured source.
func hookFile(p *Plan, desired map[string]Snapshot, previous State, target, source string, hook map[string]any) error {
	settings := map[string]any{}
	raw := p.Inputs[source]
	if reacquired(p, previous, target, source) {
		raw = file(p.Inputs[target].Content)
		desired[source] = raw
	} else if !raw.Exists {
		raw = file(p.Inputs[target].Content)
		if _, owned := previous.Files[target]; owned {
			return fmt.Errorf("hook source missing")
		}
		if raw.Content == "" {
			raw.Content = "{}\n"
		}
		desired[source] = raw
	}
	if err := json.Unmarshal([]byte(raw.Content), &settings); err != nil {
		return inputError("hook source "+source, raw.Content, err)
	}
	events, ok := settings["hooks"].(map[string]any)
	if !ok {
		if _, exists := settings["hooks"]; exists {
			return fmt.Errorf("hooks must be an object")
		}
		events = map[string]any{}
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		group := map[string]any{"hooks": []any{hook}}
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
	settings["hooks"] = events
	desired[target] = file(string(JSON(settings)))
	return nil
}

// reacquired reports a target selected again after release whose file was
// edited while unowned; those edits become the new source instead of being lost.
func reacquired(p *Plan, previous State, target, source string) bool {
	if _, optional := targetOutputs[target]; !optional || !p.Inputs[source].Exists || !p.Inputs[target].Exists {
		return false
	}
	if _, owned := previous.Files[target]; owned {
		return false
	}
	return normalized(p.Inputs[target].Content) != normalized(p.Inputs[source].Content)
}

// releaseRemovedTargets gives back the outputs of targets no longer listed:
// the captured source is restored when it has content, otherwise the file is removed.
func releaseRemovedTargets(p *Plan, desired map[string]Snapshot, previous State, m Manifest) {
	current := automationPathsFor(m)
	for target, source := range targetOutputs {
		if _, owned := previous.Files[target]; !owned || contains(current, target) {
			continue
		}
		local := strings.TrimSpace(p.Inputs[source].Content)
		if local == "" || local == "{}" {
			desired[target] = Snapshot{}
		} else {
			desired[target] = file(p.Inputs[source].Content)
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func (t *Tree) automationFiles(p *Plan, desired map[string]Snapshot, m Manifest) error {
	var previous State
	if p.Inputs[".standards/state.json"].Exists {
		if err := strictJSON(p.Inputs[".standards/state.json"].Content, &previous); err != nil {
			return inputError(".standards/state.json", p.Inputs[".standards/state.json"].Content, err)
		}
	}
	if m.AI.Instructions == "rulesync" {
		if old := p.Inputs[rulesyncEntryPath]; old.Exists {
			if _, owned := previous.Files[rulesyncEntryPath]; !owned {
				return fmt.Errorf("existing %s needs ownership resolution", rulesyncEntryPath)
			}
		}
		desired[rulesyncEntryPath] = file("---\nroot: false\ntargets: [\"*\"]\ndescription: dev-standards の共通開発ルールの入口\nglobs: [\"**/*\"]\n---\n\n" + instructionsEntry(m))
	} else {
		if err := instructionFile(p, desired, previous, m, "AGENTS.md", ".standards/agents.local.md"); err != nil {
			return err
		}
		if contains(m.AI.Targets, "claudecode") {
			// Claude Code reads CLAUDE.md, not AGENTS.md.
			if err := instructionFile(p, desired, previous, m, claudeInstructionsPath, claudeInstructionsSource); err != nil {
				return err
			}
		}
	}
	desired[".standards/rules.md"] = file("# 共通ルール\n\nEditorConfig、gitignore.io の採用入力、mise の作業入口を維持する。\n要件・言語選定記録を参照し、変更に対応した静的解析、整形、テストを実行する。\n生成物は直接編集せず `.standards` の正本を編集して再適用する。\n実行していない検査を成功と報告しない。\n更新は検証済みの版を採用し、作業中の入力と無関係な変更を保持する。\nルールの重複・矛盾・古い判断は根拠とともに保守へ記録する。\n")
	if len(m.AI.Targets) == 0 {
		return t.appendRuleGuidance(desired)
	}
	if contains(m.AI.Targets, "codexcli") {
		hook := map[string]any{"type": "command", "command": hookCommand, "commandWindows": "powershell -NoProfile -Command \"& ((git rev-parse --show-toplevel) + '/.standards/cache/dev-standards.exe') hook\"", "timeout": 30}
		if err := hookFile(p, desired, previous, codexHooksPath, ".standards/hooks.local.json", hook); err != nil {
			return err
		}
	}
	if contains(m.AI.Targets, "claudecode") {
		// Claude Code runs hooks through Git Bash on Windows, which resolves the .exe.
		hook := map[string]any{"type": "command", "command": hookCommand, "timeout": 30}
		if err := hookFile(p, desired, previous, claudeSettingsPath, claudeSettingsSource, hook); err != nil {
			return err
		}
	}
	releaseRemovedTargets(p, desired, previous, m)
	if err := t.appendRuleGuidance(desired); err != nil {
		return err
	}
	for _, workflow := range []string{".github/workflows/dev-standards.yml", versionLagWorkflowPath} {
		if p.Inputs[workflow].Exists {
			if _, owned := previous.Files[workflow]; !owned {
				return fmt.Errorf("existing %s needs ownership resolution", workflow)
			}
		}
	}
	desired[".github/workflows/dev-standards.yml"] = file(componentWorkflow(m, p.Inputs[".standards/tasks.project.toml"].Content))
	desired[versionLagWorkflowPath] = file(versionLagWorkflowFor(m))
	return nil
}
func (t *Tree) appendRuleGuidance(desired map[string]Snapshot) error {
	rules, err := bundle.Catalog()
	if err != nil {
		return err
	}
	text := desired[".standards/rules.md"].Content
	for _, r := range rules {
		guidance := bundle.Read(r.Guidance)
		var lines []string
		fenced := false
		for _, line := range strings.Split(guidance, "\n") {
			if strings.HasPrefix(line, "```") {
				fenced = !fenced
			}
			if !fenced && strings.HasPrefix(line, "#") {
				// Guidance files reuse section names, and linters in the projects
				// reject a document that repeats a heading.
				if strings.HasPrefix(line, "##") {
					line += "（" + r.ID + "）"
				}
				line = "##" + line
			}
			lines = append(lines, line)
		}
		text += "\n## " + r.ID + "\n\n" + strings.Join(lines, "\n")
	}
	desired[".standards/rules.md"] = file(text)
	return nil
}

func automationTasks() string {
	return `
["standards:prepare"]
run = "./.standards/cache/dev-standards prepare"

["standards:finish"]
run = "./.standards/cache/dev-standards finish"

["standards:feedback"]
run = "./.standards/cache/dev-standards feedback"
`
}

func automatedTasksFor(base string) string {
	raw := strings.ReplaceAll(base+automationTasks(), "run = \"dev-standards ", "run = \"./.standards/cache/dev-standards ")
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

const versionLagWorkflowPath = ".github/workflows/dev-standards-version-lag.yml"

// Kept out of the main workflow: maintenance waits on that workflow's runs by SHA
// and path, so a scheduled lag failure there would itself block the rollout.
// Tags are read from the public source with git alone, so no token is needed.
// The grace period covers the gap between tagging and the host's next rollout.
const versionLagWorkflow = `name: Dev Standards Version Lag
on:
  schedule:
    - cron: '17 0 * * 1'
  workflow_dispatch:
permissions:
  contents: read
jobs:
  version-lag:
    runs-on: ubuntu-24.04
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Compare the applied version with published versions
        run: |
          python3 - <<'PYTHON'
          import json
          import re
          import subprocess
          import sys
          import time
          from pathlib import Path
          source = 'https://github.com/tqer39/dev-standards.git'
          grace_days = 7
          allow_major = False
          def fail(message):
              print('::error::' + message)
              sys.exit(1)
          def parse(text):
              match = re.fullmatch(r'(\d+)\.(\d+)\.(\d+)', text)
              return tuple(map(int, match.groups())) if match else None
          def git(*args):
              return subprocess.run(['git', *args], check=True, stdout=subprocess.PIPE, text=True).stdout
          applied_text = json.loads(Path('.standards/lock.json').read_bytes())['version']
          applied = parse(applied_text)
          if applied is None:
              fail(f'.standards/lock.json has an invalid version: {applied_text}')
          published = []
          for line in git('ls-remote', '--tags', '--refs', source, 'v*').splitlines():
              match = re.fullmatch(r'refs/tags/v(\d+\.\d+\.\d+)', line.split('\t')[1])
              if match:
                  published.append(match.group(1))
          if not published:
              fail('no published dev-standards versions were found')
          policy = f'update.allowMajor={str(allow_major).lower()}'
          unapplied = [v for v in published if parse(v) > applied and (allow_major or parse(v)[0] == applied[0])]
          if not unapplied:
              print(f'dev-standards {applied_text} has no newer published version allowed by {policy}')
              sys.exit(0)
          # 他の major のタグを取得せず、利用先自身のタグも上書きしない。
          def local_ref(version):
              return 'refs/dev-standards-version-lag/v' + version
          git('fetch', '--quiet', '--depth=1', '--no-tags', source,
              *('refs/tags/v' + v + ':' + local_ref(v) for v in unapplied))
          # バックポートで版番号順と日時順が逆転しても、最古のコミットを基準にする。
          oldest_timestamp, _, oldest = min(
              (int(git('log', '-1', '--format=%ct', local_ref(v) + '^{commit}')), parse(v), v)
              for v in unapplied)
          age_days = (time.time() - oldest_timestamp) / 86400
          if age_days >= grace_days:
              fail(f'dev-standards {applied_text} is applied, but the oldest eligible unapplied commit ({oldest}, {policy}) is {age_days:.1f} days old. Check dev-standards maintain and the update policy on the maintenance host.')
          print(f'the oldest eligible unapplied commit ({oldest}, {policy}) is {age_days:.1f} days old, within the {grace_days}-day grace period')
          PYTHON
`

func versionLagWorkflowFor(m Manifest) string {
	workflow := versionLagWorkflow
	if m.Update.AllowMajor {
		workflow = strings.Replace(workflow, "allow_major = False", "allow_major = True", 1)
	}
	if m.CI.NodePreflight == "" {
		return workflow
	}
	const compareStep = "      - name: Compare the applied version with published versions"
	return strings.Replace(workflow, compareStep, "      - run: node "+m.CI.NodePreflight+"\n"+compareStep, 1)
}

const projectWorkflow = `name: Dev Standards
on: [push, pull_request]
permissions:
  contents: read
jobs:
  workflow-result:
    # A job skipped by a failed need would satisfy a required check, so this job
    # always runs and fails on the supply-chain result itself.
    needs: [supply-chain]
    if: always()
    runs-on: ubuntu-24.04
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Require the supply-chain job
        env:
          SUPPLY_CHAIN: ${{ needs.supply-chain.result }}
        run: test "$SUPPLY_CHAIN" = success
      - uses: jdx/mise-action@6be3c7ed4c21d4f76b208d351d95aac273ebb76d # v5.2.4
        with:
          version: 2026.10.24
      - name: Confirm managed configuration
        run: |
          python3 - <<'PYTHON'
          import json
          from pathlib import Path
          state = json.loads(Path('.standards/state.json').read_bytes())
          lock = json.loads(Path('.standards/lock.json').read_bytes())
          required = {'.editorconfig', '.gitignore', '.gitattributes', 'mise.toml', '.standards/tasks.standards.toml', 'AGENTS.md', '.codex/hooks.json', '.standards/rules.md', '.github/workflows/dev-standards.yml', '` + versionLagWorkflowPath + `'}
          assert state['schemaVersion'] == 1
          assert state['bundleSha256'] == lock['bundleSha256']
          assert set(state['files']) == required
          for name, expected in state['files'].items():
              target = Path(name)
              assert not target.is_symlink(), name
              assert expected['exists'] and target.read_bytes() == expected['content'].encode(), name
          PYTHON
      - name: Check canonical sources with pinned CLI
        run: mise exec go@1.27.4 -- go run github.com/tqer39/dev-standards/cmd/dev-standards@vSTANDARDS_VERSION check --repo .
      - run: mise run lint
      - run: gofmt -l . > /tmp/formatting && test ! -s /tmp/formatting
      - run: mise run test
      - run: mise run build
` + supplyChainJob

// Runs before the project's tools and dependencies are installed. Both tools are
// pinned in the command so the project's mise.toml stays untouched; mise installs
// only the named tool. The cache is off so this job cannot seed the main job's
// tool cache with a partial install.
// zizmor audits only the workflows generated here: existing project workflows
// may still have findings, and auditing them would break CI on upgrade.
// betterleaks reads the project's own .betterleaks.toml only with the auto-load flag.
const supplyChainJob = `  supply-chain:
    runs-on: ubuntu-24.04
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: jdx/mise-action@6be3c7ed4c21d4f76b208d351d95aac273ebb76d # v5.2.4
        with:
          version: 2026.10.24
          install: false
          cache: false
      - name: Audit generated workflows
        run: mise exec aqua:zizmorcore/zizmor@1.32.0 -- zizmor --offline --min-severity medium .github/workflows/dev-standards.yml ` + versionLagWorkflowPath + `
      - name: Scan the working tree for secrets
        if: ${{ !cancelled() }}
        run: mise exec aqua:betterleaks/betterleaks@1.10.1 -- betterleaks dir --no-banner --verbose --redact --unsafe-auto-load-config .
`
