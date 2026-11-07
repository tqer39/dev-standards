//go:build !windows

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GitHub は短命トークンで PR を操作する。トークンは環境変数でだけ渡す。
type GitHub struct {
	Run    Runner
	Tokens TokenSource
	Repo   string
	Poll   time.Duration
}

func (g *GitHub) gh(ctx context.Context, args ...string) (string, error) {
	token, err := g.Tokens.Token(ctx)
	if err != nil {
		return "", err
	}
	return g.Run.Run(ctx, Cmd{Name: "gh", Args: args, Env: []string{"GH_TOKEN=" + token}, Secrets: []string{token}})
}

func (g *GitHub) CreatePR(ctx context.Context, head, base, title, body string) (int, error) {
	out, err := g.gh(ctx, "pr", "create", "--repo", g.Repo, "--head", head, "--base", base, "--title", title, "--body", body)
	if err != nil {
		return 0, err
	}
	parts := strings.Split(strings.TrimSpace(out), "/")
	number, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0, fmt.Errorf("PR 番号を解釈できません: %q", out)
	}
	return number, nil
}

func (g *GitHub) SetStatus(ctx context.Context, sha, state, description string) error {
	_, err := g.gh(ctx, "api", "-X", "POST", "repos/"+g.Repo+"/statuses/"+sha,
		"-f", "state="+state, "-f", "context=agent-review", "-f", "description="+truncate(description, 140))
	return err
}

// WaitCheck は指定したチェックの完了を待ち、成功かどうかを返す。
func (g *GitHub) WaitCheck(ctx context.Context, sha, name string) (bool, error) {
	for {
		out, err := g.gh(ctx, "api", "repos/"+g.Repo+"/commits/"+sha+"/check-runs?check_name="+name+"&filter=latest")
		if err != nil {
			return false, err
		}
		var data struct {
			Runs []struct {
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"check_runs"`
		}
		if err := json.Unmarshal([]byte(out), &data); err != nil {
			return false, err
		}
		if len(data.Runs) > 0 && data.Runs[0].Status == "completed" {
			return data.Runs[0].Conclusion == "success", nil
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("%s の完了を確認できません: %w", name, ctx.Err())
		case <-time.After(g.Poll):
		}
	}
}

// FailedLog は修正の材料になる CI のログを返す。内容は信頼できないデータとして扱う。
func (g *GitHub) FailedLog(ctx context.Context, sha string) (string, error) {
	out, err := g.gh(ctx, "run", "list", "--repo", g.Repo, "--commit", sha, "--status", "failure", "--json", "databaseId", "--jq", ".[0].databaseId")
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(out)
	if id == "" {
		return "", nil
	}
	log, err := g.gh(ctx, "run", "view", id, "--repo", g.Repo, "--log-failed")
	return tail(log, 8000), err
}

func (g *GitHub) ArmMerge(ctx context.Context, pr int, sha string) error {
	_, err := g.gh(ctx, "pr", "merge", strconv.Itoa(pr), "--repo", g.Repo, "--auto", "--squash", "--match-head-commit", sha)
	return err
}

// WaitMerged はマージを待つ。待機中に先頭が変わったら止める。
func (g *GitHub) WaitMerged(ctx context.Context, pr int, sha string) (string, error) {
	for {
		out, err := g.gh(ctx, "pr", "view", strconv.Itoa(pr), "--repo", g.Repo, "--json", "state,headRefOid,mergeCommit")
		if err != nil {
			return "", err
		}
		var state struct {
			State       string `json:"state"`
			Head        string `json:"headRefOid"`
			MergeCommit *struct {
				OID string `json:"oid"`
			} `json:"mergeCommit"`
		}
		if err := json.Unmarshal([]byte(out), &state); err != nil {
			return "", err
		}
		if state.Head != sha {
			return "", fmt.Errorf("PR #%d の先頭が変わりました", pr)
		}
		switch state.State {
		case "MERGED":
			if state.MergeCommit == nil {
				return "", fmt.Errorf("PR #%d のマージコミットを確認できません", pr)
			}
			return state.MergeCommit.OID, nil
		case "CLOSED":
			return "", fmt.Errorf("PR #%d が閉じられました", pr)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("PR #%d のマージを確認できません: %w", pr, ctx.Err())
		case <-time.After(g.Poll):
		}
	}
}

// PullRequestMerge は PR の状態とマージコミットを返す。待機後のマージの追跡に使う。
func (g *GitHub) PullRequestMerge(ctx context.Context, pr int) (string, string, error) {
	out, err := g.gh(ctx, "pr", "view", strconv.Itoa(pr), "--repo", g.Repo, "--json", "state,mergeCommit")
	if err != nil {
		return "", "", err
	}
	var state struct {
		State       string `json:"state"`
		MergeCommit *struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
	}
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		return "", "", err
	}
	sha := ""
	if state.MergeCommit != nil {
		sha = state.MergeCommit.OID
	}
	return state.State, sha, nil
}

func (g *GitHub) MarkNeedsHuman(ctx context.Context, pr int, reason string) error {
	if _, err := g.gh(ctx, "label", "create", "needs-human", "--repo", g.Repo, "--force", "--description", "ハーネスが人間の判断を待っています"); err != nil {
		return err
	}
	if _, err := g.gh(ctx, "pr", "edit", strconv.Itoa(pr), "--repo", g.Repo, "--add-label", "needs-human"); err != nil {
		return err
	}
	_, err := g.gh(ctx, "pr", "comment", strconv.Itoa(pr), "--repo", g.Repo, "--body", "agent-harness が停止しました。\n\n"+reason)
	return err
}

func (g *GitHub) Comment(ctx context.Context, pr int, body string) error {
	_, err := g.gh(ctx, "pr", "comment", strconv.Itoa(pr), "--repo", g.Repo, "--body", body)
	return err
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
