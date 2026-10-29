# 整形を検証する

`format` は `go fmt ./...` で Go ファイルを整形します。`format:check` はファイルを
書き換えず、標準の Go 整形との差分があるファイルを検出します。

修正には `mise run format` を使い、`mise run format:check` で差分がないことを
確認します。`check` は設定だけを確認し、整形検査は実行しません。
