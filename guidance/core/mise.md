# タスクの入口を mise に集約する

mise に Go の完全な版と、bootstrap、lint、format、test、build、共通検査の入口を
集約します。共通の予約タスク名を上書きせず、ローカルと CI で同じ入口を使います。

固有のツール設定は `.standards/mise.project.toml`、固有タスクは
`.standards/tasks.project.toml` に追加します。生成した `mise.toml` と共通タスクは
直接編集せず、計画を作り直して適用します。
