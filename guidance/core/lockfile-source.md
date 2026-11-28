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
- `package-lock.json` は `lockfileVersion` 2 以上が必要である。
  `packages` がなく旧形式の `dependencies` だけがある場合は、判定できないので不適合にする。
- `pnpm-lock.yaml` が複数の文書に分かれている場合は、すべての文書を読む。
- `node-npm` と `python-uv` はコンポーネントの lockfile を、`mise-project` はリポジトリ内のすべての lockfile を対象にする。
  対象の lockfile がなければ `not_applicable` と報告する。

## レジストリの設定

lockfile が公式の URL を指していても、レジストリの設定で実際の取得先は変わる。
npm は `resolved` の `registry.npmjs.org` を設定中のレジストリに読み替え、pnpm は `integrity` だけの項目を設定中のレジストリから取得する。
このため、lockfile と同じディレクトリの設定も読み、次のどれかがあれば `fail` と報告する。

| ファイル | 不適合にする設定 |
| --- | --- |
| `.npmrc`（npm と pnpm） | `registry` または `@scope:registry` が `https://registry.npmjs.org/` 以外。`${NAME}` のような環境変数の参照も含む |
| `pnpm-workspace.yaml` | `registry`、`registries` に `https://registry.npmjs.org/` 以外がある。`namedRegistries` または `pnprServer` がある |

- `.npmrc` は npm の INI パーサーと同じに読む。キーと値の引用符、JSON のエスケープ、`;` と `#` のコメントを解釈し、
  同じキーは最後の値を採る。
- npm と同じに解釈できない行がレジストリに関わりうる場合は、不適合にする。
  文字列にならない引用、セクション、配列（`registry[]`）、キーの中の `${NAME}`、
  `registry` を含む未知のキー（`replace-registry-host` など）が該当する。
  `//host/:_authToken` のような認証情報のキーは対象にしない。
- pnpm では、lockfile のディレクトリからリポジトリのルートまでの `.npmrc` と、
  上にたどって最初に見つかった `pnpm-workspace.yaml` を読む。lockfile を分けたワークスペースでは、上位の設定が効くためである。
- 安全に読めない `.npmrc`（リポジトリの外を指すシンボリックリンクなど）と、解析できない `pnpm-workspace.yaml` は、
  実際のレジストリが分からないので不適合にする。
- uv は `uv.lock` に記録した URL から取得するので、uv の index の設定は読まない。
- `check` が読むのはリポジトリ内の設定だけである。利用者や端末の設定（`~/.npmrc` など）、環境変数、
  コマンドラインの指定によるレジストリの変更は判定しない。

## リポジトリ内のディレクトリを指す依存

取得を伴わない依存は、lockfile の位置から解決した先が、リポジトリ内の通常のディレクトリである場合だけ対象外にする。
npm の `link` とその指す先、pnpm の `directory` と importer の `link:`、uv の `editable`・`virtual`・`directory` が該当する。
次の場合は不適合にする。

- 親ディレクトリの参照でリポジトリの外に出る。
- 絶対パスである。
- 途中にシンボリックリンクがある。
- 先頭より後に `..` がある（`alias/../shared` など。手前の要素を確かめられない）。
- 存在しない、またはディレクトリでない。

npm の同梱（`inBundle`）の項目は、親のパッケージの tarball に含まれるので対象外にする。

## 報告に載せる内容

lockfile のキーや設定の値には、認証情報つきの URL が入ることがある。
`check` は、レジストリのパッケージ名の形をしたキーだけを報告に載せ、それ以外は順番だけを示す。
設定の値と、解析エラーの本文は載せない。

## 判定しない項目

- レジストリが配布する内容そのものの安全性。取得元とハッシュの形式だけを判定する。
- 利用者や端末の設定、環境変数によるレジストリの変更。
- 社内レジストリやミラーなど、公式レジストリ以外の取得元の許可。現在は許可する設定を持たない。
  必要になった利用先は、`mise run standards:feedback` で記録する。
