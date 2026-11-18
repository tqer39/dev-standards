package engine

import (
	"errors"
	"strings"
	"testing"

	bundle "github.com/tqer39/dev-standards"
)

func TestSharedGuidanceDeliveryRollbackAndConflict(t *testing.T) {
	for _, tc := range []struct {
		name         string
		instructions string
		automation   bool
		language     string
	}{
		{"direct", "", true, ""},
		{"rulesync", "rulesync", true, ""},
		{"rulesync-only", "rulesync", false, ""},
		{"rulesync-english", "rulesync", true, "en"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := fixture(t)
			p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: tc.automation, Instructions: tc.instructions, InstructionLanguage: tc.language})
			if err != nil {
				t.Fatal(err)
			}
			// 共通ルールを書いた後の失敗でも、初回適用を残さない。
			if err := tree.apply(p, func(i int) error {
				if i == len(p.Changes)-1 {
					return errors.New("書き込み失敗")
				}
				return nil
			}); err == nil {
				t.Fatal("書き込み失敗が成功になった")
			}
			for name, want := range p.Inputs {
				got, err := tree.Read(name)
				if err != nil || !equal(got, want) {
					t.Fatalf("失敗後に入力を復元しなかった: %s %v", name, err)
				}
			}
			if err := tree.Apply(p); err != nil {
				t.Fatal(err)
			}
			shared := read(t, tree.Root, ".standards/rules.md")
			if !strings.Contains(shared, "## PROCESS-PROJECT-TRACKING") || !strings.Contains(shared, "#### 開始・進捗・終了") {
				t.Fatal("進捗更新の手順が配布されていない")
			}
			if strings.Count(shared, ":robot: これは AI で書かれています。") != 1 || !strings.Contains(shared, "## PROCESS-AI-DISCLOSURE") {
				t.Fatal("AI の実装関与を明示する共通ルールが配布されていない")
			}
			if !strings.Contains(shared, "## PROCESS-JAPANESE-WRITING") || !strings.Contains(shared, "#### 導入と検証") {
				t.Fatal("日本語の出力に関する共通ルールが配布されていない")
			}
			if tc.instructions == "rulesync" {
				entry := read(t, tree.Root, rulesyncEntryPath)
				if !strings.Contains(entry, `targets: ["*"]`) || !strings.Contains(entry, "`.standards/rules.md`") {
					t.Fatal("全エージェント向けの共通ルール参照がない")
				}
				var m Manifest
				if tc.automation {
					m.AI.Targets = []string{"codexcli"}
				}
				m.AI.Instructions = tc.instructions
				m.AI.Language = tc.language
				write(t, tree.Root, "AGENTS.md", instructionsEntry(m))
			}
			again, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
				t.Fatalf("再適用で差分が生じた: %+v %v", again, err)
			}
			write(t, tree.Root, ".standards/rules.md", shared+"\n利用者の未反映の編集。\n")
			conflict, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(conflict.Conflicts) == 0 {
				t.Fatalf("ルールの手編集を検出しなかった: %+v %v", conflict, err)
			}
			if err := tree.Apply(conflict); err == nil {
				t.Fatal("手編集を上書きした")
			}
			if got := read(t, tree.Root, ".standards/rules.md"); got != shared+"\n利用者の未反映の編集。\n" {
				t.Fatal("衝突時に手編集を保持しなかった")
			}
		})
	}
}

func TestSharedGuidanceUpgradeFromPreviousGuidance(t *testing.T) {
	for _, rule := range []string{"PROCESS-PROJECT-TRACKING", "PROCESS-AI-DISCLOSURE", "PROCESS-JAPANESE-WRITING"} {
		t.Run(rule, func(t *testing.T) {
			tree := adoptWithAutomation(t)
			path := ".standards/rules.md"
			shared := read(t, tree.Root, path)
			before, removed, ok := strings.Cut(shared, "\n## "+rule+"\n")
			if !ok {
				t.Fatal("更新元に対象の共通ルールがない")
			}
			if _, after, found := strings.Cut(removed, "\n## "); found {
				before += "\n## " + after
			}
			write(t, tree.Root, path, before)
			var lock Lock
			var state State
			var manifest Manifest
			if err := strictJSON(read(t, tree.Root, ".standards/lock.json"), &lock); err != nil {
				t.Fatal(err)
			}
			if err := strictJSON(read(t, tree.Root, ".standards/state.json"), &state); err != nil {
				t.Fatal(err)
			}
			if err := bundle.Decode([]byte(read(t, tree.Root, ".standards/repository.yml")), "repository", &manifest); err != nil {
				t.Fatal(err)
			}
			lock.Version = "0.4.18"
			lock.Bundle = strings.Repeat("a", 64)
			state.Bundle = lock.Bundle
			state.Files[path] = file(before)
			manifest.Standard.Version = lock.Version
			write(t, tree.Root, ".standards/lock.json", string(JSON(lock)))
			write(t, tree.Root, ".standards/state.json", string(JSON(state)))
			write(t, tree.Root, ".standards/repository.yml", string(JSON(manifest)))
			plan, err := tree.Plan(Request{Mode: "upgrade"})
			if err != nil {
				t.Fatal(err)
			}
			if err := tree.Apply(plan); err != nil {
				t.Fatal(err)
			}
			if got := read(t, tree.Root, path); got != shared {
				t.Fatal("更新計画で新ルールを配布できなかった")
			}
			again, err := tree.Plan(Request{Mode: "reapply"})
			if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
				t.Fatalf("更新後の再適用に差分がある: %+v %v", again, err)
			}
			report, err := tree.Check()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, result := range report.Results {
				if result.Rule == rule {
					found = true
					if result.Status != "review_required" {
						t.Fatal("外部の遵守確認を実行済みと誤認した")
					}
				}
			}
			if !found {
				t.Fatal("共通ルールの検査結果がない")
			}
		})
	}
}
