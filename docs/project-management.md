# 横断プロジェクト管理

## 共通の運用設定

| 項目 | 設定 |
| --- | --- |
| 横断 Project | [既存の Project #5](https://github.com/users/tqer39/projects/5) |
| owner / number | `tqer39` / `5` |
| 対象 | `tqer39/dev-standards`、`tqer39/agent-telemetry`、`tqer39/terraform-github`、`tqer39/dotfiles`、`tqer39/kazokubi`、`tqer39/quilna`、`tqer39/tts-partner`、`tqer39/omnodi`、`tqer39/vulnerability-catalog` |
| タイムゾーン | `Asia/Tokyo`。経過の日時は `2026-11-17T15:00:00+09:00` のように記載 |
| 状態フィールド | `Status` |
| 未着手 / 作業中 / レビュー待ち | `Todo` / `In Progress` / `In Review` |
| 保留 / 完了 / 中止 | `Blocked` / `Done` / `Canceled` |
| 初回着手日 / 合意済みの期限 | `Start date` / `Target date`（DATE） |
| 完了日 / 有意な進捗の記録日 | `Completion date` / `Last update`（DATE） |
| 既存の `Created` / `Updated` / `Closed` | GitHub が提供する日時として保持。作業の開始・完了とは区別 |

対象内の利用先は、固有の `docs/project-management.md` がなければ、この共通設定を参照する。
固有設定はこの文書へのリンクと相違点だけでもよい。対象外のリポジトリは自動登録しない。
Project の権限は維持し、private な Project を公開へ変更しない。
Renovate の自動同期は別の Project #8 で運用しているため、この運用からは変更しない。

## Codex の更新手順

詳細は [共通ルール](../guidance/core/project-tracking.md) に従う。
開始時に既存 Issue を検索し、今回の依頼の受入条件と一致するものを再利用する。
一致する Issue がなければ対象リポジトリへ作成する。
Issue を横断 Project へ追加し、開始・進捗・終了時に Status と日付を更新する。
依頼の受入条件を満たした証拠がある場合だけ完了とし、未マージの変更はレビュー待ちとして残す。
調査は結論と根拠が受入条件を満たした時に完了にできる。
公開や利用者確認が必要な作業は、その確認まで開いたままにする。

経過のコメントには次の内容を含める。変化のない項目を機械的に繰り返さない。

```markdown
## 進捗（日時とタイムゾーン）

- 今回の範囲:
- 完了した条件と証拠:
- 検証結果・対象コミット（未コミットならその旨）:
- 残条件・阻害要因:
- 次の行動:
- Project への反映結果:
```

親 Issue には機能全体の受入条件と子 Issue へのリンクを持たせる。
一覧の Sub-issues progress は子 Issue の完了数として読み、機能の完成度や公開率とは解釈しない。
Issue を閉じる自動化と PR の `Closes` は、残条件を消してしまわない場合だけ使う。
Status と同じ意味のラベルは新設しない。

## 一覧の構成

- 全体一覧: Repository、Title、Status、日付、親 Issue と子 Issue の進捗を確認する表。
- 状態別: Status で分けたボード。
- 保留・レビュー待ち: `Blocked` と `In Review` を絞り込んだ表。

リポジトリ別にグループ化すると各プロジェクトを比較しやすい。
目標日がある作業は日付で並べ、未定の作業に架空の期限を付けない。
日付を持つ作業が増えた段階でロードマップ表示を追加できる。

## 初回設定の記録（2026-11-17）

2026-11-17 の確認では Project #5 は既に横断管理用として存在し、登録は1件だった。
Status は Todo、In Progress、Done の3種類で、作業日付のカスタムフィールドはなかった。
共通ラベルには旧ハーネスの承認・実行用ラベルがなく、dev-standards の Project 機能は Terraform で有効だった。
同日の設定更新で、既存 item と Done の状態を保持し、不足する状態3種類、日付4項目、ビュー3種類を整備した。
全体一覧は Repository でグループ化し、状態別はボード、保留・レビュー待ちは Status の絞り込みにした。
対象9リポジトリの未完了 Issue 64件を追加し、既存1件と合わせて65件、重複なし、対象の登録漏れなしを再取得して確認した。
今回の整備を追う [Issue #57](https://github.com/tqer39/dev-standards/issues/57) に開始日・最終更新日を記録した。
件数は初回設定時の記録であり、現在の進捗は Project と Issue を参照する。
Auto-close issue、Item closed、Pull request merged の自動完了処理は無効にし、Auto-add sub-issues to project は維持した。
既存の全 Issue を完了度の推測だけで一括分類せず、まず未完了 Issue を状態未確認（Status 空欄）として集約し、
現在の根拠を確認できた作業から状態と日付を更新する。依存更新は専用 Project を優先する。

GitHub CLI では `gh project list --owner tqer39 --format json` と
`gh project field-list 5 --owner tqer39 --format json` で設定を確認する。
読み取りは `read:project`、更新には `project` の権限が必要である。
権限が不足した場合は、ユーザーが `gh auth refresh -h github.com -s project` で認証する。
トークン自体は表示・記録しない。

Projects の更新は Codex が行う。dev-standards の CLI が GitHub を常駐監視する機能ではない。
新ルールは採用版の更新計画を確認・適用し、生成される `.standards/rules.md` を通じて利用先へ配る。
rulesync 利用先では生成後に `AGENTS.md` まで再生成して入口を確認する。
ローカル変更だけでは既存利用先への配布が完了したとは扱わない。

## 検証条件

- 初回適用で共通ルールが配布され、同じ入力の再適用計画が差分ゼロになる。
- 既存の指示、Project 固有設定、生成物の手編集を保持し、衝突と書き込み失敗時に上書きしない。
- `check` は進捗更新を検証済みとせず、`review_required` を返す。
- GitHub 上で今回の Issue の登録、Status、日付を更新後に読み直して照合する。
- `mise run check`、`mise run test:race`、`mise run session:verify`、`mise run standards:finish` を実行する。

操作仕様は [GitHub CLI の Projects](https://cli.github.com/manual/gh_project) と
[表の表示設定](https://docs.github.com/en/issues/planning-and-tracking-with-projects/customizing-views-in-your-project/customizing-the-table-layout)を参照する。
