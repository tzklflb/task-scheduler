// input パッケージは、スケジューラが受け付ける制限付きYAML入力を読み込み検証する。
package input

import "time"

const dateLayout = "2006-01-02"

// Input は、後続のスケジューリング処理へ渡す検証済みの型付きデータである。
type Input struct {
	Epic     Epic
	Settings Settings
	Workers  []Worker
	Tasks    []Task
}

type Epic struct {
	ID   string
	Name string
}

type Settings struct {
	StartDate        time.Time
	BufferDays       int
	Holidays         []time.Time
	HalfDays         []time.Time
	BufferAllocation map[string]int
	SprintGoals      []SprintGoal
	Assumptions      []string
	Selection        SelectionSettings
	// PlanWorkersは、planごとに担当候補を絞り込む。担当未指定タスクにのみ適用し、
	// タスクに固定担当（Task.Assignee）がある場合はそちらを優先する。
	PlanWorkers map[string][]string
	Waves       map[string][][]string
	// AsOfは計画の基準日を表す。基準日以前に完了した実績はそのまま反映し、
	// 担当者の空き時刻は基準日を下回らない。
	AsOf *time.Time
	// ReleaseDateとSprintAnchorは出力の表示にのみ使い、割当計算には使わない。
	ReleaseDate  *time.Time
	SprintAnchor *time.Time
	// CoreEndMinは、計算結果のコア完了日がこれより前になる場合の下限日である。
	CoreEndMin       *time.Time
	SprintLengthDays int
}

// SelectionSettingsは、固定された選択Ruleの有効化、対象、値を持つ。
type SelectionSettings struct {
	DeferNonCore     DeferNonCoreSettings
	FollowContinuity FollowContinuitySettings
	PreferredWorker  PreferredWorkerSettings
}

type DeferNonCoreSettings struct{ Enabled bool }

type FollowContinuitySettings struct {
	Enabled bool
	Scope   SelectionScope
}

type PreferredWorkerSettings struct {
	DefaultThresholdDays float64
	PlanAssignees        map[string]string
	PlanThresholdDays    map[string]float64
	Scope                SelectionScope
}

// SelectionScopeはplan、lane、task IDの積集合で対象を表す。
type SelectionScope struct {
	Plans   []string
	Lanes   []string
	TaskIDs []string
}

// Matchesは、空のscopeを全対象として軸間AND・軸内ORで照合する。
func (scope SelectionScope) Matches(taskID, plan, lane string) bool {
	return (len(scope.Plans) == 0 || contains(scope.Plans, plan)) &&
		(len(scope.Lanes) == 0 || contains(scope.Lanes, lane)) &&
		(len(scope.TaskIDs) == 0 || contains(scope.TaskIDs, taskID))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type SprintGoal struct {
	End  time.Time
	Goal string
}

type Worker struct {
	ID            string
	Lanes         []string
	AvailableFrom *time.Time
}

type Task struct {
	ID           string
	Name         string
	Plan         string
	EffortSlots  int
	Dependencies []string
	Lane         string
	Assignee     string
	// Followは、直前に完了してほしいタスクのIDである。settings.selection.follow_continuity.enabledが
	// trueのとき、そのタスクを担当した人が間を空けず継続できる候補の判定に使う。
	Follow    string
	StartOn   *time.Time
	EndOn     *time.Time
	NotBefore *time.Time
	Status    string
	// StatusSpecifiedは、statusがYAMLで明示されたかどうかを区別する。
	// 空文字列の未指定と"done"/"in_progress"以外の不正値を分けるために持つ。
	StatusSpecified bool
	DoneOn          *time.Time
	// Coreがtrueの間は、他のCoreタスクが未配置である限り非Coreタスクを配置候補にしない。
	// 割当対象の非固定タスクに限り、CoreEndの集計対象にもなる（固定日程は対象外）。
	Core bool
	// ReleaseBlockingがfalseだと、このタスクは割当計算の対象から除外される
	// （スケジュール外タスクとして扱う）。ただし依存関係を通じてcriticalLengthsの
	// 計算には含まれるため、上流タスクの選択優先度に影響することがある。
	ReleaseBlocking bool
	// QABlockingがfalseだと、CompletionWithBuffer（開発完了・バッファ込み）の
	// 集計対象から除外する。
	QABlocking bool
}

// Overrides は、意味検証の前に適用するコマンドライン由来の値である。
type Overrides struct {
	Available []string
	AsOf      string
}

// Issue は、入力に含まれる問題を一件表す。
type Issue struct {
	Location string
	Message  string
}

func (issue Issue) String() string {
	if issue.Location == "" {
		return issue.Message
	}
	return issue.Location + ": " + issue.Message
}
