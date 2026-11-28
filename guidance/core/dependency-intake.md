# 公開直後の依存と install スクリプトを取り込まない

npm、pnpm、uv を使うリポジトリでは、公開直後の版を解決しない待機期間と、依存の install スクリプトの抑止を、
パッケージマネージャー自身の設定に書く。
Renovate の `minimumReleaseAge` は Renovate が作る更新にしか効かないため、
人やエージェントが手元で `npm install foo`、`pnpm add foo`、`uv add foo` を実行したときにも効くようにする。

## check が判定する項目

`check` は、plan の入力にある lockfile ごとに、同じディレクトリの設定を読んで判定する。
設定ファイルは dev-standards が生成しないので、利用先が自分で書く。

| lockfile | 待機期間（`cooldown`） | install スクリプト（`install-scripts`） |
| --- | --- | --- |
| `package-lock.json` | `.npmrc` の `min-release-age` が正の日数 | `.npmrc` の `ignore-scripts=true` |
| `pnpm-lock.yaml` | `pnpm-workspace.yaml` の `minimumReleaseAge` が正の分数。未指定なら pnpm 11 以上 | pnpm 11 以上で、`dangerouslyAllowAllBuilds` が `true` でない |
| `uv.lock` | `pyproject.toml` の `[tool.uv]` の `exclude-newer` | 判定しない（後述） |

- pnpm の版は、`package.json` の `packageManager`、なければ `.standards/mise.project.toml` の `tools.pnpm` から読む。
- pnpm 11 以上は、`minimumReleaseAge` の既定が 1440 分（1 日）で、依存の build script を既定で実行しない。
  このため、設定を書かなくても合格になる。`minimumReleaseAge: 0` は待機を無効にするので合格にしない。
- pnpm 11 で `onlyBuiltDependencies` などは廃止され、`allowBuilds` に置き換わった。
  build script が必要な依存だけを `allowBuilds` で許可する。
- `node-npm` と `python-uv` はコンポーネントの lockfile を、`mise-project` はリポジトリ内のすべての lockfile を対象にする。
  対象の lockfile がなければ `not_applicable` と報告する。

## 判定の扱い

設定が足りない場合、`check` は `warn` と報告する。終了コードは変えない。
既存の利用先の多くがこの設定を持たず、不適合にすると採用版の更新と同時に必須チェックが失敗するためである。
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
allowBuilds:
  esbuild: true
```

`pyproject.toml`:

```toml
[tool.uv]
exclude-newer = "7 days"
```

- npm の `min-release-age` は npm 11.10 以上で使える。`.npmrc` に認証情報を書かない。
- `ignore-scripts=true` にすると、依存の `postinstall` などは実行されない。
  ネイティブモジュールなどで必要な場合は、`npm rebuild <package>` を明示して実行する。
  `npm run` や `npm test` で指定したスクリプトは実行されるが、その `pre`・`post` スクリプトは実行されない。

## lockfile と CI との関係

- npm: `min-release-age` は新しく解決する版にだけ効く。`npm ci` は `package-lock.json` のとおりに導入するので、影響を受けない。
- uv: `exclude-newer` を追加・変更したら、`uv lock` を実行して `uv.lock` を更新する。
  更新しないと、`uv sync --locked` と `uv run --locked` は lock が古いとして失敗する。
  相対指定（`"7 days"`）では、`uv.lock` の `[options]` に期間（`exclude-newer-span`）が記録され、時刻は記録されない。
  このため、日付が変わっても `--locked` は失敗せず、`uv lock` をやり直しても差分は出ない。
- pnpm: 現在の pnpm は、lockfile を読み込むときにも `minimumReleaseAge` を lockfile の各項目に適用して検証する。
  `trustLockfile: true` はこの検証を省くので、設定しない。

## Renovate との関係

- Renovate の `minimumReleaseAge` とパッケージマネージャーの設定は別々に働く。Renovate の公式文書は両方に設定することを勧めている。
- パッケージマネージャーの待機期間を Renovate より短くすれば、通常の更新ではぶつからない。
- 脆弱性の修正は Renovate の待機の対象外（`vulnerabilityAlerts` の `minimumReleaseAge` が `null`）だが、
  パッケージマネージャーの待機はそのまま効く。待機期間より新しい修正版への更新は、lockfile の生成で失敗する。
  急ぐ場合は、該当のパッケージだけを除外する（npm は `min-release-age-exclude`、pnpm は `minimumReleaseAgeExclude`、
  uv は `exclude-newer-package`）。除外は修正版を取り込んだ後に取り除く。
- Renovate は uv の `exclude-newer` を読まず、uv にも待機期間を渡さない。

## uv の sdist のビルド

uv は wheel のない依存を sdist からビルドし、そのとき依存のコードを実行する。
`[tool.uv]` の `no-build = true` または `no-build-package` でビルドを禁止できる。
wheel を配布していない依存があると導入できなくなるため、`check` では判定しない。
依存がすべて wheel で配布されている利用先では、設定を検討する。
