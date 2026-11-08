# GitHub Actions のサプライチェーン攻撃対策を導入する

GitHub Actions を使うリポジトリでは、すべてのワークフローに次の対策を導入する。
依存するアクションや PR の内容が改ざんされても、リポジトリの権限と secret に被害が及ばないようにする。

- 外部のアクションは `uses:` でコミット SHA の全桁に固定し、版を行末のコメントに残す。
  タグとブランチは付け替えられるため、固定には使わない。
- ワークフローの `permissions` は `contents: read` などの最小権限にし、書き込み権限は必要なジョブにだけ与える。
- `actions/checkout` には `persist-credentials: false` を指定する。後続の処理で認証情報を使うジョブだけを除く。
- `pull_request_target` と `workflow_run` で、PR のコードを checkout して実行しない。
- `github.event` などの外部入力は `run:` に `${{ }}` で埋め込まず、`env:` を経由して渡す。
- secret を使うジョブとリリース用のジョブでは、PR からも書き込めるキャッシュを復元しない。
- クラウドへの認証には、長期のアクセスキーではなく OIDC を使う。
- 依存の更新は Renovate の `minimumReleaseAge` で待機期間を設け、公開直後の版を取り込まない。
  脆弱性の修正は待機の対象から外す。

`check` はこの一覧を判定せず、`review_required` と報告する。
ワークフローを追加・変更するときに、差分をこの一覧と照合する。
`zizmor` などの静的解析を CI で実行すると、照合の一部を自動化できる。
