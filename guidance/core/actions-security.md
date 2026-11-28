# GitHub Actions のサプライチェーン攻撃対策を導入する

GitHub Actions を使うリポジトリでは、すべてのワークフローに次の対策を導入する。
依存するアクションや PR の内容が改ざんされても、リポジトリの権限と secret に被害が及ばないようにする。
対策は、`zizmor` の判定項目と、人が確認する項目に分かれる。

<!-- cspell:words artipacked zizmor betterleaks -->

## zizmor が判定する項目

括弧内は、対応する zizmor の監査名を示す。

- 外部のアクションは `uses:` でコミット SHA の全桁に固定する（`unpinned-uses`）。
  タグとブランチは付け替えられるため、固定には使わない。
- ワークフローの `permissions` は `contents: read` などの最小権限にし、書き込み権限はジョブ単位で与える（`excessive-permissions`）。
- `actions/checkout` には `persist-credentials: false` を指定する（`artipacked`）。
- `pull_request_target` と `workflow_run` で、PR のコードを checkout して実行しない（`dangerous-triggers`）。
- `github.event` などの外部入力は `run:` に `${{ }}` で埋め込まず、`env:` を経由して渡す（`template-injection`）。
- secret を使うジョブとリリース用のジョブでは、PR からも書き込めるキャッシュを復元しない（`cache-poisoning`）。

自律保守を導入した利用先では、共通 CI（`dev-standards.yml`）の `supply-chain` ジョブが、
版を固定した zizmor を `--offline --min-severity medium` で実行する。
必須チェックのジョブ（`workflow-result` または `standards-check`）はこのジョブの成功を待ち、失敗すると必須チェックも失敗する。

このジョブは、dev-standards が生成する `dev-standards.yml` と `dev-standards-version-lag.yml` だけを検査する。
利用先が書いたワークフローは対象にしない。
既存のワークフローに指摘が残る利用先で、採用版の更新と同時に CI が失敗するのを避ける。

## 人が確認する項目

zizmor の成功だけでは判断できないので、ワークフローを追加・変更するときに差分と照合する。

- 固定した SHA の行末に版をコメントで残し、SHA がその版のコミットであること。
- 書き込み権限を与えたジョブと、`persist-credentials: false` を外したジョブが、その権限と認証情報を実際に必要とすること。
- クラウドへの認証に、長期のアクセスキーではなく OIDC を使うこと。
- 依存の更新に Renovate の `minimumReleaseAge` で待機期間を設け、公開直後の版を取り込まないこと。
  脆弱性の修正は待機の対象から外す。
- zizmor の指摘を `.github/zizmor.yml` や `# zizmor: ignore[...]` で除外する場合は、除外する理由。
- 利用先が書いたワークフローも、「zizmor が判定する項目」を満たすこと。
  次の節の条件を満たして自動判定に移すまでは、人が照合する。

## 検査を利用先の全ワークフローへ広げる条件

利用先は、次の条件を満たしてから、自分の CI で `.github` 全体を検査する。
共通 CI は生成物なので、直接編集して対象を広げない。

- `zizmor --offline --min-severity medium .github` の指摘が 0 件になっている。
  意図して残す指摘は、理由とともに除外を設定してある。
- zizmor の版を `mise.toml` などで固定し、Renovate の待機期間を経て更新する。

共通 CI の対象を `.github` 全体へ広げるのは、共通 CI を持つすべての利用先で 1 つ目の条件を確認した後にする。
共通 CI を持たない利用先には `supply-chain` ジョブが届かないので、同じ検査を自分の CI で実行する。

## secret の検査

`supply-chain` ジョブは、版を固定した betterleaks で作業ツリーの secret も検査する。
過去の誤検知を拾わないよう、全履歴は対象にしない。検出した値はログに表示しない。

- 実際の secret を検出した場合は、除外せず、先に失効と差し替えを行う。
- 文書の例やテストの固定値などの誤検知は、リポジトリのルートの `.betterleaks.toml` か、
  該当行の `betterleaks:allow` コメントで除外する。`.betterleaks.toml` があれば、ジョブはそれを読み込む。
- `.betterleaks.toml` には、`[extend]` の `useDefault = true` を必ず書く。
  これがない設定は既定のルールを置き換えるので、除外だけを書くと何も検出しなくなる。
- 除外は検査対象の PR 自身が追加できる。除外を追加・変更する PR では、対象が実際の secret でないことをレビューで確認する。
  `useDefault = true` が残っていることも確認する。

`.betterleaks.toml` で文書の例を除外する場合は、次のように書く。

```toml
[extend]
useDefault = true

[[allowlists]]
description = "文書に載せた例示用の値"
paths = ['''^docs/example\.md$''']
```

## check の扱い

`check` はこの一覧を判定せず、`review_required` と報告する。
`supply-chain` ジョブの成功は生成ワークフローと作業ツリーの secret についての判定であり、
利用先のワークフロー全体がこのルールに適合することを示さない。
