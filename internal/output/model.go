// output パッケージは、input.Input と scheduling.Schedule から共通のViewを作り、
// JSON要約とHTMLへ変換する。
package output

import (
	"slices"
	"sort"
	"time"

	"github.com/tzklflb/task-scheduler/internal/input"
	"github.com/tzklflb/task-scheduler/internal/scheduling"
)

const dateLayout = "2006-01-02"

// View は、JSON要約とHTMLが共有する正規化済みデータである。
type View struct {
	EpicID               string
	EpicName             string
	Title                string
	Start                string
	AsOf                 *string
	ReleaseDate          *string
	SprintAnchor         *string
	SprintLengthDays     int
	SprintGoals          []SprintGoal
	Holidays             []string
	HalfDays             []string
	Assumptions          []string
	CoreEnd              string
	CompletionWithBuffer string
	RemainingEffort      float64
	BufferDates          []string
	Workers              []string
	Plans                []string
	Tasks                []Task
	ExcludedTasks        []Task
}

// SprintGoal は、Viewが持つ名前付きスプリント期間を一件表す。
type SprintGoal struct {
	Number int
	Start  string
	End    string
	Goal   string
}

// Task は、Viewが持つタスク一件を表す。予定・実績・固定期間・スケジュール外の
// いずれかであり、区別はSourceフィールドで判定する。
type Task struct {
	ID                string
	Name              string
	Plan              string
	Assignee          string
	Start             *string
	End               *string
	Status            *string
	DoneOn            *string
	Effort            float64
	Dependencies      []string
	Source            string
	DeliveryScope     DeliveryScope
	StartsInAfternoon bool
	EndsAtNoon        bool
}

// DeliveryScope は、タスクがリリースに対していつまでに必要かを表す。
// input.Task の ReleaseBlocking と Core から導出する（deliveryScope参照）。
type DeliveryScope string

const (
	DeliveryScopeCore            DeliveryScope = "core"
	DeliveryScopeReleaseRequired DeliveryScope = "release_required"
	DeliveryScopePostRelease     DeliveryScope = "post_release"
)

// Build は、検証済み入力と計算済みスケジュールを一度だけ出力用に正規化する。
func Build(value input.Input, result scheduling.Schedule) View {
	assignments := make(map[string]scheduling.Assignment, len(result.Assignments))
	for _, assignment := range result.Assignments {
		assignments[assignment.ID] = assignment
	}

	view := View{
		EpicID:               value.Epic.ID,
		EpicName:             value.Epic.Name,
		Title:                value.Epic.ID + " " + value.Epic.Name + " 開発タイムライン",
		Start:                value.Settings.StartDate.Format(dateLayout),
		SprintGoals:          []SprintGoal{},
		Holidays:             []string{},
		HalfDays:             []string{},
		Assumptions:          append([]string{}, value.Settings.Assumptions...),
		CoreEnd:              result.CoreEnd.Format(dateLayout),
		CompletionWithBuffer: result.CompletionWithBuffer.Format(dateLayout),
		RemainingEffort:      result.RemainingEffort,
		BufferDates:          []string{},
		Workers:              []string{},
		Plans:                []string{},
		Tasks:                []Task{},
		ExcludedTasks:        []Task{},
	}
	if value.Settings.AsOf != nil {
		view.AsOf = datePointer(*value.Settings.AsOf)
	}
	if value.Settings.ReleaseDate != nil {
		view.ReleaseDate = datePointer(*value.Settings.ReleaseDate)
	}
	if value.Settings.SprintAnchor != nil {
		view.SprintAnchor = datePointer(*value.Settings.SprintAnchor)
	}
	view.SprintLengthDays = value.Settings.SprintLengthDays
	for _, date := range value.Settings.Holidays {
		view.Holidays = append(view.Holidays, date.Format(dateLayout))
	}
	for _, date := range value.Settings.HalfDays {
		view.HalfDays = append(view.HalfDays, date.Format(dateLayout))
	}
	for _, date := range result.BufferDates {
		view.BufferDates = append(view.BufferDates, date.Format(dateLayout))
	}
	for _, worker := range value.Workers {
		view.Workers = append(view.Workers, worker.ID)
	}
	sprintStart := value.Settings.StartDate
	for number, goal := range value.Settings.SprintGoals {
		view.SprintGoals = append(view.SprintGoals, SprintGoal{
			Number: number + 1,
			Start:  sprintStart.Format(dateLayout),
			End:    goal.End.Format(dateLayout),
			Goal:   goal.Goal,
		})
		sprintStart = goal.End.AddDate(0, 0, 1)
	}

	for _, inputTask := range value.Tasks {
		assignment, scheduled := assignments[inputTask.ID]
		if !scheduled && inputTask.Status == "done" {
			continue
		}
		task := taskView(inputTask, assignment, scheduled)
		if scheduled {
			view.Tasks = append(view.Tasks, task)
			if !slices.Contains(view.Plans, task.Plan) {
				view.Plans = append(view.Plans, task.Plan)
			}
		} else {
			view.ExcludedTasks = append(view.ExcludedTasks, task)
		}
	}
	sort.Slice(view.Tasks, func(i, j int) bool { return view.Tasks[i].ID < view.Tasks[j].ID })
	sort.Slice(view.ExcludedTasks, func(i, j int) bool { return view.ExcludedTasks[i].ID < view.ExcludedTasks[j].ID })
	return view
}

func taskView(inputTask input.Task, assignment scheduling.Assignment, scheduled bool) Task {
	task := Task{
		ID:            inputTask.ID,
		Name:          inputTask.Name,
		Plan:          inputTask.Plan,
		Assignee:      inputTask.Assignee,
		Effort:        float64(inputTask.EffortSlots) / 2,
		Dependencies:  append([]string{}, inputTask.Dependencies...),
		Source:        "excluded",
		DeliveryScope: deliveryScope(inputTask),
	}
	if inputTask.DoneOn != nil {
		task.DoneOn = datePointer(*inputTask.DoneOn)
	}
	if !scheduled {
		if inputTask.Status != "" {
			task.Status = stringPointer(inputTask.Status)
		}
		return task
	}
	task.Assignee = assignment.Assignee
	task.Start = datePointer(assignment.Start)
	task.End = datePointer(assignment.End)
	task.StartsInAfternoon = assignment.StartsInAfternoon
	task.EndsAtNoon = assignment.EndsAtNoon
	if assignment.Status != "" {
		task.Status = stringPointer(assignment.Status)
	}
	switch {
	case assignment.Status == "done" && assignment.EndSlot == 0:
		task.Source = "actual"
	case assignment.Fixed:
		task.Source = "fixed"
	default:
		task.Source = "planned"
	}
	return task
}

func deliveryScope(task input.Task) DeliveryScope {
	if !task.ReleaseBlocking {
		return DeliveryScopePostRelease
	}
	if task.Core {
		return DeliveryScopeCore
	}
	return DeliveryScopeReleaseRequired
}

func datePointer(value time.Time) *string {
	formatted := value.Format(dateLayout)
	return &formatted
}

func stringPointer(value string) *string { return &value }
