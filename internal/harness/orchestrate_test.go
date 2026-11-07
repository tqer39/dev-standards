//go:build !windows

package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeAgents struct {
	implOutcomes []ImplOutcome
	reviews      [][]byte
	reviewOut    ReviewOutcome
	sent         []string
	started      int
	reviewed     int
	prompts      []string
}

func (f *fakeAgents) StartImplementer(context.Context, *Record, string, string) error {
	f.started++
	return nil
}
func (f *fakeAgents) SendImplementer(_ context.Context, _ *Record, text string) error {
	f.sent = append(f.sent, text)
	return nil
}
func (f *fakeAgents) ImplementerAlive(context.Context, *Record) (bool, error) { return true, nil }
func (f *fakeAgents) WaitImplementer(context.Context, *Record, string, time.Duration) (ImplOutcome, Result, error) {
	outcome := ImplDone
	if len(f.implOutcomes) > 0 {
		outcome, f.implOutcomes = f.implOutcomes[0], f.implOutcomes[1:]
	}
	return outcome, Result{Status: "done"}, nil
}
func (f *fakeAgents) Review(_ context.Context, _ *Record, _, prompt string, _ time.Duration) (ReviewOutcome, []byte, error) {
	f.reviewed++
	f.prompts = append(f.prompts, prompt)
	if f.reviewOut != ReviewDone {
		return f.reviewOut, nil, nil
	}
	data := f.reviews[0]
	if len(f.reviews) > 1 {
		f.reviews = f.reviews[1:]
	}
	return ReviewDone, data, nil
}

type fakeTree struct {
	reviewDir   string
	verifyFails int
	changes     []Change
	applyErr    error
	commits     int
	reverted    bool
}

func (f *fakeTree) PathsFor(*Record) Paths {
	return Paths{Trusted: "/t", Work: "/w", Review: f.reviewDir}
}
func (f *fakeTree) Prepare(_ context.Context, rec *Record, _ []byte) (Paths, error) {
	rec.BaseSHA, rec.AcceptanceSHA = "base", "base"
	return f.PathsFor(rec), nil
}
func (f *fakeTree) Verify(context.Context, string, []string) (bool, string, error) {
	if f.verifyFails > 0 {
		f.verifyFails--
		return false, "failed", nil
	}
	return true, "", nil
}
func (f *fakeTree) Export(context.Context, string) ([]byte, error) { return []byte("patch"), nil }
func (f *fakeTree) Apply(context.Context, *Record, []byte) error   { return f.applyErr }
func (f *fakeTree) Changes(context.Context, *Record, string) ([]Change, error) {
	return f.changes, nil
}
func (f *fakeTree) Commit(context.Context, *Record) (string, error) {
	f.commits++
	return "abc", nil
}
func (f *fakeTree) Push(context.Context, *Record, string) error { return nil }
func (f *fakeTree) StagedPatch(context.Context, *Record) (string, error) {
	return "diff", nil
}
func (f *fakeTree) Revert(context.Context, *Record) (string, string, error) {
	f.reverted = true
	return "agent/revert-r1", "rev", nil
}

type fakeForge struct {
	checks      []bool
	prs         int
	statuses    []string
	armed       []string
	needsHuman  []string
	mergeResult string
	comments    int
	threads     [][]ReviewThread
	resolved    []string
	waited      []string
}

func (f *fakeForge) UnresolvedThreads(context.Context, int) ([]ReviewThread, error) {
	if len(f.threads) == 0 {
		return nil, nil
	}
	result := f.threads[0]
	if len(f.threads) > 1 {
		f.threads = f.threads[1:]
	}
	return result, nil
}
func (f *fakeForge) ResolveThread(_ context.Context, id string) error {
	f.resolved = append(f.resolved, id)
	return nil
}
func (f *fakeForge) WaitExternalReview(_ context.Context, _, name string, _ time.Duration) error {
	f.waited = append(f.waited, name)
	return nil
}

func (f *fakeForge) CreatePR(context.Context, string, string, string, string) (int, error) {
	f.prs++
	return 100 + f.prs, nil
}
func (f *fakeForge) SetStatus(_ context.Context, _, state, _ string) error {
	f.statuses = append(f.statuses, state)
	return nil
}
func (f *fakeForge) WaitCheck(context.Context, string, string) (bool, error) {
	if len(f.checks) == 0 {
		return true, nil
	}
	result := f.checks[0]
	f.checks = f.checks[1:]
	return result, nil
}
func (f *fakeForge) FailedLog(context.Context, string) (string, error) { return "log", nil }
func (f *fakeForge) ArmMerge(_ context.Context, _ int, sha string) error {
	f.armed = append(f.armed, sha)
	return nil
}
func (f *fakeForge) WaitMerged(_ context.Context, _ int, sha string) (string, error) {
	if sha == "rev" {
		return "revmerge", nil
	}
	return f.mergeResult, nil
}
func (f *fakeForge) MarkNeedsHuman(_ context.Context, _ int, reason string) error {
	f.needsHuman = append(f.needsHuman, reason)
	return nil
}
func (f *fakeForge) Comment(context.Context, int, string) error {
	f.comments++
	return nil
}

type fakeScanner struct{ leak bool }

func (f fakeScanner) Scan(context.Context, []byte) (bool, error) { return f.leak, nil }

const passVerdict = `{"sha":"abc","approved":true,"security_findings":[],"findings":[],"criteria":[{"criterion":"c","met":true}],"reason":"ok"}`
const repairVerdict = `{"sha":"abc","approved":false,"security_findings":[],"findings":[{"summary":"直す"}],"criteria":[{"criterion":"c","met":false}],"reason":"r"}`
const securityVerdict = `{"sha":"abc","approved":false,"security_findings":[{"summary":"危険"}],"findings":[],"criteria":[{"criterion":"c","met":false}],"reason":"r"}`

func newOrchestrator(t *testing.T, agents *fakeAgents, tree *fakeTree, forge *fakeForge, scanner fakeScanner) (*Orchestrator, *Record) {
	t.Helper()
	tree.reviewDir = t.TempDir()
	store := Store{Root: t.TempDir()}
	orchestrator := &Orchestrator{
		Store: store, Agents: agents, Tree: tree, Forge: forge, Scanner: scanner,
		Config: func(string) (RepoConfig, error) {
			return RepoConfig{Policy: Policy{StopPaths: []string{".github/workflows/**"}}, Verify: []string{"true"}, RequiredCheck: "workflow-result", ExternalReviewCheck: "CodeRabbit", ExternalReviewAuthors: []string{"coderabbitai"}}, nil
		},
		RoundLimit: time.Minute, MergeLimit: time.Minute,
	}
	return orchestrator, &Record{RunID: "r1", Repo: "o/n", Base: "main", Title: "t", Criteria: "c", Model: "m", Effort: "medium", ReviewerModel: "rm", Status: StatusImplementing}
}

func TestRunHappyPathMergesFixedSHA(t *testing.T) {
	agents := &fakeAgents{reviews: [][]byte{[]byte(passVerdict)}}
	forge := &fakeForge{mergeResult: "merge"}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, true); err != nil {
		t.Fatal(err)
	}
	if len(forge.armed) != 1 || forge.armed[0] != "abc" || forge.statuses[0] != "success" {
		t.Fatalf("armed=%v statuses=%v", forge.armed, forge.statuses)
	}
	if rec.Status != StatusCompleted || rec.MergeSHA != "merge" || !rec.FirstRoundPassed {
		t.Fatalf("rec=%+v", rec)
	}
}

func TestRunRequiresHumanApproval(t *testing.T) {
	orchestrator, rec := newOrchestrator(t, &fakeAgents{}, &fakeTree{}, &fakeForge{}, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, false); err == nil {
		t.Fatal("run without approval was accepted")
	}
}

func TestRunRefusesWhileRepoIsStopped(t *testing.T) {
	orchestrator, rec := newOrchestrator(t, &fakeAgents{}, &fakeTree{}, &fakeForge{}, fakeScanner{})
	if err := orchestrator.Store.SetStop("o/n", "main の CI が失敗"); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Run(context.Background(), rec, nil, true); err == nil {
		t.Fatal("stopped repository was accepted")
	}
}

func TestLeakStopsBeforeAnyPush(t *testing.T) {
	forge := &fakeForge{}
	tree := &fakeTree{}
	orchestrator, rec := newOrchestrator(t, &fakeAgents{}, tree, forge, fakeScanner{leak: true})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrStopped) || forge.prs != 0 || tree.commits != 0 {
		t.Fatalf("err=%v prs=%d commits=%d", err, forge.prs, tree.commits)
	}
	if rec.Status != StatusStopped {
		t.Fatalf("status=%s", rec.Status)
	}
}

func TestSensitivePathOpensPRButNeverMerges(t *testing.T) {
	forge := &fakeForge{}
	tree := &fakeTree{changes: []Change{{Status: 'M', Mode: "100644", Path: ".github/workflows/ci.yml"}}}
	orchestrator, rec := newOrchestrator(t, &fakeAgents{}, tree, forge, fakeScanner{})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrStopped) || forge.prs != 1 || len(forge.armed) != 0 || len(forge.needsHuman) != 1 {
		t.Fatalf("err=%v forge=%+v", err, forge)
	}
}

func TestSecurityVerdictStops(t *testing.T) {
	forge := &fakeForge{}
	orchestrator, rec := newOrchestrator(t, &fakeAgents{reviews: [][]byte{[]byte(securityVerdict)}}, &fakeTree{}, forge, fakeScanner{})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrStopped) || len(forge.armed) != 0 || forge.statuses[0] != "failure" {
		t.Fatalf("err=%v forge=%+v", err, forge)
	}
}

func TestWrongSHAStops(t *testing.T) {
	verdict := `{"sha":"zzz","approved":true,"security_findings":[],"findings":[],"criteria":[{"criterion":"c","met":true}],"reason":"ok"}`
	forge := &fakeForge{}
	orchestrator, rec := newOrchestrator(t, &fakeAgents{reviews: [][]byte{[]byte(verdict)}}, &fakeTree{}, forge, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, true); !errors.Is(err, ErrStopped) || len(forge.armed) != 0 {
		t.Fatalf("err=%v armed=%v", err, forge.armed)
	}
}

func TestRepairLimitStopsAfterTwoRounds(t *testing.T) {
	agents := &fakeAgents{reviews: [][]byte{[]byte(repairVerdict)}}
	forge := &fakeForge{}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrStopped) || rec.Repairs != 2 || len(agents.sent) != 2 || len(forge.armed) != 0 {
		t.Fatalf("err=%v repairs=%d sent=%d", err, rec.Repairs, len(agents.sent))
	}
}

func TestCIFailureIsRepaired(t *testing.T) {
	agents := &fakeAgents{reviews: [][]byte{[]byte(passVerdict)}}
	forge := &fakeForge{checks: []bool{false, true, true}, mergeResult: "merge"}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, true); err != nil {
		t.Fatal(err)
	}
	if rec.Repairs != 1 || rec.FirstRoundPassed || rec.Status != StatusCompleted {
		t.Fatalf("rec=%+v", rec)
	}
}

func TestQuotaWaitsWithoutSwitching(t *testing.T) {
	agents := &fakeAgents{implOutcomes: []ImplOutcome{ImplQuota}}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, &fakeForge{}, fakeScanner{})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrQuota) || rec.Status != StatusWaitingQuota {
		t.Fatalf("err=%v status=%s", err, rec.Status)
	}
	stored, loadErr := orchestrator.Store.Load("r1")
	if loadErr != nil || stored.Status != StatusWaitingQuota {
		t.Fatalf("stored=%+v err=%v", stored, loadErr)
	}
}

func TestReviewerQuotaAlsoWaits(t *testing.T) {
	agents := &fakeAgents{reviewOut: ReviewQuota}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, &fakeForge{}, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, true); !errors.Is(err, ErrQuota) {
		t.Fatalf("err=%v", err)
	}
}

func TestResumeContinuesRunningImplementer(t *testing.T) {
	agents := &fakeAgents{reviews: [][]byte{[]byte(passVerdict)}}
	forge := &fakeForge{mergeResult: "merge"}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	rec.Status, rec.BaseSHA, rec.AcceptanceSHA = StatusWaitingQuota, "base", "base"
	if err := orchestrator.Resume(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if agents.started != 0 || len(agents.sent) != 1 || rec.Status != StatusCompleted {
		t.Fatalf("started=%d sent=%v status=%s", agents.started, agents.sent, rec.Status)
	}
}

func TestPostMergeFailureRevertsAndStopsRepo(t *testing.T) {
	agents := &fakeAgents{reviews: [][]byte{[]byte(passVerdict)}}
	tree := &fakeTree{}
	forge := &fakeForge{checks: []bool{true, false}, mergeResult: "merge"}
	orchestrator, rec := newOrchestrator(t, agents, tree, forge, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, true); err != nil {
		t.Fatal(err)
	}
	reason, _ := orchestrator.Store.Stopped("o/n")
	if !tree.reverted || rec.Status != StatusReverted || reason == "" || forge.armed[1] != "rev" {
		t.Fatalf("reverted=%v rec=%+v reason=%q armed=%v", tree.reverted, rec, reason, forge.armed)
	}
}

func TestReconcileChecksUnverifiedMerges(t *testing.T) {
	forge := &fakeForge{}
	orchestrator, rec := newOrchestrator(t, &fakeAgents{}, &fakeTree{}, forge, fakeScanner{})
	rec.Status, rec.MergeSHA = StatusMergedUnchecked, "merge"
	if err := orchestrator.Store.Save(rec); err != nil {
		t.Fatal(err)
	}
	if err := orchestrator.Reconcile(context.Background(), "o/n"); err != nil {
		t.Fatal(err)
	}
	stored, err := orchestrator.Store.Load("r1")
	if err != nil || stored.Status != StatusCompleted {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func rabbit(id, body string) ReviewThread {
	return ReviewThread{ID: id, Author: "coderabbitai", Bot: true, Path: "a.go", Line: 1, Body: body}
}

func TestExternalFindingIsRepairedThenResolved(t *testing.T) {
	agents := &fakeAgents{reviews: [][]byte{[]byte(passVerdict)}}
	forge := &fakeForge{mergeResult: "merge", threads: [][]ReviewThread{{rabbit("T1", "変数名を直す")}, {rabbit("T1", "変数名を直す")}}}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, true); err != nil {
		t.Fatal(err)
	}
	if len(forge.waited) != 2 || forge.waited[0] != "CodeRabbit" {
		t.Fatalf("waited=%v", forge.waited)
	}
	// 指摘があるラウンドでは、別モデルのレビューに進まない。
	if agents.reviewed != 1 || rec.Repairs != 1 || rec.FirstRoundPassed {
		t.Fatalf("reviewed=%d rec=%+v", agents.reviewed, rec)
	}
	if len(agents.sent) != 1 || !strings.Contains(agents.sent[0], "変数名を直す") {
		t.Fatalf("sent=%v", agents.sent)
	}
	// レビュー担当にも過去の指摘を渡し、解消を確認させる。
	if !strings.Contains(agents.prompts[0], "変数名を直す") {
		t.Fatalf("prompt=%s", agents.prompts[0])
	}
	if len(forge.resolved) != 1 || forge.resolved[0] != "T1" || rec.Status != StatusCompleted {
		t.Fatalf("resolved=%v status=%s", forge.resolved, rec.Status)
	}
}

func TestResolvesOnlyThreadsStillOpen(t *testing.T) {
	agents := &fakeAgents{reviews: [][]byte{[]byte(passVerdict)}}
	// CodeRabbit が修正を検知して自分で解決した場合は、ハーネスは何もしない。
	forge := &fakeForge{mergeResult: "merge", threads: [][]ReviewThread{{rabbit("T1", "直す")}, {}}}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	if err := orchestrator.Run(context.Background(), rec, nil, true); err != nil {
		t.Fatal(err)
	}
	if len(forge.resolved) != 0 {
		t.Fatalf("resolved=%v", forge.resolved)
	}
}

func TestExternalFindingsShareRepairLimit(t *testing.T) {
	agents := &fakeAgents{}
	forge := &fakeForge{threads: [][]ReviewThread{{rabbit("A", "1")}, {rabbit("B", "2")}, {rabbit("C", "3")}}}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrStopped) || rec.Repairs != 2 || len(forge.armed) != 0 || agents.reviewed != 0 {
		t.Fatalf("err=%v rec=%+v", err, rec)
	}
}

func TestHumanCommentStopsBeforeMerge(t *testing.T) {
	agents := &fakeAgents{}
	human := ReviewThread{ID: "H", Author: "tqer39", Path: "a.go", Line: 1, Body: "待って"}
	forge := &fakeForge{threads: [][]ReviewThread{{human}}}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrStopped) || len(forge.armed) != 0 || len(agents.sent) != 0 || len(forge.needsHuman) != 1 {
		t.Fatalf("err=%v sent=%v forge=%+v", err, agents.sent, forge)
	}
}

func TestExternalSecurityFindingStops(t *testing.T) {
	agents := &fakeAgents{}
	forge := &fakeForge{threads: [][]ReviewThread{{rabbit("S", "**Security** の問題")}}}
	orchestrator, rec := newOrchestrator(t, agents, &fakeTree{}, forge, fakeScanner{})
	err := orchestrator.Run(context.Background(), rec, nil, true)
	if !errors.Is(err, ErrStopped) || len(agents.sent) != 0 || len(forge.armed) != 0 {
		t.Fatalf("err=%v sent=%v", err, agents.sent)
	}
}
