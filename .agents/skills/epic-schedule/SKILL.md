---
name: epic-schedule
description: Epic の schedule.yaml を新規作成または安全に更新し、検証済みのスケジュール差分を確認するときに使う。
---

# Epic スケジュール入力

このスキルが扱うのは、リポジトリ直下の `epics/<EPIC-ID>/schedule.yaml` だけである。

実装方法を変えても守るべき条件は、[基本契約](../../../requirements.md)で定める。フィールドの意味と計算規則は `internal/` 配下のコードとテスト、操作方法は [README](../../../README.md) と利用者向けの Go CLI を正本とする。このスキルには、入力仕様や計算規則を補完または複製しない。

作成と更新は同じ候補ファイルのフローで扱う。コミット、push、PR、外部システムの更新は行わない。

## 変更に必要な情報

依頼から、操作、Epic ID、対象タスク、変更値を分けて整理する。タスク名、ID、依存、工数、lane、担当者、日付、計画に影響する設定は推測しない。結果に影響する情報が不足するときは、確定している情報と未確定の情報を示して確認する。

ステータス、完了日、担当者などの記録だけを更新する場合は、既存の `settings.as_of` をそのまま保持する。利用者が再計画を明示し、基準日も指定した場合だけ `as_of` を変更する。再計画の依頼に基準日がない場合は確認する。

`as_of` が未設定のEpicへは、現在の入力検証規則上、ステータスを記録できない。この場合は、基準日の初回設定によって再計画が発生することを説明し、再計画するかと基準日を確認する。ステータス更新として暗黙に `as_of` を追加しない。

スキル自体の動作確認には、`testdata/generic-epic/schedule.yaml` またはその一時コピーを使う。これはテスト専用であり、実在のEpicとして扱わない。

## 候補から反映まで

1. 既存Epicでは、対象ファイルを読み、ハッシュと `go run ./cmd/task-scheduler summary --format json --input <対象>` の出力を保存する。新規Epicでは、同じIDの対象ファイルが存在しないことを確認する。
2. 対象と同じディレクトリに `.schedule.yaml.candidate-` で始まる候補ファイルを作る。既存ファイルを直接編集しない。新規作成でも候補を先に作る。
3. 候補に対して `go run ./cmd/task-scheduler check --input <candidate>` を実行する。成功した候補だけ、`go run ./cmd/task-scheduler summary --format json --input <candidate>` で担当、期間、コア完了日、バッファ込み完了日、残工数を取得する。
4. 既存Epicでは、変更前後の要約と入力差分を利用者に示す。新規Epicでは、候補の要約を示す。反映の了承を得てから次へ進む。
5. `go run ./cmd/task-scheduler apply-candidate` で候補を反映する。既存Epicには保存したSHA-256を `--expected-sha256` で渡す。再計画と新しい基準日を利用者が明示した既存更新だけ、`--allow-as-of-change` を併用する。新規Epicには `--expect-absent` を渡し、このフラグは渡さない。このコマンドは候補の `epic.id`、基準日、公開CLIと同じ入力検証、競合を確認した後に同一ディレクトリ内で原子的に置換するため、検証失敗や対象の予期しない更新時に元ファイルを変更しない。
6. 反映後は `go run ./cmd/task-scheduler check --input <対象>` を実行し、次のようにHTMLを一時ディレクトリへ生成する。反映前の検証または要約に失敗した候補は、対象へ反映せず修正するか破棄する。

   ```bash
   go run ./cmd/task-scheduler render --format html \
     --input epics/<EPIC-ID>/schedule.yaml \
     --output <一時ディレクトリ>/gantt.html
   ```

候補の作成、検証、要約、反映にはリポジトリルートから次のコマンドを使う。

```bash
go run ./cmd/task-scheduler check --input epics/<EPIC-ID>/schedule.yaml
go run ./cmd/task-scheduler summary --format json --input epics/<EPIC-ID>/schedule.yaml
go run ./cmd/task-scheduler apply-candidate \
  --target epics/<EPIC-ID>/schedule.yaml \
  --candidate epics/<EPIC-ID>/.schedule.yaml.candidate-... \
  --expected-sha256 <変更前のSHA-256>
```

新規作成では最後の引数を `--expect-absent` に替える。反映後の生成物は一時ディレクトリに置き、リポジトリの生成物を上書きしない。
