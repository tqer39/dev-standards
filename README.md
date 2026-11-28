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

既存の Codex または Claude Code のログインを使う自律保守、固定 hooks、通常開発の検証記録、
更新 PR の自動検証・マージ、版の更新と利用先への移行にも対応します。
初回設定と運用条件は [自律保守](docs/autonomous-maintenance.md) を参照してください。

AGENTS.md を rulesync で生成している利用先では、共通の入口を rulesync のルールとして配置できます。
dev-standards 自身による rulesync を使ったその他の LLM への配布、Renovate の共通設定、GitHub 管理設定の変更、
言語別の検証を自動構成する複数言語のモノレポ対応は後続の実装です。
Node.js/npm を主コンポーネントとし、既存タスクで検証する Go/Swift パッケージを明示的に併設できます。

## Claude Code への対応

`.standards/repository.yml` の `ai.targets` で、hooks と共通指示を配置するエージェントを選びます。
`codexcli` と `claudecode` を指定でき、両方を並べることもできます。省略時は従来どおり `codexcli` です。

| ターゲット | 配置するファイル |
| --- | --- |
| `codexcli` | `AGENTS.md`、`.codex/hooks.json` |
| `claudecode` | `AGENTS.md`、`CLAUDE.md`、`.claude/settings.json` の hooks |

`CLAUDE.md` と `.claude/settings.json` の既存の内容は、`.standards/claude.local.md` と
`.standards/claude-settings.local.json` に正本として保存して合成します。hooks 以外の設定も保持します。
rulesync を使う利用先では、`CLAUDE.md` を rulesync の出力として扱い、dev-standards は管理しません。
共通の入口は rulesync のルールとして Claude Code にも届きます。

新しい利用先では `onboard` に `--ai-targets claudecode` を追加します。
既存の利用先では `ai.targets` を書き換え、`dev-standards plan` と `apply` で再生成します。
ターゲットから外したエージェントのファイルは、導入前の内容に戻すか、元がなければ削除します。
外している間に編集したファイルは、再びターゲットに加えたときに正本として取り込みます。
切り替えは人が `plan` と `apply` で行います。自律保守は、未適用の切り替えによる削除や復元を
所有範囲外の変更として拒否し、その利用先の保守を失敗として記録します。
自律保守のエージェントの切り替えは [自律保守](docs/autonomous-maintenance.md) を参照してください。

## Issue と横断 Projects による進捗管理

主担当のエージェントが作業開始・進捗・終了時に Issue と横断 Project を更新する手順を共通ルールとして配布します。
自律保守または rulesync の共通指示の入口を導入した利用先に、`.standards/rules.md` として届きます。
Issue には受入条件、日時、検証結果、残条件を残し、Project の Status と日付で全体を確認します。
設定と初回確認は [横断プロジェクト管理](docs/project-management.md) を参照してください。
GitHub への更新はエージェントが行い、`check` と `verify` は更新済みとは判定せず `review_required` と報告します。

## AI が実装した PR の明示

AI が実装に関与した PR の本文は、1行目を `:robot: これは AI で書かれています。` とします。
作成・更新時に重複を避けて既存本文を保持し、保存された本文を読み直して確認します。
[共通ルール](guidance/core/ai-disclosure.md) は、共通指示の配布が有効な利用先の `.standards/rules.md` に配置し、
rulesync 利用先では共通入口を各エージェントの rules 出力へ生成して参照させます。
エージェント固有の hooks による自動挿入は行わず、`check` は遵守を `review_required` と報告します。
既存利用先への反映には、採用版の更新・適用と rulesync の再生成が必要です。

## 日本語の文章と出力

日本語の文書・出力を作成・編集する利用先では、textlint・cspell・yomiyasu を導入する
[共通ルール](guidance/core/japanese-writing.md)を配布します。
textlint は文章規則、cspell は英単語や技術用語の綴り、yomiyasu は読みやすさの推敲を担当します。
会話、コミットメッセージ、PR 本文にも同じ方針を適用します。
自律保守または rulesync の共通入口を導入した利用先へ `.standards/rules.md` として届きます。
ツール・スキルの導入と既存 lint・CI への接続は利用先で行い、既存設定と辞書を保持します。
`check` と `verify` は導入や推敲の完了を自動判定せず、`review_required` と報告します。
既存利用先への配布には採用版の更新計画と適用が必要です。

## worktree での作業

エージェントがファイルを変更するときは、既定ブランチの元の checkout ではなく worktree で行うよう指示する
[共通ルール](guidance/core/worktree.md)を配布します。
ハーネスを使わない対話での直接編集や、隣のリポジトリの変更、別のエージェントへの依頼にも適用します。
自律保守または rulesync の共通入口を導入した利用先へ `.standards/rules.md` として届きます。
これは LLM に渡す指示であり、元の checkout への書き込みを機械的に拒否する hook ではありません。
`check` と `verify` は作業場所を検査せず、`review_required` と報告します。
既存利用先への配布には採用版の更新計画と適用が必要です。

## TypeScript の型チェック

TypeScript を使う利用先では、型チェックに [tsrs](https://github.com/oxc-project/tsrs) を使う
[共通ルール](guidance/core/typescript-tsrs.md)を配布します。
tsrs は mise の npm ツールとして版を固定し、`typecheck:tsrs` などのタスクとして実行します。
tsrs は実験版のため、安定版になるまでは tsc の型チェックを必須の検査として残します。
固定した版にバイナリがないプラットフォームでは tsc を使い、tsrs のタスクを CI の必須チェックには接続しません。
版を更新するときは、配布元、対応プラットフォーム、既知の互換性の問題を確認します。
自律保守または rulesync の共通入口を導入した利用先へ `.standards/rules.md` として届きます。
配布だけでは利用先にタスクを追加せず、生成する CI も変更しません。
`check` と `verify` は tsrs の導入と実行結果を判定せず、`review_required` と報告します。
既存利用先への配布には採用版の更新計画と適用が必要です。

## 旧委譲ルールの移行

旧版で配布していた `.claude/rules/dev-standards-agent-harness.md` は廃止しました。
次の適用または自律保守で、以前の `state.json` で所有しているファイルだけを削除し、所有記録からも外します。
所有していない同名ファイルは読み書きせず保持します。所有ファイルの内容に変更があれば衝突として停止します。
クロスレビューの手順は利用者の個人設定で管理し、リポジトリごとには配布しません。

## 開発環境の準備

共通ルールとして、各リポジトリは `mise run bootstrap` で開発環境のセットアップを完了できるようにします。
前提条件の明記と初回・再実行の検証については [bootstrap のルール](guidance/core/bootstrap.md) を参照してください。
`node-npm` の `project:bootstrap` は、ローカルと CI の両方で npm 依存の導入後に実行します。
ブラウザーなど依存パッケージを必要とする追加の準備を、このタスクへ接続できます。

Go と開発ツールの版は `mise.toml` に固定しています。
内容を確認して trust を設定し、セットアップします。

```sh
mise trust
mise run bootstrap
mise run check
```

`mise run check` は、Go の静的解析、Markdown lint、整形確認、テスト、
CLI のビルドを実行します。テストには実際の CLI と mise を使う受け入れテストも含まれます。
Windows では保守検証のビルドを繰り返すため、通常テストの上限は20分、CI ジョブの上限は30分です。

```sh
mise run test:race
mise run security:check
mise run security:actions
mise run security:secrets
mise run catalog:check
```

`security:actions` は zizmor で `.github` のワークフローを、`security:secrets` は betterleaks で作業ツリーの secret を検査します。
CI ではどちらも PR と `main` への push で実行します。
`security:check`（govulncheck）は、PR と無関係な新しい脆弱性で PR を止めないよう、`main` への push と毎日の定期実行で確認します。

保守処理の受け入れテスト `TestFleetUpdatesSourceAndProjectWithoutHumanApproval` は、
複製したリポジトリで検証一式を再実行するため時間がかかります。
`-short` の指定時はこのテストだけを省略するため、`mise run test:race` では実行しません。
CI では他のテストを `GOFLAGS=-short` で実行し、このテストは別のジョブで並列に実行します。
このテストは PR では Ubuntu、`main` への push では全 OS で実行します。

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
mise run bootstrap
dev-standards check
dev-standards verify
```

`adopt` は宣言した保存先に計画と親ディレクトリを作成します。
設定ファイル自体の反映は `apply` が行います。
言語選定記録は `docs/decisions/0001-language.md` に配置します。
既に同じ場所に有効な記録があれば、`--decision` は省略できます。

`check` は設定の整合性を調べる処理です。コードの lint やテストを実行したことにはしません。
`verify` は設定を確認したうえで、mise の lint、整形確認、test、build を実行します。
開始時のファイルの状態は設定の確認より前に記録し、すべてのタスクが終わった時点の状態と比べます。
違っていれば検証記録を作りません。途中で変えて元に戻した変更は検出しません。
言語選定の妥当性のレビューと、新規環境での bootstrap は別の証拠が必要なため、
結果をそれぞれ `review_required`、`not_run` として明示します。

[GitHub Actions の安全対策](guidance/core/actions-security.md)、
[CI のキャッシュと処理時間](guidance/core/ci-speed.md)、
[Renovate の更新のまとめ方](guidance/core/renovate-grouping.md)も、
設定の照合では判定できないため `review_required` と報告します。

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
この検査は各ジョブの checkout の直後に実行します。生成 CI は、この検査より前に `run:` の step を置きません。
mise と依存の導入はその後になり、必須の lint・整形確認・テスト・ビルドも維持します。
正本を変更した後は `plan` と `apply` で CI を再生成してください。

自律保守の CI には `supply-chain` ジョブがあり、ツールと依存の導入より前に次の検査を実行します。
必須チェックのジョブ（`workflow-result` または `standards-check`）はこのジョブの成功を待ちます。

- zizmor: dev-standards が生成する `dev-standards.yml` と `dev-standards-version-lag.yml` を検査します。
  利用先が書いたワークフローは対象にしません。対象を広げる条件は[共通ルール](guidance/core/actions-security.md)に記載しています。
- betterleaks: 作業ツリーの secret を検査します。全履歴は対象にしません。
  誤検知は、利用先のルートの `.betterleaks.toml` か、該当行の `betterleaks:allow` コメントで除外します。
  `.betterleaks.toml` には `[extend]` の `useDefault = true` が必要です。
  除外だけを書いた設定は既定のルールを置き換え、何も検出しなくなります。

どちらも版を固定して `mise exec` で実行するため、利用先の `mise.toml` には追記しません。
既存利用先には採用版の更新で届きます。作業ツリーに betterleaks の検出が残っていると、更新後の CI が失敗します。
更新の前に、次のコマンドで確認してください。

```sh
mise exec aqua:betterleaks/betterleaks@1.10.1 -- betterleaks dir --no-banner --verbose --redact --unsafe-auto-load-config .
```

## 依存の取り込みと lockfile の検査

`check` と `verify` は、plan の入力にある `package-lock.json`、`pnpm-lock.yaml`、`uv.lock` と、
同じディレクトリのパッケージマネージャーの設定を読んで、次の 3 つを判定します。
設定ファイルは生成しません。利用先が自分で書きます。

| ルール | check | 判定 | 不足のときの報告 |
| --- | --- | --- | --- |
| `CORE-DEPENDENCY-INTAKE` | `cooldown` | 公開直後の版を解決しない待機期間（npm 11.10 以上の `min-release-age`、pnpm の strict な `minimumReleaseAge`、uv の `exclude-newer`） | `warn`。終了コードは変えません |
| `CORE-DEPENDENCY-INTAKE` | `install-scripts` | 依存の install スクリプトの抑止（npm の `ignore-scripts=true`、pnpm 11 以上の既定と `allowBuilds`） | `warn`。終了コードは変えません |
| `CORE-LOCKFILE-SOURCE` | `source` | lockfile の取得元が公式レジストリで完全性ハッシュがあり、リポジトリのレジストリ設定が取得先を変えていないこと | `fail`。終了コード 1 で終了します |

- 対象の lockfile がなければ `not_applicable` と報告します。
- `warn` は適合を意味しません。採用版の更新と同時に利用先の必須チェックが失敗しないよう、不適合にしていないだけです。
  報告の `evidence` に、足りない設定とファイルを表示します。
- 待機期間は、設定が実際に働くときだけ合格にします。`min-release-age` を無視する古い npm、
  strict でない pnpm の既定、`uv.toml` に上書きされた `pyproject.toml`、0 や未来の `exclude-newer` は合格にしません。
- 取得元の検査は、git・URL・tarball を指す依存、公式以外のレジストリ、リポジトリの外を指すローカル依存、
  `.npmrc` と `pnpm-workspace.yaml` によるレジストリの変更を不適合にします。
  採用版を上げる前に、`dev-standards check` の `source` が `pass` であることを確認してください。
- `.npmrc` と `uv.toml` は認証情報を含むことがあるので、plan の入力には含めず、`check` が必要な設定だけを読みます。
  報告には、設定の値、URL、解析エラーの本文を載せません。
- 判定するのはリポジトリ内の設定だけです。利用者や端末の設定、環境変数は判定しません。
- plan が受け付けない入力は、`check` の結果にならず、設定不正として終了コード 2 で止まります。
  解析できない `package.json`・`package-lock.json`・`pyproject.toml`・`uv.lock`、
  `node-npm` の `lockfileVersion` 1 の `package-lock.json` が該当します。
  生成物との一致を判定する前提が欠けていて、`plan`・`apply` も同じ入力で同じように止まるためです。
  plan が中身を検証しない `mise-project` では、同じ lockfile を `check` が `source` の `fail`（終了コード 1）として報告します。

設定の例、lockfile・CI・Renovate との関係は[依存の取り込みのルール](guidance/core/dependency-intake.md)、
取得元の条件は [lockfile の取得元のルール](guidance/core/lockfile-source.md)を参照してください。

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

## 更新の遅れの検知

自律保守の利用先には、適用版がタグの付いた版から遅れていないかを週１回確認する
`.github/workflows/dev-standards-version-lag.yml` も生成します。
更新ポリシーが許可する未適用版のうち、最古のコミットから7日以上経つとジョブが失敗します。
`update.allowMajor=false` では適用中と同じ major の新しい版だけを対象にし、`true` では major 更新も含めます。
バックポートで版番号順とコミット日時順が逆転しても、最古のコミット日時を基準にします。
secret は不要で、PR は作成しません。
タグ付けも定期保守が行うので、保守全体の停止はこの検知では分かりません。
詳細は [自律保守](docs/autonomous-maintenance.md#更新の遅れの検知) を参照してください。

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

ツールの固定版や採用する共通ルールの版を更新するときは、更新先の CLI で
`dev-standards plan --upgrade --output .standards/plans/upgrade.json` を実行します。
変更計画を確認し、同じ更新先の CLI で `apply --plan .standards/plans/upgrade.json` を実行してください。
保守台帳へは登録せず、手動採用の利用先を自律保守へ切り替えません。
自律保守を採用済みの場合は、その宣言に従って hooks と CI も再生成します。
更新計画では `.standards/repository.yml` を JSON 形式へ整え、元の YAML コメントは保持しません。
この差分も計画に表示するため、適用前に確認してください。
通常の `plan` はロックを暗黙に更新せず、生成物の直接編集、古い版への更新、
計画作成後の入力変更に対する既存の保護も維持します。

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
入力を解析できないとき、または入力が受け付けられないときの診断（エラーの文言と、それを写した feedback の記録）には、
ファイル名と、分かる場合は行番号だけを載せます。
lockfile や設定のキーと値、URL、パーサーのエラーの本文は載せません。
形からは秘密でないと言えないので、英数字だけのキーや値も載せません。読んだファイルのパスと、スキーマで決まっている項目名は載せます。

この保証は、失敗したときの診断に限ります。次のものは対象外で、入力の内容を含みます。

- `check` と `verify` の報告の `changes`。生成物との差分を、変更前と変更後の全文で示します。
  `.standards/mise.project.toml` などの正本に書いた内容は、解析に成功すれば、`mise.toml` や `.standards/state.json` の差分としてそのまま出ます。
- 報告の `results` に載る名前。`repository.yml` で指定した component の名前と、`verify` が実行したタスクの名前は、意図して表示します。
- `plan` が出力する計画。入力の全文を含みます。
- 採用済みの前の版へ転送した `check`・`verify`・`plan` の出力。利用先が採用版を上げるまで、前の版の動作のままです。
- `verify` が実行するタスク（lint、test など）のログ。
- 自律保守が自分で読む JSON（台帳、`.standards/cache/host.json` など）のエラー。

認証情報は、正本にも lockfile にも書かないでください。

構造検証に成功しただけで、未実行の検査も含めて完全適合とは表示しません。

検証記録は、同じリポジトリ内の追跡対象の通常ファイルを指すシンボリックリンクも扱います。
外部参照や管理ファイルのシンボリックリンクは拒否します。サブモジュールは固定コミットと
初期化状態を区別し、初期化済みなら内容の変更も記録を無効化します。
未初期化のサブモジュールを自動取得したり、そのコードを検証済みとみなしたりしません。

自律保守の CI は、固定版の CLI を Go 1.27.4 で起動し、正本からの再生成結果を照合します。
初回の CI には Go モジュール取得のネットワーク接続が必要です。問題の記録には `mise run standards:feedback` を使えます。
