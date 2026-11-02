# dev-standards の開発ルール

回答と Markdown 文書は日本語にする。
README.md と docs/decisions/0001-implementation-language.md を読んでから変更する。

- 利用先の既存設定と無関係な変更を保持する。
- 変更計画と再生成の検証を経由し、未検証の処理を成功として扱わない。
- CLI の動作変更には初回適用、再適用、衝突、失敗時の回帰テストを追加する。
- 検証は mise run check と mise run test:race を実行する。
- 依存変更では mise run security:check も実行する。
- push 前に git diff --check と最終差分を確認する。

## 通常開発と自律保守

- 開始時に `mise run standards:prepare`、終了前に `mise run session:verify` と `mise run standards:finish` を実行する。
- 自律保守の原則と初回設定は `docs/autonomous-maintenance.md` を参照する。
- 共通の問題は観測と根拠だけを `dev-standards feedback` に記録する。
- 保守のための人間の追加指示・承認を待たず、設定済み台帳と保守処理へ引き継ぐ。
- 初期の保守エージェントはルール・説明・プロファイル・テンプレートを更新する。権限・検証・保守機構のコードを自ら緩和しない。
