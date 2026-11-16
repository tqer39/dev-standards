package engine

import (
	"errors"
	"strings"
	"testing"

	bundle "github.com/tqer39/dev-standards"
)

func TestProjectTrackingDeliveryRollbackAndConflict(t *testing.T) {
	for _, instructions := range []string{"", "rulesync"} {
		t.Run(instructions, func(t *testing.T) {
			tree := fixture(t)
			p, err := tree.Plan(Request{Mode: "adopt", Profile: "go", DecisionPath: "decision.md", Automation: true, Instructions: instructions})
			if err != nil {
				t.Fatal(err)
			}
			// 新しい配布ルールを書いた後の失敗でも、初回適用を残さない。
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
			if instructions == "rulesync" {
				var m Manifest
				m.AI.Targets = []string{"codex"}
				m.AI.Instructions = instructions
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

func TestProjectTrackingUpgradeFromPreviousGuidance(t *testing.T) {
	tree := adoptWithAutomation(t)
	path := ".standards/rules.md"
	shared := read(t, tree.Root, path)
	before, _, ok := strings.Cut(shared, "\n## PROCESS-PROJECT-TRACKING\n")
	if !ok {
		t.Fatal("更新元に進捗ルールがない")
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
		if result.Rule == "PROCESS-PROJECT-TRACKING" {
			found = true
			if result.Status != "review_required" {
				t.Fatal("GitHub を更新済みと誤認した")
			}
		}
	}
	if !found {
		t.Fatal("進捗ルールの検査結果がない")
	}
}
