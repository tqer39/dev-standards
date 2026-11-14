# mise run bootstrap で繰り返し実行できるセットアップ

リポジトリのルートで `mise run bootstrap` を実行すると、開発に必要なツールと依存関係の
セットアップが完了するようにする。タスクを明示して呼び出す。
`mise bootstrap` はマシン全体の設定も適用する組み込みコマンドなので、
リポジトリのタスクを実行するときは `mise run bootstrap` を使用する。

mise 自体の導入、設定内容を確認したうえでの `mise trust`、認証情報や OS 固有の前提条件は
README に明記する。それ以外の自動化できるセットアップ手順は `bootstrap` タスクに集約する。
再実行しても既存の設定やユーザーの変更を壊さず、途中で失敗した場合は非ゼロで終了する。

Go は `go mod download`、Python/uv は `uv sync --locked`、Node.js/npm は
`npm ci --ignore-scripts` を共通の入口から実行する。`mise-project` は `repo:bootstrap` に接続し、
利用先で必要なセットアップ全体を定義する。追加処理は既存の固有タスクに接続し、
生成ファイルを直接変更せず、正本から計画・適用する。

計画を確認して適用し、check と verify で確認する。
これらは新規環境でのセットアップ成功を証明しないため、前提条件だけを準備した環境での
`mise run bootstrap` の初回実行と再実行を別途確認する。未実行の場合は未検証と記録する。

CI でも採用版の CLI で正本から再生成して照合する。保存済みスナップショット同士の一致だけで適合としない。

保守用の新規 checkout でも、宣言した bootstrap で依存を準備してから検証する。
ルール生成を使う利用先では、既存の生成タスクを実行して正本と生成物を照合する。

自動更新でも既存のコミットフックを維持する。利用先の件名形式が必要な場合は、
`.standards/mise.project.toml` の `vars.standards_commit_message` に1行の件名を宣言する。
宣言した件名を自動コミットと更新 PR の題名に使い、フックを無効化して形式を回避しない。

呼び出し方は [mise の CLI リファレンス](https://mise.jdx.dev/cli/) を参照する。
