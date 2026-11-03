# dev-standards

個人開発リポジトリの共通ルールを、設定の正本と検査で管理する CLI です。
初回適用では変更計画を作り、確認した計画だけを反映します。
同じ版の再適用では、入力が同じなら差分が出ません。
管理ファイルの改行と gitignore.io の特殊なパターンは `.gitattributes` でも保持します。

## この版の実装範囲

Go はリポジトリのルートに module が１つある構成、Python/uv と Node.js/npm はルートまたは
指定ディレクトリにパッケージが１つある構成に対応します。
macOS、Linux、Windows で同じ CLI と mise タスクを使用します。

- EditorConfig の配置と固有設定の保持
- gitignore.io API から取得済みの入力を保存した、オフラインでの `.gitignore` 再生成
- mise によるツールの固定と bootstrap、lint、format、test、build の入口
- 要件、候補、選定理由を含む言語選定記録の構造検証
- 変更計画、ロック、適用済み状態、衝突検出、通常の書き込み失敗時の復元
- YAML と JSON Schema によるルール・宣言・プロファイル・例外の検証

既存の Codex ログインを使う自律保守、固定 hooks、通常開発の検証記録、
更新 PR の自動検証・マージ、版の更新と利用先への移行にも対応します。
初回設定と運用条件は [自律保守](docs/autonomous-maintenance.md) を参照してください。

AGENTS.md を rulesync で生成している利用先では、共通の入口を rulesync のルールとして配置できます。
dev-standards 自身による rulesync を使った他の LLM への配布、Renovate の共通設定、GitHub 管理設定の変更、
言語別の検証を自動構成する複数言語のモノレポ対応は後続の実装です。
Node.js/npm を主コンポーネントとし、既存タスクで検証する Go/Swift パッケージを明示的に併設できます。

## 開発環境の準備

共通ルールとして、各リポジトリは `mise bootstrap` で開発環境のセットアップを完了できるようにします。
前提条件の明記と初回・再実行の検証については [bootstrap のルール](guidance/core/bootstrap.md) を参照してください。

Go と開発ツールの版は `mise.toml` に固定しています。
内容を確認して trust を設定し、セットアップします。

```sh
mise trust
mise bootstrap
mise run check
```

`mise run check` は、Go の静的解析、Markdown lint、整形確認、テスト、
CLI のビルドを実行します。テストには実際の CLI と mise を使う受け入れテストも含まれます。
Windows では保守検証のビルドを繰り返すため、通常テストの上限は20分、CI ジョブの上限は30分です。

```sh
mise run test:race
mise run security:check
mise run catalog:check
```

ビルドした CLI は `bin/` にあります。
Windows の実行ファイル名は `dev-standards.exe`、他の OS では `dev-standards` です。
利用先では、この実行ファイルが PATH から見つかる状態にしてください。
`go install ./cmd/dev-standards` で Go のインストール先へ配置する方法も使えます。

## 自律保守を含む初回導入

初回以降の保守を自動化する場合は、[ホスト設定と onboard](docs/autonomous-maintenance.md) を使用します。
以下の `adopt` は、保守台帳への登録を伴わない基本の適用操作です。

## 初回適用

対象の Git リポジトリのルートで実行します。先に Go module を作成し、
言語選定を Markdown に記録してください。例えば `decision.md` を次のように作ります。

```markdown
# 開発言語の選定

## 要件

複数の OS で動作する CLI を、単体の実行ファイルとして配布する。

## 候補

Go と Python を、配布方法、開発速度、保守性から比較する。

## 選定理由

Go を採用する。利用先で別の言語ランタイムを準備する負担を減らせるため。
```

```sh
dev-standards inspect
dev-standards adopt --profile go --decision decision.md --output .standards/plans/adopt.json
```

計画の JSON には、変更前・変更後の内容と衝突を表示します。
`adopt` と `plan` は、差分がある場合は終了コード `1` になります。
これは適用の失敗ではありません。計画を確認したら次へ進みます。

```sh
dev-standards apply --plan .standards/plans/adopt.json
mise trust
mise bootstrap
dev-standards check
dev-standards verify
```

`adopt` は宣言した保存先に計画と親ディレクトリを作成します。
設定ファイル自体の反映は `apply` が行います。
言語選定記録は `docs/decisions/0001-language.md` に配置します。
既に同じ場所に有効な記録があれば、`--decision` は省略できます。

`check` は設定の整合性を調べる処理です。コードの lint やテストを実行したことにはしません。
`verify` は設定を確認したうえで、mise の lint、整形確認、test、build を実行します。
言語選定の妥当性のレビューと、新規環境での bootstrap は別の証拠が必要なため、
結果をそれぞれ `review_required`、`not_run` として明示します。

## Python/uv の既存リポジトリへの適用

`python-uv` は既存の `pyproject.toml` と `uv.lock` を使い、pytest、Ruff、uv build を実行します。
uv workspace と複数パッケージには対応しません。既存の Python コードを Go に移す必要はありません。
`mise.toml` の `tools.python` と `tools.uv` は、現在使っている版を完全な `x.y.z` で固定してください。
pytest と Ruff は利用先の開発依存に含めます。言語選定記録には Python を採用した理由を記します。

```sh
dev-standards adopt --profile python-uv --component-path session-indexer --decision decision.md --output .standards/plans/adopt.json
dev-standards apply --plan .standards/plans/adopt.json
mise trust
mise run bootstrap
dev-standards verify
```

ルートのパッケージでは `--component-path` を省略できます。
自律保守の初回導入も `onboard --profile python-uv --component-path session-indexer` で指定します。
予約済みの `lint`、`test` などは計画を作る前に明示的に移行します。
共通の検査を追加の固有検査で補う場合は `.standards/tasks.project.toml` に
`project:lint`、`project:test`、`project:build` などを定義してください。
対応する共通タスクの依存として実行し、共通検査は維持します。
既存の運用タスクと Git hooks は保持します。
Python と uv の固定値はロックへ保存し、検証でロックファイルを暗黙に更新しません。
macOS、Linux、Windows で同じタスクを生成しますが、固有タスクの OS 対応は別途必要です。

## Node.js/npm の既存リポジトリへの適用

`node-npm` は既存の `package.json` と `package-lock.json` を使います。
共通タスクは npm scripts の `lint`、`format`、`format:check`、`test`、`build` を呼び出すため、
Oxlint、Biome、ESLint などの実際のツールは利用先で選べます。
５つの scripts をすべて定義し、`mise.toml` の `tools.node` を完全な `x.y.z` で固定してください。
bootstrap は `npm ci --ignore-scripts` で依存のライフサイクルスクリプトを実行しません。
npm workspaces と、コンポーネント以外の `package.json` には対応しません。
言語選定記録には TypeScript または JavaScript を採用した理由を記します。

```sh
dev-standards adopt --profile node-npm --decision decision.md --output .standards/plans/adopt.json
```

AGENTS.md を rulesync で生成している場合は `--instructions rulesync` を追加します。
dev-standards は AGENTS.md を書き換えず、共通の入口を `.rulesync/rules/dev-standards.md` に配置します。
適用後に `rulesync generate` で AGENTS.md を再生成してください。
`check` は、AGENTS.md に共通の入口が含まれていない場合に失敗します。

## 複数言語の既存タスクを維持する導入

`mise-project` は既存の複数言語構成や pnpm を使うリポジトリ向けです。
ルートの mise 設定に `repo:bootstrap`、`repo:lint`、`repo:format`、`repo:format:check`、
`repo:test`、`repo:build` を明示し、各タスクで対象全体の既存検査を実行します。
共通の入口はこれらを呼び出し、検査の失敗をそのまま伝えます。
`adopt --profile mise-project` または `onboard --profile mise-project` で適用します。

このプロファイルは言語別の検査内容を推測しません。言語選定記録に対象、検証範囲、
OS 固有の検査を記録してください。Node.js、Python、Go、uv、pnpm の宣言は完全な版番号で固定し、
ツール一覧のハッシュをロックに保存します。既存のマニフェストと依存ロックも適用計画の入力に含めます。
言語選定記録の見出しは日本語と英語に対応します。
Node.js の gitignore.io 入力を基底とし、Python、Go、Swift などの既存の除外設定は固有の正本に保持します。
共通タスクは各 OS で呼び出せますが、固有のコマンドの移植性は利用先で検証する必要があります。

## 既存 CI の安全性検査

自律保守の CI は GitHub 提供の `ubuntu-24.04` を使い、checkout の資格情報を残しません。
Node.js/npm の利用先で依存導入前に既存の安全性検査が必要な場合は、
`.standards/repository.yml` の `ci.nodePreflight` に、例えば `scripts/check-ci-security.mjs` を指定します。
空でない既存の `.mjs` ファイルだけを指定でき、親ディレクトリやシェル式は拒否します。
この検査は mise と依存の導入より前に実行し、必須の lint・整形確認・テスト・ビルドも維持します。
正本を変更した後は `plan` と `apply` で CI を再生成してください。

## CI の重複起動を抑える

自動化の共通 CI では、既定で `push` と `pull_request` の両方を検証します。
PR のある作業ブランチでの二重起動を避ける場合、`.standards/repository.yml` の
`ci.pushBranches` に `main` など push を検証するブランチを明示します。

```yaml
ci:
  pushBranches: [main]
  prBudgetSeconds: 600
  minimumSamples: 20
  breachWindows: 2
```

指定時は、この一覧への push、すべての PR、手動実行を対象にします。
タグの push は対象外です。作業ブランチの検証には PR または手動実行が必要です。
既定ブランチ名を自動推測しないため、実際の既定ブランチと必要なリリースブランチを指定してください。
名前は英数字・アンダースコアで始まる各区間を `/` でつなぎ、区間内の `-` と `.` も使えます。
空の一覧、重複、glob、式、無効な末尾は拒否します。上限は32件、各255文字です。
未指定の利用先のイベント、必須ジョブ名、検証コマンドは変更しません。
変更後は `plan` と `apply` で再生成します。PR のブランチ・パス制限は追加しません。

## Go/Swift パッケージを併設する Node.js/npm リポジトリ

主コンポーネントには `node-npm` を使い、追加パッケージのパス、言語、既存の検証タスクを JSON で宣言します。
未宣言の追加パッケージは引き続き拒否します。追加パッケージは Node.js の検査で検証済みとは扱いません。

```json
[
  {"path": "harness", "profile": "go", "verifyTask": "project:verify:harness"},
  {"path": "apple", "profile": "swift", "verifyTask": "project:verify:apple"}
]
```

このファイルを `companions.json` とし、`.standards/tasks.project.toml` に対応するタスクを先に定義します。
各タスクには実行する `run` コマンドが必要です。`platforms`、`sources`、`outputs` などによる省略、
依存タスク経由の循環は許可しません。言語ごとの lint、整形、テスト、ビルドの具体的な範囲は利用先で定め、
既存の検証スクリプトへ接続してください。Go/Swift の設定自体を共通プロファイルが生成する機能ではありません。
Swift を併設する場合は検証対象 OS を macOS に限定し、生成 CI も macOS 上で実行します。
Swift/Xcode はホスト側で準備してください。Go の版は既存の mise 設定で固定します。

```sh
dev-standards adopt --profile node-npm --companions companions.json --instruction-language en --decision decision.md --output .standards/plans/adopt.json
```

`onboard` にも同じ追加オプションを使えます。`--instruction-language en` は共通の AGENTS.md 入口を英語にします。
言語選定記録は従来の日本語見出しに加え、`Requirements`、`Candidates`、`Selection rationale` も使用できます。
宣言は `.standards/repository.yml` に保存します。再適用時に初回の JSON を再指定する必要はありません。
`verify` は主コンポーネントの検査後に追加タスクを実行し、失敗時には成功の検証記録を作りません。
`mise run check` と生成 CI も `standards:companions` を通じて追加タスクを実行します。

## 適用版の確認

利用先では次のコマンドで、ロックに記録された適用版を確認できます。
起動用 CLI が古い場合も、その CLI の版ではなく `.standards/lock.json` の値を表示します。

```sh
mise run standards:version
```

表示例は `dev-standards 適用版: 0.3.1` です。この表示は設定やテストの成功を意味しません。
`mise run standards:prepare` では、設定の整合性を確認した後に適用版とバンドルの SHA-256 も表示します。
`dev-standards inspect` の `standard` にも、採用リポジトリ、版、バンドルの SHA-256 を含めます。
未導入の場合は `standard` を出力せず、壊れたロックを成功扱いにしません。
`dev-standards version` / `--version` は、実行している CLI 自身の版を表示します。

## 再適用

```sh
dev-standards plan --output .standards/plans/reapply.json
dev-standards apply --plan .standards/plans/reapply.json
dev-standards check
```

同じ入力からの再適用計画は、変更件数ゼロ、終了コード `0` になります。
保存済みの計画を再実行した場合も、初回に変更があった計画は入力が変化しているため拒否します。
再適用には新しく作った計画を使用してください。

設定を変更するときは生成物ではなく正本を編集します。

| 生成物 | 固有設定の正本 |
| --- | --- |
| `.editorconfig` | `.standards/editorconfig.local` |
| `.gitignore` | `.standards/gitignore.local` |
| `.gitattributes` | `.standards/gitattributes.local` |
| `mise.toml` | `.standards/mise.project.toml` |
| 共通タスク | `.standards/tasks.project.toml` に固有タスクを追加 |

正本の変更後に `plan` と `apply` を実行します。
生成物を直接編集した場合は衝突として停止し、その編集を自動で破棄しません。
削除が未コミットの場合も保護します。削除がコミット済みの管理ファイルは復元計画を作れます。

## 既存リポジトリへの適用条件

既存の EditorConfig、gitignore、mise の内容を固有設定へ移し、生成物に組み込みます。
無関係なファイルや未コミット変更は保持します。

既存 mise に共通の予約タスク、`task_config`、外部タスクディレクトリがある場合は、
名前と優先順位を安全に自動移行できないため、理由を示して停止します。
先に固有タスクへ移行してから、計画を作成してください。
既存の `tools.go` は完全な `x.y.z` の固定値が必要です。

共通の基本ルールは例外で免除できません。空でない例外宣言は拒否します。
構造を定める例外スキーマは同梱していますが、期限付き免除の運用は後続の実装です。

## ルールと正本

| パス | 内容 |
| --- | --- |
| `rules/core/*.yml` | ルール ID、適用条件、検査、修正方法 |
| `guidance/core/*.md` | 各ルールの説明 |
| `profiles/*.yml` | 適用ルールと対応 OS、必要なツール |
| `templates/base/` | EditorConfig と gitignore.io の採用入力 |
| `schemas/` | 定義形式の JSON Schema |
| `.standards/repository.yml` | 利用先の構成と採用版 |
| `.standards/lock.json` | 版、定義一式の SHA-256、Go の版、gitignore 入力のハッシュ |
| `.standards/state.json` | 管理ファイルの前回出力とモード |

共通定義は実行ファイルに埋め込みます。通常の検証と再適用はネットワークから最新版を取得しません。
`.gitignore` の共通入力は gitignore.io API で整備し、固有設定を保持して再生成します。
取得・更新と検証の手順は [gitignore のルール](guidance/core/gitignore.md) を参照してください。
ロックと異なる版・入力を使う場合も、暗黙に更新せず停止します。
この初期版はソースからビルドして使用し、リリース配布物の取得・検証は後続の実装で扱います。

## 保持と失敗時の動作

適用前に、対象ルート、HEAD、入力、生成結果を再確認します。
計画を編集して任意のパスや内容を追加しても、正本からの再生成と一致しなければ適用しません。
パスの親参照、管理対象のシンボリックリンク、通常ファイル以外への書き込みも拒否します。

適用中の通常のエラーでは、今回書いたファイルを復元します。
別の処理による編集を検出したファイルは、復元時にも上書きせず衝突を報告します。
プロセスの強制終了・電源断に対する完全なトランザクション復旧は初期版の対象外です。
残った `.dev-standards-apply.lock` は、実行中の処理がないことを確認してから取り除き、再計画してください。

終了コードは `0` が対象処理の成功、`1` が差分・不適合、`2` が設定不正・衝突・実行不能です。
構造検証に成功しただけで、未実行の検査も含めて完全適合とは表示しません。

検証記録は、同じリポジトリ内の追跡対象の通常ファイルを指すシンボリックリンクも扱います。
外部参照や管理ファイルのシンボリックリンクは拒否します。サブモジュールは固定コミットと
初期化状態を区別し、初期化済みなら内容の変更も記録を無効化します。
未初期化のサブモジュールを自動取得したり、そのコードを検証済みとみなしたりしません。

自律保守の CI は、固定版の CLI を Go 1.27.3 で起動し、正本からの再生成結果を照合します。
初回の CI には Go モジュール取得のネットワーク接続が必要です。問題の記録には `mise run standards:feedback` を使えます。
