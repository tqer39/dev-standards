# 公開直後の依存と install スクリプトを取り込まない

npm、pnpm、uv を使うリポジトリでは、公開直後の版を解決しない待機期間と、依存の install スクリプトの抑止を、
パッケージマネージャー自身の設定に書く。
Renovate の `minimumReleaseAge` は、Renovate が作る更新にしか効かない。
パッケージマネージャーの設定は、人やエージェントが手元で `npm install foo`、`pnpm add foo`、`uv add foo` を実行したときにも効く。

<!-- cspell:words sdist cooldown pyproject -->

## check が判定する項目

`check` は、plan の入力にある lockfile を 1 つずつ判定する。判定には、同じディレクトリの設定を読む。
設定ファイルは dev-standards が生成しないので、利用先が自分で書く。

| lockfile | 待機期間（`cooldown`） | install スクリプト（`install-scripts`） |
| --- | --- | --- |
| `package-lock.json` | `.npmrc` の `min-release-age` が正の日数で、実行する npm が 11.10 以上 | `.npmrc` の `ignore-scripts=true` |
| `pnpm-lock.yaml` | `pnpm-workspace.yaml` の `minimumReleaseAge` が正の分数で、`minimumReleaseAgeStrict` が `false` でない（pnpm 11.0.0〜11.0.3 では `true`） | pnpm 11 以上で、`dangerouslyAllowAllBuilds` が有効でない |
| `uv.lock` | `uv.toml`、なければ `pyproject.toml` の `[tool.uv]` の `exclude-newer` が、正の期間か過去の日時 | 判定しない（後述） |

npm:

- 同じ `.npmrc` に `before` があると、npm は `min-release-age` より `before` を優先する。この場合は合格にしない。
- `min-release-age` は npm 11.10.0 で追加された。それより前の npm は設定を無視するので、設定があっても合格にしない。
- npm の版は、mise が導入するものを根拠にする。`.standards/mise.project.toml` の `tools.npm`、
  なければ `tools.node` に同梱の npm で判断する。
  npm 11.10 以上は、Node.js 24.14.1 以上の 24 系、25.7.0 以上の 25 系、26 以上に同梱されている。
- `package.json` の `packageManager` に `npm@11.10.0` と書いても、npm は自分の版を切り替えない。
  共通の bootstrap と CI も npm を直接呼ぶので、`packageManager` と `engines` は判定に使わない。
- 版が分からない場合も合格にしない。

pnpm:

- pnpm の版は、`package.json` の `packageManager`、なければ `.standards/mise.project.toml` の `tools.pnpm` から読む。
- `pnpm-workspace.yaml` は、lockfile のディレクトリから上にたどって最初に見つかったものを読む。
- pnpm 11 以上では、`minimumReleaseAge` の既定が 1440 分（1 日）になる。ただし、既定の待機は strict にならない。
  条件を満たす版がないと、待機期間の内側の版を取り込む。このため、既定のままでは合格にしない。
- pnpm 11.0.4 以降では、`minimumReleaseAge` を明示すると `minimumReleaseAgeStrict` の既定が `true` になり、満たす版がなければ失敗する。
  既定の 1 日のまま合格にするには、`minimumReleaseAgeStrict: true` を書く。
- pnpm 11.0.0〜11.0.3 では、`minimumReleaseAge` を明示しても `minimumReleaseAgeStrict` の既定は `false` のままになる。
  この範囲の版では、`minimumReleaseAgeStrict: true` を書かないと合格にしない。
  版によらず strict にするため、設定には `minimumReleaseAgeStrict: true` を明示する。
- pnpm 11 以上は、依存の build script を既定で実行しない。pnpm 11 で `onlyBuiltDependencies` などは廃止され、
  `allowBuilds` に置き換わった。build script が必要な依存だけを `allowBuilds` で許可する。

uv:

- 同じディレクトリに `uv.toml` があると、uv は `pyproject.toml` の `[tool.uv]` を読まない。`check` も `uv.toml` だけを読む。
- `exclude-newer` は、`"7 days"` や `P7D` のような正の期間か、過去の RFC 3339 の日時のときだけ合格にする。
  0 の期間、未来の日時、解釈できない値は合格にしない。

共通:

- `node-npm` と `python-uv` はコンポーネントの lockfile を、`mise-project` はリポジトリ内のすべての lockfile を対象にする。
  対象の lockfile がなければ `not_applicable` と報告する。
- `check` はリポジトリ内の設定だけを読む。利用者や端末の設定（`~/.npmrc`、`~/.config/uv/uv.toml` など）、
  環境変数、コマンドラインの指定は判定しない。
- 読めない設定、解析できない設定は合格にしない。

## 判定の扱い

設定が足りない場合、`check` は `warn` と報告する。終了コードは変えない。
既存の利用先の多くはこの設定を持たない。不適合にすると、採用版の更新と同時に必須チェックが失敗する。
`warn` は適合を意味しない。報告の `evidence` に従って設定を追加する。

## 設定の例

待機期間は 7 日を目安にする。Renovate の `minimumReleaseAge`（共有設定では 14 日）より短くする。

`.npmrc`:

```ini
min-release-age=7
ignore-scripts=true
```

`pnpm-workspace.yaml`:

```yaml
minimumReleaseAge: 10080
minimumReleaseAgeStrict: true
allowBuilds:
  esbuild: true
```

`pyproject.toml`:

```toml
[tool.uv]
exclude-newer = "7 days"
```

- リポジトリの `.npmrc` に認証情報を書かない。
- `ignore-scripts=true` にすると、依存の `postinstall` などは実行されない。
  ネイティブモジュールなどで必要な場合は、対象を限って `npm rebuild <package> --ignore-scripts=false` を実行する。
  `.npmrc` の設定は `npm rebuild` にも効くので、このコマンドだけで解除する。
  `npm run` や `npm test` で指定したスクリプトは実行されるが、その `pre`・`post` スクリプトは実行されない。

## lockfile と CI との関係

- npm: `min-release-age` は新しく解決する版にだけ効く。`npm ci` は `package-lock.json` のとおりに導入するので、影響を受けない。
- uv: `exclude-newer` と `exclude-newer-package` を追加・変更・削除したら、`uv lock` を実行して `uv.lock` を更新する。
  更新しないと、`uv sync --locked` と `uv run --locked` は lock が古いとして失敗する。
  相対指定（`"7 days"`）では、uv 0.11.32 と 0.13.8 は `uv.lock` の `[options]` に期間（`exclude-newer-span = "P7D"`）を記録する。
  併記される `exclude-newer` は互換のための固定値（`0001-01-01T00:00:00Z`）で、解決した時刻ではない。
  この 2 つの版では、日付が変わっても `--locked` は失敗せず、`uv lock` をやり直しても差分は出ない。
- pnpm: pnpm 11.1.3 以降は、lockfile を読み込むときにも `minimumReleaseAge` を lockfile の各項目に適用して検証する。
  `trustLockfile: true` はこの検証を省くので、設定しない。

## Renovate との関係

- Renovate の `minimumReleaseAge` とパッケージマネージャーの設定は別々に働く。Renovate の公式文書は両方に設定することを勧めている。
- パッケージマネージャーの待機期間を Renovate より短くすれば、通常の更新ではぶつからない。
- 脆弱性の修正は Renovate の待機の対象外（`vulnerabilityAlerts` の `minimumReleaseAge` が `null`）だが、
  パッケージマネージャーの待機はそのまま効く。待機期間より新しい修正版への更新は、lockfile の生成で失敗する。
  急ぐ場合は、該当のパッケージだけを除外する（npm は `min-release-age-exclude`、pnpm は `minimumReleaseAgeExclude`、
  uv は `exclude-newer-package`）。
- 除外は、除外したパッケージの lockfile にある版がすべて待機期間を過ぎた後に取り除く。修正版を取り込んだ直後には取り除かない。
  pnpm の除外をパッケージ名だけで書くと、そのパッケージのすべての版に効く。
  除外の間に、修正版より新しい版が lockfile に入ることがあるので、修正版の公開日だけでは判断しない。
  pnpm 11.1.3 以降は lockfile の各項目にも待機期間を適用する。
  待機期間の内側の版が残ったまま除外を外すと、次の install が拒否される。
  pnpm 11.1.2 以前は lockfile を検証しないが、同じ手順で取り除く。
- uv は、`exclude-newer-package` を追加したときだけでなく、取り除いたときにも `uv lock` を実行して `uv.lock` を更新する。
  取り除いただけでは、`uv sync --locked` と `uv run --locked` は lock が古いとして失敗する（uv 0.11.32 と 0.13.8 で確認）。
- Renovate は uv の `exclude-newer` を読まず、uv にも待機期間を渡さない。

## uv の sdist のビルド

uv は wheel のない依存を sdist からビルドし、そのとき依存のコードを実行する。
`[tool.uv]` の `no-build = true` または `no-build-package` でビルドを禁止できる。
wheel を配布していない依存があると導入できなくなるため、`check` では判定しない。
依存がすべて wheel で配布されている利用先では、設定を検討する。
