//go:build !windows

package harness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Result は実装担当の完了の申告である。信用せず、差分と検査で確認する。
type Result struct {
	RunID   string `json:"run_id"`
	Round   int    `json:"round"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

func ReadResult(workdir, runID string, round int) (Result, bool, error) {
	data, err := os.ReadFile(filepath.Join(workdir, ".agent-run", "result.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, err
	}
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		// 書き込み途中の可能性があるので、次の確認まで待つ。
		return Result{}, false, nil
	}
	if result.RunID != runID || result.Round != round {
		return Result{}, false, nil
	}
	return result, true, nil
}

type Issue struct {
	Summary string `json:"summary"`
	Path    string `json:"path,omitempty"`
}

type Criterion struct {
	Criterion string `json:"criterion"`
	Met       bool   `json:"met"`
}

type Verdict struct {
	SHA              string      `json:"sha"`
	Approved         bool        `json:"approved"`
	SecurityFindings []Issue     `json:"security_findings"`
	Findings         []Issue     `json:"findings"`
	Criteria         []Criterion `json:"criteria"`
	Reason           string      `json:"reason"`
}

type Decision int

const (
	DecisionPass Decision = iota
	DecisionRepair
	DecisionStop
)

// Decide はレビューの出力を決定的に解釈する。セキュリティの指摘と判定の不備は止める。
func (v Verdict) Decide() Decision {
	if len(v.SecurityFindings) > 0 || len(v.Criteria) == 0 {
		return DecisionStop
	}
	if !v.Approved || len(v.Findings) > 0 {
		return DecisionRepair
	}
	for _, criterion := range v.Criteria {
		if !criterion.Met {
			return DecisionRepair
		}
	}
	return DecisionPass
}

func ParseVerdict(data []byte, wantSHA string) (Verdict, error) {
	var envelope struct {
		Structured json.RawMessage `json:"structured_output"`
	}
	body := data
	if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.Structured) > 0 {
		body = envelope.Structured
	}
	var verdict Verdict
	if err := json.Unmarshal(body, &verdict); err != nil {
		return Verdict{}, fmt.Errorf("レビューの判定を解釈できません: %w", err)
	}
	if verdict.SHA != wantSHA {
		return Verdict{}, fmt.Errorf("レビューの SHA が一致しません: %q", verdict.SHA)
	}
	return verdict, nil
}

// VerdictSchema は claude --json-schema に渡すレビュー出力の形式である。
const VerdictSchema = `{"type":"object","additionalProperties":false,"required":["sha","approved","security_findings","findings","criteria","reason"],"properties":{"sha":{"type":"string"},"approved":{"type":"boolean"},"security_findings":{"type":"array","items":{"$ref":"#/$defs/issue"}},"findings":{"type":"array","items":{"$ref":"#/$defs/issue"}},"criteria":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["criterion","met"],"properties":{"criterion":{"type":"string"},"met":{"type":"boolean"}}}},"reason":{"type":"string"}},"$defs":{"issue":{"type":"object","additionalProperties":false,"required":["summary"],"properties":{"summary":{"type":"string"},"path":{"type":"string"}}}}}`
