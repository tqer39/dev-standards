# TypeScript の型チェックに tsrs を使う

<!-- cspell:words tsrs musl -->

TypeScript を使うリポジトリでは、型チェックに [tsrs](https://github.com/oxc-project/tsrs) を使う。
tsrs は TypeScript 7 の型チェッカーを Rust に移植したもので、npm パッケージ `tsrs` として配布されている。
型チェックだけを置き換え、アプリの実装言語、ビルド、実行時の依存は変えない。
TypeScript のソースと `tsconfig.json` の両方を持つリポジトリを対象とし、どちらかがないリポジトリは対象外とする。

公式には今も実験版なので、安定版になるまでは次の初期値で使う。
利用先で扱いを変える場合は、理由と確認した内容を利用先の文書に残す。

## 版の固定と実行

- tsrs は mise の npm ツールとして版を固定し、mise のタスクとして実行する。
  `latest` や範囲指定は使わない。
- タスクだけにツールを指定し、プロジェクト全体の `[tools]` や `package.json` の依存には追加しない。
  `npm ci`、ビルド、既存の型チェックが使う TypeScript の版を変えないためである。
- 共通の mise 設定を生成している利用先は、`.standards/mise.project.toml` などの正本を編集し、
  変更計画を確認して再生成する。

```toml
[tasks."typecheck:tsrs"]
description = "Experimental Rust TypeScript check; tsc remains required"
tools = { "npm:tsrs" = "0.18.0-ts7.1.0-dev.20260929" }
run = "tsrs --noEmit"
```

版は例であり、利用先で確認した版を固定する。
`tsconfig.json` がリポジトリの直下にない場合は、タスクの `dir` で対象のディレクトリを指定する。
ローカルで型を確かめるときは、まず `mise run typecheck:tsrs` を実行する。

## tsc との併用

tsrs が安定版になるまでは、tsc による型チェックを必須の検査として残す。

- 既存の `tsc --noEmit` などの型チェックと、それを呼ぶ lint・test・build のタスクを削除・置換しない。
- tsrs のタスクは追加の検査とし、`lint`、`test`、`build` など CI の必須チェックが呼ぶタスクには接続しない。
- 両者の結果が食い違ったら tsc の結果を正とし、差分を既知の互換性の問題と照合する。
  検査を無効にして tsrs だけを通さない。
- tsc を外して tsrs だけにするのは、tsrs が安定版になり、利用先の全プラットフォームで動作を確認してからとする。

## バイナリがないプラットフォーム

tsrs はプラットフォームごとのバイナリを npm の `optionalDependencies` で配布し、対象は版によって異なる。
`0.18.0-ts7.1.0-dev.20260929` は macOS arm64 と Linux x64／arm64（GNU／musl）だけを配布し、
Windows と macOS x64 向けのバイナリはない。
固定した版にバイナリがないプラットフォームでは、tsrs を使わず tsc の型チェックを使う。

dev-standards が生成する CI は `windows-latest` でも必須チェックを実行する。
tsrs のタスクを必須チェックが呼ぶタスクに接続すると、バイナリがないランナーで必須チェックが失敗する。
CI で tsrs を実行する場合は、バイナリがあるランナーに限定した別のジョブにし、必須チェックの条件に加えない。
必須チェックの対象 OS を減らして tsrs に合わせない。

## 版の更新

版を更新するときは、固定する前に次を確認する。

- 配布元: npm の `tsrs` が公式リポジトリ `oxc-project/tsrs` から公開された版であること。
  レジストリ署名と attestation を検証できる場合は検証する。
- 対応プラットフォーム: 新しい版の `optionalDependencies`（`npm view tsrs@<版> optionalDependencies`）。
  対象が変わったら、tsc に切り替えるプラットフォームの記述も更新する。
- 互換性: [既知の互換性の制約](https://github.com/oxc-project/tsrs/blob/main/docs/COMPATIBILITY.md)と
  リリースノート。対応する TypeScript の版が変わる場合は、tsc との診断の差分も確認する。
- 動作: 更新後に tsrs と tsc の両方で型チェックを実行し、結果を比べる。

mise は既定で npm のインストール時スクリプトを抑止する。この設定を tsrs のために緩めない。

## 配布と検証の範囲

共通指示の配布を有効にした利用先では、このルールを `.standards/rules.md` に配布する。
既存利用先には、採用版の更新計画を確認・適用してから届く。
この配布だけでは、利用先に tsrs のタスクは追加されない。タスクの追加と版の固定は利用先で行う。
dev-standards は生成する CI と mise のタスクを、このルールでは変更しない。
`check` と `verify` は tsrs の導入と実行結果を判定せず、`review_required` と報告する。

## 一次情報

- [tsrs の公式リポジトリ](https://github.com/oxc-project/tsrs)
- [既知の互換性の制約](https://github.com/oxc-project/tsrs/blob/main/docs/COMPATIBILITY.md)
- [mise の npm バックエンド](https://mise.jdx.dev/dev-tools/backends/npm.html)
