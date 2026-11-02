# dev-standards

個人開発リポジトリの共通ルールを、設定の正本と検査で管理する CLI です。
初回適用では変更計画を作り、確認した計画だけを反映します。
同じ版の再適用では、入力が同じなら差分が出ません。
管理ファイルの改行と gitignore.io の特殊なパターンは `.gitattributes` でも保持します。

## この版の実装範囲

Go はリポジトリのルートに module が１つある構成、Python/uv はルートまたは
指定ディレクトリにビルド可能なパッケージが１つある構成に対応します。
macOS、Linux、Windows で同じ CLI と mise タスクを使用します。

- EditorConfig の配置と固有設定の保持
- gitignore.io から取得済みの入力を保存した、オフラインでの再生成
- mise によるツールの固定と bootstrap、lint、format、test、build の入口
- 要件、候補、選定理由を含む言語選定記録の構造検証
- 変更計画、ロック、適用済み状態、衝突検出、通常の書き込み失敗時の復元
- YAML と JSON Schema によるルール・宣言・プロファイル・例外の検証

既存の Codex ログインを使う自律保守、固定 hooks、通常開発の検証記録、
更新 PR の自動検証・マージ、版の更新と利用先への移行にも対応します。
初回設定と運用条件は [自律保守](docs/autonomous-maintenance.md) を参照してください。

rulesync による他の LLM への配布、Renovate の共通設定、GitHub 管理設定の変更、
複数言語のモノレポは後続の実装です。

## 開発環境の準備

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
