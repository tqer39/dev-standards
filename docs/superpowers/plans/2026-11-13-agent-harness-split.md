# agent-harness の分離 実装計画

<!-- 番号付きの手順はコード例をはさんで続くので除外する。 -->
<!-- markdownlint-disable MD029 -->

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `dev-standards` の `agent-harness` を、動作を変えずに新しいリポジトリ `tqer39/agent-harness` へ移し、`dev-standards` から削除する。

**Architecture:** コードはパスを保ったまま複製し、Go の module パスだけを書き換える。Git の履歴は移さず、移行元のコミットを記録する。新しいリポジトリには `dev-standards onboard` で共通ルールを適用する。設計の変更（`codex exec` への置き換えなど）はこの計画に含めず、分離後に新しいリポジトリで別の計画として行う。

**Tech Stack:** Go 1.27.3（外部依存なし）、mise、GitHub CLI、Terraform（`terraform-github`）、`dev-standards` CLI

**仕様:** [設計仕様書](../specs/2026-11-13-issue-driven-harness-design.md) の「リポジトリの分離」

---

## 前提

- 作業する Mac は Mac Studio とする。
- ワークスペースは `~/workspace/tqer39`、`dev-standards` の既定の checkout は `~/workspace/tqer39/dev-standards` とする。
- `[人間]` の手順は、公開リポジトリの作成や Terraform の適用など、取り消しにくい操作である。エージェントは実行せず、人間に依頼する。
- この計画の作業中は、ハーネスで新しいタスクを実行しない。
- この計画と 2026-11-13 の仕様書を含む `dev-standards` の PR が `main` にマージ済みである。Task 2 は `origin/main` からファイルを取り出す。

## 移すファイル

| 移行元（`dev-standards`） | 移行先（`agent-harness`） |
| --- | --- |
| `cmd/agent-harness/` | 同じパス |
| `internal/harness/` | 同じパス |
| `harness/`（埋め込む権限プロファイル） | 同じパス |
| `scripts/agent-harness-probe.sh` | 同じパス |
| `docs/agent-harness.md` | 同じパス |
| `docs/superpowers/specs/2026-11-07-claude-codex-harness-design.md` | 同じパス |
| `docs/superpowers/specs/2026-11-07-claude-codex-harness.infra` | 同じパス |
| `docs/superpowers/specs/2026-11-13-issue-driven-harness-design.md` | 同じパス |
| `docs/superpowers/plans/2026-11-07-claude-codex-harness.md` | 同じパス |

この計画（`docs/superpowers/plans/2026-11-13-agent-harness-split.md`）は、`dev-standards` の変更の記録として `dev-standards` に残す。

## Task 1: [人間] GitHub にリポジトリを作る

`terraform-github` の CI の App トークンは個人アカウントにリポジトリを作れないので、先に作って Terraform に取り込む（`agent-telemetry` と同じ手順）。

- [ ] **Step 1: 空のリポジトリを作る**

```bash
gh repo create tqer39/agent-harness --public \
  --description "承認済みの GitHub Issue から、Codex の実装、Claude のレビュー、マージまでを進めるエージェントハーネス"
```

Expected: `https://github.com/tqer39/agent-harness` が表示される。

- [ ] **Step 2: 作成を確認する**

```bash
gh repo view tqer39/agent-harness --json nameWithOwner,visibility,isEmpty
```

Expected: `{"isEmpty":true,"nameWithOwner":"tqer39/agent-harness","visibility":"PUBLIC"}`

## Task 2: コードを移して初回コミットを作る

**Files:**

- Create: `~/workspace/tqer39/agent-harness/`（上の表のファイル一式）
- Create: `~/workspace/tqer39/agent-harness/go.mod`
- Create: `~/workspace/tqer39/agent-harness/docs/decisions/0001-language.md`（`onboard` に渡す言語選定記録）
- Create: `~/workspace/tqer39/agent-harness/README.md`

- [ ] **Step 1: 移行元を最新にして SHA を記録する**

```bash
git -C ~/workspace/tqer39/dev-standards fetch origin
SRC_SHA=$(git -C ~/workspace/tqer39/dev-standards rev-parse origin/main)
echo "$SRC_SHA"
```

Expected: 40 文字の SHA。以降の手順で使う。
`origin/main` にハーネスの未マージの変更がないことを `gh pr list --repo tqer39/dev-standards --search "harness in:title"` で確認する。

- [ ] **Step 2: clone して、移行元の main からファイルを取り出す**

```bash
cd ~/workspace/tqer39
gh repo clone tqer39/agent-harness
cd agent-harness
git -C ~/workspace/tqer39/dev-standards archive "$SRC_SHA" \
  cmd/agent-harness internal/harness harness scripts/agent-harness-probe.sh \
  docs/agent-harness.md \
  docs/superpowers/specs/2026-11-07-claude-codex-harness-design.md \
  docs/superpowers/specs/2026-11-07-claude-codex-harness.infra \
  docs/superpowers/specs/2026-11-13-issue-driven-harness-design.md \
  docs/superpowers/plans/2026-11-07-claude-codex-harness.md \
  | tar -x
find . -type f -not -path './.git/*' | wc -l
```

Expected: `42`（上の表のファイル数。2026-11-13 時点）。

- [ ] **Step 3: go.mod を作り、import のパスを書き換える**

```bash
cat > go.mod <<'EOF'
module github.com/tqer39/agent-harness

go 1.27.3
EOF
grep -rl 'github.com/tqer39/dev-standards/' --include='*.go' . \
  | xargs sed -i '' 's#github.com/tqer39/dev-standards/#github.com/tqer39/agent-harness/#g'
grep -rn 'tqer39/dev-standards' --include='*.go' . || echo "no references"
```

Expected: `no references`

- [ ] **Step 4: ビルドとテストを通す**

`mise.toml` は Task 3 の `onboard` が作るので、ここでは書かずに版だけ指定して実行する。

```bash
mise exec go@1.27.3 -- go vet ./...
mise exec go@1.27.3 -- go test -timeout=20m ./...
mise exec go@1.27.3 -- go build -o bin/ ./cmd/agent-harness
rm -rf bin
```

Expected: すべて成功する。`dev-standards` と同じテストがそのまま通る。
失敗した場合は、移し漏れたファイルか、`dev-standards` 側のファイルを読むテストがないかを確認する。動作を変える修正はしない。

- [ ] **Step 5: 言語選定記録を作る**

`docs/decisions/0001-language.md` を次の内容で作る。

```markdown
# 開発言語の選定

## 要件

Mac で常駐し、Codex、Claude、GitHub、1Password の CLI を子プロセスとして制御する。
状態の記録、排他、停止と再開を型とテストで扱う。

## 候補

Go と Python を、子プロセスの制御、配布、移行元のコードの再利用から比較する。

## 選定理由

Go を採用する。移行元の dev-standards で Go により実装と検証を済ませており、
標準ライブラリだけで子プロセス、HTTP、JSON を扱える。
```

- [ ] **Step 6: README を作る**

`README.md` を次の内容で作る。

```markdown
# agent-harness

人間が承認した受入条件をもとに、Codex の実装、Claude のレビュー、マージまでを進めるハーネスです。
Mac で動かします。

運用手順は [agent-harness の運用](docs/agent-harness.md)、設計は [仕様書](docs/superpowers/specs/2026-11-13-issue-driven-harness-design.md) を参照してください。

## 移行元

2026-11-13 に [tqer39/dev-standards](https://github.com/tqer39/dev-standards) から分離しました。
それ以前の履歴は dev-standards の該当コミット以前を参照してください。
```

コミット前に、README の「該当コミット」を Step 1 の `$SRC_SHA` へのリンクに置き換える。

```bash
sed -i '' "s#該当コミット#[$SRC_SHA](https://github.com/tqer39/dev-standards/tree/$SRC_SHA)#" README.md
grep -c "$SRC_SHA" README.md
```

Expected: `1`

- [ ] **Step 7: 初回コミットを作って push する**

```bash
git add -A
git commit -F - <<EOF
🎉 feat: dev-standards からエージェントハーネスを移す

tqer39/dev-standards@$SRC_SHA の agent-harness を、動作を変えずに移す。
Go の module パスだけを github.com/tqer39/agent-harness に書き換える。
EOF
git push -u origin main
```

Expected: `main` に push される。

## Task 3: 共通ルールを適用する

`dev-standards onboard` で、mise のタスク、EditorConfig、`.gitignore`、hooks、CI、共通指示を配置し、自律保守の台帳に登録する。

**Files:**

- Create: `onboard` が生成する一式（`mise.toml`、`.editorconfig`、`.gitignore`、`.github/workflows/`、`AGENTS.md` など）

- [ ] **Step 1: 作業ブランチを作る**

```bash
cd ~/workspace/tqer39/agent-harness
git switch -c chore/onboard-dev-standards
```

- [ ] **Step 2: onboard を実行する**

```bash
~/workspace/tqer39/dev-standards/bin/dev-standards onboard \
  --repo ~/workspace/tqer39/agent-harness \
  --decision docs/decisions/0001-language.md \
  --fleet ~/workspace/tqer39/dev-standards/.local/maintenance/fleet.json
```

Expected: 衝突なく完了する。衝突や未対応の構成が出たら、表示された理由を記録して止まり、人間に判断を求める。

- [ ] **Step 3: mise のタスクで検証する**

```bash
mise trust
mise bootstrap
mise run check
mise run test:race
```

Expected: すべて成功する。
`mise run build` の対象が `./cmd/agent-harness` になっていなければ、`mise.toml` の build タスクを `go build -o bin/ ./cmd/agent-harness` にする。

- [ ] **Step 4: ハーネスの文書に lint をかける**

```bash
mise exec -- markdownlint-cli2 "docs/**/*.md" README.md
```

Expected: `0 issues`。移行元の `.markdownlint-cli2.jsonc` と同じく `MD013` を無効にする設定がなければ、`onboard` の生成物に従い、指摘された箇所を直す。

- [ ] **Step 5: コミットして PR を作る**

```bash
git add -A
git commit -m "🔧 chore: dev-standards の共通ルールを適用する"
git push -u origin chore/onboard-dev-standards
gh pr create --repo tqer39/agent-harness --title "chore: dev-standards の共通ルールを適用する" --body "dev-standards onboard で共通ルールを適用し、自律保守の台帳に登録した。"
```

Expected: PR の CI が成功する。マージは人間が判断する。

## Task 4: terraform-github で管理対象にする

**Files:**

- Create: `terraform-github/terraform/src/repositories/agent-harness/main.tf`
- Create: `terraform-github/terraform/src/repositories/agent-harness/terraform.tf`
- Create: `terraform-github/terraform/src/repositories/agent-harness/providers.tf`
- Create: `terraform-github/terraform/src/repositories/agent-harness/variables.tf`
- Create: `terraform-github/terraform/src/repositories/agent-harness/.terraform.lock.hcl`（`agent-telemetry` から複製）

- [ ] **Step 1: 作業用の worktree を作る**

`~/workspace/tqer39/terraform-github` の checkout は別の作業中のことがあるので、worktree を使う。

```bash
cd ~/workspace/tqer39/terraform-github
git fetch origin
git worktree add ../terraform-github-worktrees/add-agent-harness -b feat/add-agent-harness-repository origin/main
cd ../terraform-github-worktrees/add-agent-harness
mkdir -p terraform/src/repositories/agent-harness
cd terraform/src/repositories/agent-harness
```

- [ ] **Step 2: 定義を書く**

`main.tf`:

```hcl
module "this" {
  source              = "../../../modules/repository"
  github_token        = var.github_token
  repository          = "agent-harness"
  owner               = "tqer39"
  default_branch      = "main"
  enable_owner_bypass = true
  visibility          = "public"
  topics              = ["claude-code", "codex", "ai-agents", "orca"]
  description         = "承認済みの GitHub Issue から、Codex の実装、Claude のレビュー、マージまでを進めるエージェントハーネス"
}

# CI の App installation token は個人アカウントにリポジトリを作成できないため、先に作成したものを取り込む。
import {
  to = module.this.github_repository.this[0]
  id = "agent-harness"
}
```

`terraform.tf`:

```hcl
terraform {
  required_version = "1.16.5"
  required_providers {
    github = {
      source  = "integrations/github"
      version = "6.13.0"
    }
  }
  backend "s3" {
    bucket       = "terraform-tfstate-tqer39-072693953877-ap-northeast-1"
    key          = "terraform-github/repositories/agent-harness.tfstate"
    region       = "ap-northeast-1"
    encrypt      = true
    use_lockfile = true
  }
}
```

`providers.tf`:

```hcl
provider "github" {
  owner = "tqer39"
  token = var.github_token
}
```

`variables.tf`:

```hcl
variable "github_token" {
  type        = string
  description = "GitHub token"
  sensitive   = true
}
```

ロックファイルは同じ provider の版の `agent-telemetry` から複製する。

```bash
cp ../agent-telemetry/.terraform.lock.hcl .
```

- [ ] **Step 3: 整形と構文を確認する**

```bash
cd ~/workspace/tqer39/terraform-github-worktrees/add-agent-harness
mise exec -- terraform fmt -check -recursive terraform/src/repositories/agent-harness
mise exec -- terraform -chdir=terraform/src/repositories/agent-harness init -backend=false
mise exec -- terraform -chdir=terraform/src/repositories/agent-harness validate
```

Expected: `fmt` は出力なし、`validate` は `Success! The configuration is valid.`

- [ ] **Step 4: コミットして PR を作る**

```bash
git add terraform/src/repositories/agent-harness
git commit -m "✨ feat: agent-harness リポジトリを追加"
git push -u origin feat/add-agent-harness-repository
gh pr create --repo tqer39/terraform-github --title "feat: agent-harness リポジトリを追加" \
  --body "dev-standards から分離したエージェントハーネスのリポジトリを管理対象にする。リポジトリは作成済みなので import で取り込む。"
```

Expected: PR の plan に、`agent-harness` の import と設定の変更だけが出る。

- [ ] **Step 5: [人間] plan を確認してマージする**

`terraform-github` はマージで適用まで進むので、エージェントはマージしない。

## Task 5: 手元の起動先を切り替える

**Files:**

- Modify: `~/.claude/skills/agent-harness/SKILL.md`（ハーネスの実行ファイルのパス）

- [ ] **Step 1: 新しいリポジトリでビルドする**

```bash
cd ~/workspace/tqer39/agent-harness
git switch main && git pull
mise run build
ls -l bin/agent-harness
```

- [ ] **Step 2: skill の実行ファイルのパスを書き換える**

```bash
sed -i '' 's#/workspace/tqer39/dev-standards/bin/agent-harness#/workspace/tqer39/agent-harness/bin/agent-harness#g' ~/.claude/skills/agent-harness/SKILL.md
grep -n "bin/agent-harness" ~/.claude/skills/agent-harness/SKILL.md
```

Expected: すべての行が `~/workspace/tqer39/agent-harness/bin/agent-harness` を指す。

- [ ] **Step 3: 新しい実行ファイルで doctor を通す**

```bash
bin/agent-harness doctor --repo tqer39/agent-telemetry
```

Expected: 移行前と同じ結果になる。権限プロファイルは埋め込みの内容が同じなので FAIL にならない。

## Task 6: dev-standards からハーネスを削除する

**Files:**

- Delete: 「移すファイル」の表の移行元（この計画を除く）
- Modify: `mise.toml`（build タスク）
- Modify: `README.md`（ハーネスの案内）

- [ ] **Step 1: 作業ブランチを作る**

```bash
cd ~/workspace/tqer39/dev-standards
git fetch origin
git worktree add .claude/worktrees/remove-agent-harness -b chore/remove-agent-harness origin/main
cd .claude/worktrees/remove-agent-harness
```

- [ ] **Step 2: ファイルを削除する**

```bash
git rm -r -q cmd/agent-harness internal/harness harness scripts/agent-harness-probe.sh \
  docs/agent-harness.md \
  docs/superpowers/specs/2026-11-07-claude-codex-harness-design.md \
  docs/superpowers/specs/2026-11-07-claude-codex-harness.infra \
  docs/superpowers/specs/2026-11-13-issue-driven-harness-design.md \
  docs/superpowers/plans/2026-11-07-claude-codex-harness.md
```

- [ ] **Step 3: build タスクから外す**

`mise.toml` の build タスクを次のとおりにする。

```toml
[tasks.build]
run = "go build -o bin/ ./cmd/dev-standards"
```

- [ ] **Step 4: README の案内を差し替える**

`README.md` の次の行を置き換える。

変更前:

```markdown
Claude と Codex で実装からマージまでを進める手順は [agent-harness の運用](docs/agent-harness.md) を参照してください。
```

変更後:

```markdown
Claude と Codex で実装からマージまでを進めるハーネスは [tqer39/agent-harness](https://github.com/tqer39/agent-harness) に分離しました。
```

- [ ] **Step 5: 残った参照がないことを確認する**

```bash
git grep -n -iE "agent-harness|internal/harness|harnessassets" -- ':!docs/superpowers/plans/2026-11-13-agent-harness-split.md'
```

Expected: README の新しい案内の行だけが出る。

- [ ] **Step 6: 検証する**

```bash
go mod tidy
git diff --exit-code go.mod go.sum
mise run check
mise run test:race
git diff --check
```

Expected: `go.mod` と `go.sum` は変わらない（ハーネスは外部依存を持たない）。検証はすべて成功する。

- [ ] **Step 7: コミットして PR を作る**

```bash
git add -A
git commit -m "🔥 chore: agent-harness を tqer39/agent-harness に分離して削除する"
git push -u origin chore/remove-agent-harness
gh pr create --repo tqer39/dev-standards --title "chore: agent-harness を tqer39/agent-harness に分離して削除する" \
  --body "agent-harness を tqer39/agent-harness に移したので、dev-standards から削除する。動作の変更はない。"
```

Expected: PR の CI（`workflow-result`）が成功する。マージは人間が判断する。

- [ ] **Step 8: 調査の Issue に移動先を書く**

```bash
gh issue comment 35 --repo tqer39/dev-standards \
  --body "設計の仕様書は tqer39/agent-harness の docs/superpowers/specs/2026-11-13-issue-driven-harness-design.md に移しました。"
```

## 完了の条件

- `tqer39/agent-harness` の `main` で `mise run check` と `mise run test:race` が成功する。
- 新しい実行ファイルの `doctor` が、移行前と同じ結果になる。
- `tqer39/dev-standards` からハーネスのコードと文書がなくなり、CI が成功する。
- `terraform-github` が `agent-harness` を管理している。

## この計画の後

`tqer39/agent-harness` で、仕様書の「移行」の 1〜8 を別の計画として書く。最初の計画は「`Herdr` を `codex exec` による実行に置き換え、イベントの表示を追加する」とする。
