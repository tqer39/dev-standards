package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func publish(ctx context.Context, x Executor, dir string, e Entry, base string, c Config) (string, error) {
	candidate, err := git(ctx, x, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	candidate = strings.TrimSpace(candidate)
	if !strings.HasPrefix(e.Remote, "https://github.com/") {
		// Normal fast-forward push rejects concurrent remote updates.
		if _, err = git(ctx, x, dir, "push", e.Remote, "HEAD:refs/heads/"+e.Branch); err != nil {
			return "", err
		}
		return candidate, nil
	}
	repo := strings.TrimSuffix(strings.TrimPrefix(e.Remote, "https://github.com/"), ".git")
	tree, err := git(ctx, x, dir, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	branch := "dev-standards/auto-" + hash(base + tree)[:16]
	exists, err := git(ctx, x, dir, "ls-remote", e.Remote, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(exists) == "" {
		if _, err = git(ctx, x, dir, "push", e.Remote, "HEAD:refs/heads/"+branch); err != nil {
			return "", err
		}
	} else {
		candidate = strings.Fields(exists)[0]
		if _, err = git(ctx, x, dir, "fetch", e.Remote, candidate); err != nil {
			return "", err
		}
		existingTree, err := git(ctx, x, dir, "rev-parse", "FETCH_HEAD^{tree}")
		if err != nil || existingTree != tree {
			return "", fmt.Errorf("existing automation branch was edited; refusing to overwrite")
		}
	}
	prs, err := x.Run(ctx, dir, "gh", []string{"pr", "list", "--repo", repo, "--head", branch, "--state", "open", "--json", "number"}, "")
	if err != nil {
		return "", err
	}
	var open []struct {
		Number int `json:"number"`
	}
	if err = json.Unmarshal([]byte(prs), &open); err != nil {
		return "", err
	}
	number := 0
	if len(open) > 0 {
		number = open[0].Number
	} else {
		output, err := x.Run(ctx, dir, "gh", []string{"pr", "create", "--repo", repo, "--base", e.Branch, "--head", branch, "--title", "chore: 共通ルールの自律保守", "--body", "固定された変更範囲と予算、独立した読み取り専用レビュー、ローカル検証を通過した変更です。CI の成功と対象コミットを確認した後、自律保守がマージします。"}, "")
		if err != nil {
			return "", err
		}
		parts := strings.Split(strings.TrimSpace(output), "/")
		if _, err = fmt.Sscanf(parts[len(parts)-1], "%d", &number); err != nil {
			return "", err
		}
	}
	workflow := ".github/workflows/dev-standards.yml"
	if e.Role == "source" {
		workflow = ".github/workflows/ci.yml"
	}
	if err = waitCI(ctx, x, dir, repo, candidate, workflow); err != nil {
		return "", err
	}
	if _, err = x.Run(ctx, dir, "gh", []string{"pr", "merge", fmt.Sprint(number), "--repo", repo, "--squash", "--match-head-commit", candidate}, ""); err != nil {
		return "", err
	}
	merged, err := x.Run(ctx, dir, "gh", []string{"pr", "view", fmt.Sprint(number), "--repo", repo, "--json", "mergeCommit", "--jq", ".mergeCommit.oid"}, "")
	if err != nil {
		return "", err
	}
	merged = strings.TrimSpace(merged)
	if len(merged) != 40 {
		return "", fmt.Errorf("merge was not confirmed")
	}
	if err = waitCI(ctx, x, dir, repo, merged, workflow); err != nil {
		return "", err
	}
	return merged, nil
}
func waitCI(ctx context.Context, x Executor, dir, repo, sha, path string) error {
	for {
		out, err := x.Run(ctx, dir, "gh", []string{"api", "repos/" + repo + "/actions/runs?head_sha=" + sha + "&per_page=100"}, "")
		if err != nil {
			return err
		}
		var data struct {
			Runs []struct {
				SHA        string `json:"head_sha"`
				Path       string `json:"path"`
				Status     string `json:"status"`
				Conclusion string `json:"conclusion"`
			} `json:"workflow_runs"`
		}
		if err = json.Unmarshal([]byte(out), &data); err != nil {
			return err
		}
		found := false
		for _, r := range data.Runs {
			if r.SHA != sha || r.Path != path {
				continue
			}
			found = true
			if r.Status != "completed" {
				break
			}
			if r.Conclusion != "success" {
				return fmt.Errorf("CI did not succeed: %s", r.Conclusion)
			}
			return nil
		}
		_ = found
		select {
		case <-ctx.Done():
			return fmt.Errorf("CI confirmation: %w", ctx.Err())
		case <-time.After(10 * time.Second):
		}
	}
}
func refresh(ctx context.Context, x Executor, dir string, e Entry, sha string) error {
	if _, err := git(ctx, x, dir, "fetch", e.Remote, sha); err != nil {
		return err
	}
	_, err := git(ctx, x, dir, "checkout", "--detach", sha)
	return err
}

func tagRelease(ctx context.Context, x Executor, dir string, e Entry) error {
	b, err := os.ReadFile(filepath.Join(dir, "assets.go"))
	if err != nil {
		return err
	}
	match := regexp.MustCompile(`const Version = "([0-9]+[.][0-9]+[.][0-9]+)"`).FindSubmatch(b)
	if len(match) != 2 {
		return fmt.Errorf("release version missing")
	}
	tag := "v" + string(match[1])
	existing, err := git(ctx, x, dir, "ls-remote", e.Remote, "refs/tags/"+tag)
	if err != nil {
		return err
	}
	head, err := git(ctx, x, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(existing) != "" {
		if strings.Fields(existing)[0] != strings.TrimSpace(head) {
			return fmt.Errorf("release tag already points elsewhere")
		}
		return nil
	}
	if _, err = git(ctx, x, dir, "tag", tag, strings.TrimSpace(head)); err != nil {
		return err
	}
	_, err = git(ctx, x, dir, "push", e.Remote, "refs/tags/"+tag)
	return err
}
