# lockfile の取得元を公式レジストリに限る

lockfile に記録された依存の取得元を、公式レジストリと完全性ハッシュつきの項目に限る。
`npm ci` や `uv sync --locked` は lockfile のとおりに導入するので、lockfile の取得元を差し替える変更は、
manifest との不一致としては検出されない。`check` が lockfile の中身を読んで検出する。

## check が判定する項目

`check` は、plan の入力にある lockfile を読み、次を満たさないパッケージがあれば `fail` と報告して終了コード 1 で終了する。

| lockfile | 条件 |
| --- | --- |
| `package-lock.json` | `resolved` が `https://registry.npmjs.org/` で始まり、`integrity` が `sha512-` で始まる |
| `pnpm-lock.yaml` | `resolution` の `integrity` が `sha512-` で始まる。`tarball` があれば `https://registry.npmjs.org/` で始まる。git の項目がない |
| `uv.lock` | `source` が `registry = "https://pypi.org/simple"` で、sdist と wheel の `url` が `https://files.pythonhosted.org/` で始まり、`hash` が `sha256:` で始まる |

- git、URL、tarball を指す依存は不適合になる。公式レジストリに公開された版へ置き換える。
- リポジトリ内のディレクトリを指す項目は対象にしない。
  npm の `link`・同梱（`inBundle`）の項目、pnpm の `directory`、uv の `editable`・`virtual`・`directory` が該当する。
- `package-lock.json` は `lockfileVersion` 2 以上が必要である。
- `pnpm-lock.yaml` が複数の文書に分かれている場合は、すべての文書を読む。
- `node-npm` と `python-uv` はコンポーネントの lockfile を、`mise-project` はリポジトリ内のすべての lockfile を対象にする。
  対象の lockfile がなければ `not_applicable` と報告する。

## 判定しない項目

- レジストリが配布する内容そのものの安全性。取得元とハッシュの形式だけを判定する。
- 社内レジストリやミラーなど、公式レジストリ以外の取得元の許可。現在は許可する設定を持たない。
  必要になった利用先は、`mise run standards:feedback` で記録する。
