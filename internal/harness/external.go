//go:build !windows

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	maxCommentRunes = 1500
	// ExternalReviewGrace は外部レビューの check が現れるまでの猶予である。導入していないリポジトリを止めない。
	ExternalReviewGrace = 3 * time.Minute
)

var (
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	htmlDetails = regexp.MustCompile(`(?s)<details>.*?</details>`)
	htmlTag     = regexp.MustCompile(`<[^>]+>`)
	blankLines  = regexp.MustCompile(`\n{2,}`)
	// 誤検知は人間の確認に倒れるだけなので、広めに止める。
	securityText = regexp.MustCompile(`(?i)security|critical|脆弱性|セキュリティ`)
)

// ReviewThread は未解決のレビュースレッドである。Body は信頼できないデータとして扱う。
type ReviewThread struct {
	ID     string
	Author string
	Bot    bool
	Path   string
	Line   int
	Body   string
}

// FedThread は実装担当に修正を依頼した外部レビューの指摘である。
type FedThread struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// cleanComment は表示されない部分を除く。隠れた文章を実装担当に渡さないためである。
func cleanComment(body string) string {
	body = htmlComment.ReplaceAllString(body, "")
	body = htmlDetails.ReplaceAllString(body, "")
	body = htmlTag.ReplaceAllString(body, "")
	body = strings.TrimSpace(blankLines.ReplaceAllString(body, "\n"))
	return truncate(body, maxCommentRunes)
}

const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){reviewThreads(first:100){pageInfo{hasNextPage}nodes{id isResolved path line comments(first:1){nodes{body author{__typename login}}}}}}}}`

// UnresolvedThreads は未解決のレビュースレッドを返す。
func (g *GitHub) UnresolvedThreads(ctx context.Context, pr int) ([]ReviewThread, error) {
	owner, name, _ := strings.Cut(g.Repo, "/")
	out, err := g.gh(ctx, "api", "graphql", "-f", "query="+threadsQuery, "-f", "owner="+owner, "-f", "name="+name, "-F", "number="+strconv.Itoa(pr))
	if err != nil {
		return nil, err
	}
	var data struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID         string `json:"id"`
							IsResolved bool   `json:"isResolved"`
							Path       string `json:"path"`
							Line       int    `json:"line"`
							Comments   struct {
								Nodes []struct {
									Body   string `json:"body"`
									Author struct {
										Type  string `json:"__typename"`
										Login string `json:"login"`
									} `json:"author"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		return nil, fmt.Errorf("レビュースレッドを解釈できません: %w", err)
	}
	threads := data.Data.Repository.PullRequest.ReviewThreads
	if threads.PageInfo.HasNextPage {
		return nil, fmt.Errorf("PR #%d のレビュースレッドが多すぎます", pr)
	}
	var result []ReviewThread
	for _, node := range threads.Nodes {
		if node.IsResolved {
			continue
		}
		thread := ReviewThread{ID: node.ID, Path: node.Path, Line: node.Line}
		if len(node.Comments.Nodes) > 0 {
			comment := node.Comments.Nodes[0]
			thread.Author = comment.Author.Login
			thread.Bot = comment.Author.Type == "Bot"
			thread.Body = cleanComment(comment.Body)
		}
		result = append(result, thread)
	}
	return result, nil
}

// ResolveThread はレビュースレッドを解決済みにする。
func (g *GitHub) ResolveThread(ctx context.Context, id string) error {
	_, err := g.gh(ctx, "api", "graphql", "-f", "query=mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{id}}}", "-f", "id="+id)
	return err
}

// WaitExternalReview は外部レビューの完了を待つ。猶予内に現れなければ未導入として進める。
func (g *GitHub) WaitExternalReview(ctx context.Context, sha, name string, grace time.Duration) error {
	deadline := time.Now().Add(grace)
	for {
		out, err := g.gh(ctx, "api", "repos/"+g.Repo+"/commits/"+sha+"/check-runs?check_name="+name+"&filter=latest")
		if err != nil {
			return err
		}
		var data struct {
			Runs []struct {
				Status string `json:"status"`
			} `json:"check_runs"`
		}
		if err := json.Unmarshal([]byte(out), &data); err != nil {
			return err
		}
		if len(data.Runs) > 0 {
			// 結果は問わない。指摘の有無はスレッドで判断する。
			if data.Runs[0].Status == "completed" {
				return nil
			}
		} else if !time.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s の完了を確認できません: %w", name, ctx.Err())
		case <-time.After(g.Poll):
		}
	}
}

type threadClass struct {
	New        []ReviewThread
	StopReason string
}

// classifyThreads はスレッドを修正依頼か人間の判断に分ける。
// 人間の発言と Bot を装った利用者の発言は、LLM に任せず人間に返す。
func classifyThreads(threads []ReviewThread, config RepoConfig, fed []FedThread) threadClass {
	var result threadClass
	for _, thread := range threads {
		if slices.ContainsFunc(fed, func(item FedThread) bool { return item.ID == thread.ID }) {
			continue
		}
		if !thread.Bot || !slices.Contains(config.ExternalReviewAuthors, thread.Author) {
			result.StopReason = fmt.Sprintf("%s の未解決のコメントがあります: %s:%d", thread.Author, thread.Path, thread.Line)
			return result
		}
		if securityText.MatchString(thread.Body) {
			result.StopReason = fmt.Sprintf("%s がセキュリティまたは重大な問題を指摘しました: %s:%d", thread.Author, thread.Path, thread.Line)
			return result
		}
		result.New = append(result.New, thread)
	}
	return result
}

func formatThread(thread ReviewThread) string {
	return fmt.Sprintf("%s:%d (%s)\n%s", thread.Path, thread.Line, thread.Author, thread.Body)
}
