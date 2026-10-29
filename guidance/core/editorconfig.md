# EditorConfig の導入

生成する `.editorconfig` は UTF-8、LF、末尾改行、基本の空白インデントを定め、
Go ファイルにはタブを使用します。Markdown では行末空白を一律に削除しません。

リポジトリ固有の設定は生成物を直接編集せず、`.standards/editorconfig.local` に
記録して再適用します。必須設定と衝突する固有設定は受け入れません。
