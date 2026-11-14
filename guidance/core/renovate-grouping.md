# Renovate の更新を同じカテゴリでまとめる

Renovate の PR ごとに CI が実行されるので、同じカテゴリの依存はなるべく１つの PR にまとめる。
GitHub Actions、テストツール、lint と format のツール、同じ製品のパッケージと型定義などを単位にする。

```json
{
  "packageRules": [
    {
      "groupName": "GitHub Actions",
      "matchManagers": ["github-actions"],
      "matchUpdateTypes": ["minor", "patch"]
    }
  ]
}
```

まとめるのは minor と patch にし、影響の大きい major は別の PR に分ける。
無関係な依存まで１つにまとめない。１件の失敗で PR 全体が止まり、原因の切り分けも難しくなる。
脆弱性の修正は、まとめる対象の更新を待たずに適用する。

複数のリポジトリに共通するまとめ方は共有の Renovate 設定に置き、固有の依存だけを各リポジトリの設定に書く。
`check` はこの内容を判定せず、`review_required` と報告する。
