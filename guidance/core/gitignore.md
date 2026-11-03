# gitignore.io API で .gitignore を整備する

`.gitignore` の共通設定は gitignore.io API から取得したテンプレートを基に整備する。
使用言語と対応 OS に合うテンプレートを選び、取得元 URL と取得内容を保存する。
通常の適用・検証では保存済みの入力を使い、毎回 API から最新版を取得しない。

## API からの取得と更新

テンプレート一覧は `https://www.toptal.com/developers/gitignore/api/list?format=json` で確認できる。
生成 API は `https://www.toptal.com/developers/gitignore/api/<カンマ区切りのテンプレート名>` を使う。
例えば Go と対応 OS の組み合わせは `go,linux,macos,windows` とする。
API の仕様は [公式 API 文書](https://docs.gitignore.io/use/api) と
[コマンドラインの例](https://docs.gitignore.io/use/command-line) を参照する。

dev-standards の現在のプロファイルでは、言語の `go`、`python`、`node` のいずれかと
`linux,macos,windows` を採用する。取得済みの入力は `templates/base/gitignore*.txt` に保存し、
利用先には `.standards/vendor/gitignore/` 配下へ配置する。取得元 URL と入力の SHA-256 はロックに記録する。

共通入力を更新するときは、API の応答を一時ファイルで受け取り、取得の成功と内容・差分を確認してから
対応するテンプレートへ反映する。通信失敗、空の応答、エラー内容を採用入力として保存しない。
既存の `.gitignore` に API の応答を直接上書きしない。
テンプレートの変更は共通定義の更新として検証し、利用先では検証済みの版へ移行する。
利用先の保存済み入力やロックだけを直接書き換えない。

## 固有設定の保持と検証

既存の除外設定とリポジトリ固有の追加設定は `.standards/gitignore.local` に保持する。
生成された `.gitignore` を直接編集せず、正本の変更から計画を作成して確認・適用する。

- `check` と `verify` で、採用入力との整合性とプロジェクトの検証結果を確認する。
- 必要なソース・設定ファイルを除外せず、秘密情報や一時ファイルの除外を固有設定で打ち消していないことを確認する。
- 同じ入力で再適用計画を作成し、差分がゼロになることを確認する。
- 衝突や取得・検証の失敗時は既存設定を保持し、未検証の状態を成功として扱わない。
