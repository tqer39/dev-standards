# dev-standards の開発ルール

回答と Markdown 文書は日本語にする。
README.md と docs/decisions/0001-implementation-language.md を読んでから変更する。

- 利用先の既存設定と無関係な変更を保持する。
- 変更計画と再生成の検証を経由し、未検証の処理を成功として扱わない。
- CLI の動作変更には初回適用、再適用、衝突、失敗時の回帰テストを追加する。
- 検証は mise run check と mise run test:race を実行する。
- 依存変更では mise run security:check も実行する。
- push 前に git diff --check と最終差分を確認する。
