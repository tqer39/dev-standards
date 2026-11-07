# agent-harness の運用

設計は [仕様書](superpowers/specs/2026-11-07-claude-codex-harness-design.md) を参照する。

## 初回の準備

```sh
mise run build
bin/agent-harness setup
scripts/agent-harness-probe.sh
bin/agent-harness doctor --repo <owner>/<name>
```

1Password の Vault、サービスアカウント、GitHub App は人間が作成する。

## タスクの実行

人間が受入条件を承認してから実行する。

```sh
bin/agent-harness run \
  --repo <owner>/<name> --title 'feat: 変更の要約' --kind feature \
  --task-file task.md --criteria-file criteria.md --approved-by-human
```

固定する受入テストがあれば `--acceptance-patch tests.patch` を追加する。
実装担当の画面は Herdr で `impl-<run_id>` として確認できる。

| 終了コード | 意味 | 対応 |
| --- | --- | --- |
| 0 | マージと確認が完了、または revert が完了 | 表示された状態を確認する |
| 3 | 利用枠の解除待ち | 解除後に `bin/agent-harness resume --run <run_id>` |
| 4 | 人間の判断待ち | PR の `needs-human` と理由を確認する |
| 1 | その他の失敗 | 表示された内容を確認する |

## 停止と再開

main の CI が失敗すると、そのリポジトリの自動処理を止めて revert する。
原因を確認したら、人間が次を実行する。

```sh
bin/agent-harness clear-stop --repo <owner>/<name>
```

Mac の停止などでマージ後の確認が途中で終わった場合は、次を実行する。

```sh
bin/agent-harness reconcile --repo <owner>/<name>
```

## 実行記録

`~/.local/state/agent-harness/runs/<run_id>.json` に保存する。

## リポジトリ固有の設定

基準ブランチの `.agent-harness/policy.json` で追加できる。既定の停止パスは削除できない。

```json
{
  "stop_paths": ["billing/**"],
  "verify": ["mise", "run", "check"],
  "required_check": "workflow-result",
  "external_review_check": "CodeRabbit",
  "external_review_authors": ["coderabbitai"]
}
```

`external_review_check` に `none` を指定すると、外部レビューの完了を待たない。
未解決のスレッドは、この設定にかかわらず確認する。

## 関連リポジトリの改善

`agent-telemetry`、`omnodi`、`terraform-github` に改善が必要なときは、都度 PR を出す。
`terraform-github` はマージで適用まで進むので、PR の作成までとする。
