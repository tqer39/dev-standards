# gitignore.io の入力を保存する

Go、Linux、macOS、Windows の gitignore.io 入力を版に含め、ネットワークに
依存せず `.gitignore` を再生成します。採用入力はロックのハッシュで固定します。

リポジトリ固有のパターンは生成物を直接編集せず、`.standards/gitignore.local` に
記録して再適用します。共通ルールの管理ファイルを無視するパターンは受け入れません。
