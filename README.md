# task-scheduler

`task-scheduler` は、タスク、依存関係、担当条件、担当者が作業を始められる日から、実行可能な担当と日程の計画を作る CLI ツールである。

実装方法を変えても守るべき目的と条件は、[requirements.md](requirements.md) で定める。入力形式、検証規則、割当アルゴリズムなど、現在の厳密な挙動は `internal/` 配下のコードとテストで確認する。この README では、現在の操作方法を説明する。

実際のスケジュールデータは公開しない。利用時はPrivateな保存元から `epics/<EPIC-ID>/schedule.yaml` をローカルへコピーする。`/epics/` はGitの管理対象から除外しているため、コピーしたデータはこのリポジトリへコミットされない。公開されるテストデータは、実在するEpicを表さない `testdata/generic-epic/` だけである。

## 実行方法

リポジトリのルートで、次の形式で実行する。

```bash
go run ./cmd/task-scheduler <command> [flags]
```

## 入力を検証する

`check` は入力ファイルを検証する。計画や出力ファイルは生成しない。

```bash
go run ./cmd/task-scheduler check \
  --input epics/<EPIC-ID>/schedule.yaml
```

## 計画の要約を出力する

`summary` は、計画結果の要約を JSON 形式で標準出力へ書き出す。

```bash
go run ./cmd/task-scheduler summary \
  --format json \
  --input epics/<EPIC-ID>/schedule.yaml
```

## 計画をファイルへ出力する

`render` は、計画結果をHTML形式でファイルへ書き出す。

```bash
go run ./cmd/task-scheduler render \
  --format html \
  --input epics/<EPIC-ID>/schedule.yaml \
  --output gantt.html
```

## 検証済みの候補を反映する

`apply-candidate` は、検証済みの候補ファイルを計画ファイルへ反映する。対象の Git リポジトリ内で実行する。

既存の計画を更新する場合は、候補を作った時点の計画ファイルの SHA-256 を指定する。

```bash
go run ./cmd/task-scheduler apply-candidate \
  --target epics/<EPIC-ID>/schedule.yaml \
  --candidate epics/<EPIC-ID>/.schedule.yaml.candidate-... \
  --expected-sha256 <変更前のSHA-256>
```

新しい計画ファイルを作る場合は、`--expected-sha256` の代わりに `--expect-absent` を指定する。

```bash
go run ./cmd/task-scheduler apply-candidate \
  --target epics/<EPIC-ID>/schedule.yaml \
  --candidate epics/<EPIC-ID>/.schedule.yaml.candidate-... \
  --expect-absent
```

既存の計画時点である `settings.as_of` を変更する場合は、`--allow-as-of-change` も指定する。

## 計画作成コマンドの共通オプション

`check`、`summary`、`render` では、次のオプションを使用できる。

- `--input PATH` は、入力する YAML ファイルを指定する。この指定は必須である。
- `--available WORKER=YYYY-MM-DD` は、担当者が作業を始められる日を上書きする。複数回指定できる。
- `--as-of YYYY-MM-DD` は、計画時点を上書きする。

## Epic の計画を更新する

`epics/<EPIC-ID>/schedule.yaml` を新規作成または更新する手順は、[Epic スケジュール入力スキル](.agents/skills/epic-schedule/SKILL.md)で説明する。
