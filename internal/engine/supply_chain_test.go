package engine

import (
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const mainWorkflowPath = ".github/workflows/dev-standards.yml"

type supplyChainStep struct {
	Name string            `yaml:"name"`
	Uses string            `yaml:"uses"`
	Run  string            `yaml:"run"`
	If   string            `yaml:"if"`
	With map[string]string `yaml:"with"`
	Env  map[string]string `yaml:"env"`
}

type supplyChainDocument struct {
	Jobs map[string]struct {
		Needs  []string          `yaml:"needs"`
		If     string            `yaml:"if"`
		RunsOn string            `yaml:"runs-on"`
		Steps  []supplyChainStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func parseSupplyChain(t *testing.T, content string) supplyChainDocument {
	t.Helper()
	var document supplyChainDocument
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func supplyChainGate(t *testing.T, steps []supplyChainStep) (int, supplyChainStep) {
	t.Helper()
	for i, step := range steps {
		if step.Name == "Require the supply-chain job" {
			return i, step
		}
	}
	t.Fatal("required job lacks the supply-chain gate")
	return 0, supplyChainStep{}
}

func requiredJobName(profile string) string {
	if profile == "go" {
		return "workflow-result"
	}
	return "standards-check"
}

// The required check of each project is the existing job, so the new job only
// protects a project when that job waits for it and fails with it.
func TestSupplyChainJobGatesRequiredJobInEveryProfile(t *testing.T) {
	for _, profile := range []string{"go", "node-npm", "python-uv", "mise-project"} {
		t.Run(profile, func(t *testing.T) {
			m := defaultManifest()
			m.Components[0].Profile = profile
			document := parseSupplyChain(t, componentWorkflow(m, ""))
			if len(document.Jobs) != 2 {
				t.Fatalf("unexpected jobs: %v", document.Jobs)
			}
			required, ok := document.Jobs[requiredJobName(profile)]
			if !ok {
				t.Fatal("required job name changed")
			}
			if len(required.Needs) != 1 || required.Needs[0] != "supply-chain" || required.If != "always()" {
				t.Fatalf("required job can be skipped or does not wait: needs=%v if=%q", required.Needs, required.If)
			}
			at, gate := supplyChainGate(t, required.Steps)
			if gate.Env["SUPPLY_CHAIN"] != "${{ needs.supply-chain.result }}" || gate.Run != `test "$SUPPLY_CHAIN" = success` || gate.If != "" {
				t.Fatalf("required job does not fail with the supply-chain job: %+v", gate)
			}
			// Only checkout may precede the gate: the job must fail before it installs tools or runs project code.
			if at != 1 || !strings.HasPrefix(required.Steps[0].Uses, "actions/checkout@") {
				t.Fatalf("gate must directly follow checkout: %+v", required.Steps[:at+1])
			}
			job := document.Jobs["supply-chain"]
			if job.RunsOn != "ubuntu-24.04" || len(job.Needs) != 0 {
				t.Fatalf("supply-chain job must start first on Linux: %+v", job)
			}
			var audit, secrets string
			for _, step := range job.Steps {
				switch {
				case strings.HasPrefix(step.Uses, "actions/checkout@"):
					if step.With["persist-credentials"] != "false" {
						t.Fatal("checkout keeps credentials")
					}
				case strings.HasPrefix(step.Uses, "jdx/mise-action@"):
					if step.With["install"] != "false" || step.With["cache"] != "false" {
						t.Fatalf("job installs project tools or shares the tool cache: %v", step.With)
					}
				case step.Name == "Audit generated workflows":
					audit = step.Run
				case step.Name == "Scan the working tree for secrets":
					secrets = step.Run
					if step.If != "${{ !cancelled() }}" {
						t.Fatal("secret scan is skipped when the workflow audit fails")
					}
				}
			}
			if audit != "mise exec aqua:zizmorcore/zizmor@1.32.0 -- zizmor --offline --min-severity medium "+mainWorkflowPath+" "+versionLagWorkflowPath {
				t.Fatalf("audit must be pinned and limited to generated workflows: %s", audit)
			}
			if secrets != "mise exec aqua:betterleaks/betterleaks@1.10.1 -- betterleaks dir --no-banner --verbose --redact --unsafe-auto-load-config ." {
				t.Fatalf("secret scan must be pinned, redacted, and limited to the working tree: %s", secrets)
			}
		})
	}
}

func TestRequiredJobFailsUnlessSupplyChainSucceeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("generated steps run on GitHub-hosted Linux")
	}
	_, gate := supplyChainGate(t, parseSupplyChain(t, componentWorkflow(defaultManifest(), "")).Jobs["workflow-result"].Steps)
	for result, pass := range map[string]bool{"success": true, "failure": false, "cancelled": false, "skipped": false, "": false} {
		_, err := runWorkflowStep(t, t.TempDir(), gate.Run, []string{"SUPPLY_CHAIN=" + result})
		if (err == nil) != pass {
			t.Fatalf("supply-chain result %q: %v", result, err)
		}
	}
}

// Profile-specific rewrites replace the first match, so they must keep targeting
// the project job and leave the supply-chain job on Linux without project steps.
func TestSupplyChainJobIsUnaffectedByProjectSettings(t *testing.T) {
	m := defaultManifest()
	m.Components[0].Profile = "node-npm"
	m.CI.NodePreflight = "scripts/check.mjs"
	m.Companions = []Companion{{Profile: "swift", Path: "app"}}
	document := parseSupplyChain(t, componentWorkflow(m, "[\"project:bootstrap\"]\nrun = \"true\"\n"))
	if document.Jobs["standards-check"].RunsOn != "macos-26" || document.Jobs["supply-chain"].RunsOn != "ubuntu-24.04" {
		t.Fatal("companion runner moved to the wrong job")
	}
	for name, job := range document.Jobs {
		preflight, mise := -1, -1
		for i, step := range job.Steps {
			if step.Run == "node scripts/check.mjs" {
				preflight = i
			}
			if strings.HasPrefix(step.Uses, "jdx/mise-action@") {
				mise = i
			}
			if name == "supply-chain" && (strings.Contains(step.Run, "npm ci") || strings.Contains(step.Run, "mise run")) {
				t.Fatalf("supply-chain job runs project code: %s", step.Run)
			}
		}
		if preflight < 0 || preflight > mise {
			t.Fatalf("%s installs mise before the existing preflight", name)
		}
	}
}

// A project's preflight may reject a job that has a run step, or any action
// other than checkout, before it (kazokubi's scripts/check-ci-security.mjs).
// The generated jobs must pass such a check without an exception on the project side.
func TestNodePreflightDirectlyFollowsCheckoutInEveryJob(t *testing.T) {
	for _, profile := range []string{"go", "node-npm", "python-uv", "mise-project"} {
		t.Run(profile, func(t *testing.T) {
			m := defaultManifest()
			m.Components[0].Profile = profile
			m.CI.NodePreflight = "scripts/check.mjs"
			workflows := map[string]string{
				mainWorkflowPath:       componentWorkflow(m, ""),
				versionLagWorkflowPath: versionLagWorkflowFor(m),
			}
			for path, content := range workflows {
				for name, job := range parseSupplyChain(t, content).Jobs {
					if len(job.Steps) < 2 || !strings.HasPrefix(job.Steps[0].Uses, "actions/checkout@") || job.Steps[1].Run != "node scripts/check.mjs" {
						t.Fatalf("%s %s: preflight must directly follow checkout: %+v", path, name, job.Steps)
					}
				}
			}
			required := parseSupplyChain(t, workflows[mainWorkflowPath]).Jobs[requiredJobName(profile)]
			if at, _ := supplyChainGate(t, required.Steps); at != 2 {
				t.Fatalf("gate must follow the preflight: %+v", required.Steps[:at+1])
			}
		})
	}
}

// An adopted project receives the job through an upgrade plan: the previous
// workflow is replaced atomically, and a human edit still blocks the update.
func TestSupplyChainJobReachesAdoptedProjectByUpgrade(t *testing.T) {
	tree := adoptWithAutomation(t)
	current := read(t, tree.Root, mainWorkflowPath)
	if !strings.Contains(current, "\n  supply-chain:\n") {
		t.Fatal("first adoption lacks the supply-chain job")
	}
	previous := strings.Replace(current, supplyChainJob, "", 1)
	previous = strings.Replace(previous, "    needs: [supply-chain]\n    if: always()\n", "", 1)
	if previous == current || strings.Contains(previous, "needs:") {
		t.Fatal("fixture does not reproduce the previous workflow")
	}
	var state State
	if err := json.Unmarshal([]byte(read(t, tree.Root, ".standards/state.json")), &state); err != nil {
		t.Fatal(err)
	}
	state.Files[mainWorkflowPath] = file(previous)
	write(t, tree.Root, ".standards/state.json", string(JSON(state)))
	write(t, tree.Root, mainWorkflowPath, previous)
	commit(t, tree.Root)
	plan, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(plan.Conflicts) != 0 {
		t.Fatalf("upgrade plan: %+v %v", plan, err)
	}
	injected := errors.New("write failure")
	if err := tree.apply(plan, func(i int) error {
		if i == 1 {
			return injected
		}
		return nil
	}); !errors.Is(err, injected) {
		t.Fatalf("rollback: %v", err)
	}
	if read(t, tree.Root, mainWorkflowPath) != previous {
		t.Fatal("failed upgrade changed the workflow")
	}
	if err := tree.Apply(plan); err != nil {
		t.Fatal(err)
	}
	if read(t, tree.Root, mainWorkflowPath) != current {
		t.Fatal("upgrade did not deliver the supply-chain job")
	}
	commit(t, tree.Root)
	again, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(again.Changes) != 0 || len(again.Conflicts) != 0 {
		t.Fatalf("not idempotent: %+v %v", again, err)
	}
	write(t, tree.Root, mainWorkflowPath, strings.Replace(current, supplyChainJob, "", 1))
	conflict, err := tree.Plan(Request{Mode: "reapply"})
	if err != nil || len(conflict.Conflicts) != 1 {
		t.Fatalf("removed job is not protected: %+v %v", conflict, err)
	}
	if err := tree.Apply(conflict); err == nil {
		t.Fatal("overwrote a human edit")
	}
}
