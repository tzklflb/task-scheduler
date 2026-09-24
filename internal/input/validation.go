package input

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"time"
)

func validateSemantics(value Input) []Issue {
	var issues []Issue
	add := func(location, message string) { issues = append(issues, Issue{location, message}) }
	if len(value.Tasks) == 0 {
		add("tasks", "タスクが 1 件もない")
	}
	if value.Settings.BufferDays < 0 {
		add("settings.buffer_days", "0 以上の整数で指定する")
	}
	if value.Settings.SprintLengthDays <= 0 {
		add("settings.sprint_length_days", "正の整数で指定する")
	}
	if value.Settings.BufferDays > 0 && len(value.Settings.BufferAllocation) == 0 {
		add("settings.buffer_allocation", "buffer_days が 1 以上なら必要")
	}
	allocationTotal := 0
	allocationKeys := make([]string, 0, len(value.Settings.BufferAllocation))
	for _, key := range sortedKeys(value.Settings.BufferAllocation) {
		days := value.Settings.BufferAllocation[key]
		allocationTotal += days
		if days < 0 {
			add("settings.buffer_allocation."+key, "0 以上の整数で指定する")
		}
		if key != "tail" {
			allocationKeys = append(allocationKeys, key)
		}
	}
	if len(value.Settings.BufferAllocation) > 0 && allocationTotal != value.Settings.BufferDays {
		add("settings.buffer_allocation", "合計が buffer_days と一致しない")
	}
	validateAllocationKeys(value.Settings, allocationKeys, add)

	holidays := DateSet(value.Settings.Holidays)
	for _, day := range value.Settings.HalfDays {
		if holidays[day.Format(dateLayout)] {
			add("settings.holidays", "holidays と half_days の両方に指定された日がある")
			break
		}
	}
	selection := value.Settings.Selection
	if selection.PreferredWorker.DefaultThresholdDays < 0 {
		add("settings.selection.preferred_worker.default_threshold_days", "0 以上で指定する")
	}

	workers := map[string]Worker{}
	lanes := map[string]bool{}
	for _, worker := range value.Workers {
		if _, exists := workers[worker.ID]; exists {
			add("workers", fmt.Sprintf("worker ID %q が重複している", worker.ID))
		}
		workers[worker.ID] = worker
		for _, lane := range worker.Lanes {
			lanes[lane] = true
		}
	}
	tasks := map[string]Task{}
	plans := map[string]bool{}
	taskLanes := map[string]bool{}
	for _, task := range value.Tasks {
		if _, exists := tasks[task.ID]; exists {
			add("tasks", fmt.Sprintf("タスク ID %q が重複している", task.ID))
		}
		tasks[task.ID] = task
		plans[task.Plan] = true
		taskLanes[task.Lane] = true
	}
	for _, plan := range sortedKeys(selection.PreferredWorker.PlanAssignees) {
		workerID := selection.PreferredWorker.PlanAssignees[plan]
		if !plans[plan] {
			add("settings.selection.preferred_worker.plan_assignees."+plan, "該当するタスクがない")
		}
		if _, exists := workers[workerID]; !exists {
			add("settings.selection.preferred_worker.plan_assignees."+plan, fmt.Sprintf("worker %q が未定義", workerID))
		}
	}
	for _, plan := range sortedKeys(value.Settings.PlanWorkers) {
		allowed := value.Settings.PlanWorkers[plan]
		if !plans[plan] {
			add("settings.plan_workers."+plan, "該当するタスクがない")
		}
		if len(allowed) == 0 {
			add("settings.plan_workers."+plan, "担当候補が空")
		}
		for _, workerID := range allowed {
			if _, exists := workers[workerID]; !exists {
				add("settings.plan_workers."+plan, fmt.Sprintf("worker %q が未定義", workerID))
			}
		}
		if preferred := selection.PreferredWorker.PlanAssignees[plan]; preferred != "" && !slices.Contains(allowed, preferred) {
			add("settings.selection.preferred_worker.plan_assignees."+plan, "優先担当が担当候補に含まれていない")
		}
	}
	for _, plan := range sortedKeys(selection.PreferredWorker.PlanThresholdDays) {
		threshold := selection.PreferredWorker.PlanThresholdDays[plan]
		if !plans[plan] {
			add("settings.selection.preferred_worker.plan_threshold_days."+plan, "該当するタスクがない")
		}
		if threshold < 0 {
			add("settings.selection.preferred_worker.plan_threshold_days."+plan, "負の値を指定できない")
		}
	}
	validateSelectionScope(selection.FollowContinuity.Scope, "settings.selection.follow_continuity.scope", tasks, plans, taskLanes, add)
	validateSelectionScope(selection.PreferredWorker.Scope, "settings.selection.preferred_worker.scope", tasks, plans, taskLanes, add)
	validateWaves(value.Settings.Waves, tasks, plans, add)
	for index := 1; index < len(value.Settings.SprintGoals); index++ {
		if value.Settings.SprintGoals[index].End.Before(value.Settings.SprintGoals[index-1].End) {
			add("settings.sprint_goals", "end は昇順で指定する")
			break
		}
	}

	hasStatus := false
	for _, task := range value.Tasks {
		location := "tasks." + task.ID
		if task.StatusSpecified {
			hasStatus = true
		}
		if task.EffortSlots <= 0 {
			add(location+".effort", "0 より大きく指定する")
		}
		if !lanes[task.Lane] {
			add(location+".lane", fmt.Sprintf("lane %q を担当できる worker がいない", task.Lane))
		}
		if task.Assignee != "" {
			worker, exists := workers[task.Assignee]
			if !exists {
				add(location+".assignee", fmt.Sprintf("worker %q が未定義", task.Assignee))
			} else if !slices.Contains(worker.Lanes, task.Lane) {
				add(location+".assignee", "担当者が lane を担当できない")
			}
		} else if allowed := value.Settings.PlanWorkers[task.Plan]; len(allowed) > 0 && !hasWorkerForLane(allowed, workers, task.Lane) {
			add(location+".lane", "plan_workers の担当候補に lane を担当できる worker がいない")
		}
		for _, dependency := range task.Dependencies {
			dependencyTask, exists := tasks[dependency]
			if !exists {
				add(location+".deps", fmt.Sprintf("依存 %q が未定義", dependency))
				continue
			}
			if task.ReleaseBlocking && !dependencyTask.ReleaseBlocking {
				add(location+".deps", "リリース必須タスクがスケジュール外タスクに依存している")
			}
			if task.Core && !dependencyTask.Core {
				add(location+".deps", "コア対象タスクはコア外タスクに依存できない")
			}
		}
		if task.Follow != "" {
			if _, exists := tasks[task.Follow]; !exists {
				add(location+".follow", fmt.Sprintf("参照先 %q が未定義", task.Follow))
			}
		}
		if task.EndOn != nil && task.StartOn == nil {
			add(location+".end_on", "start_on とあわせて指定する")
		}
		if task.StartOn != nil {
			if task.EndOn != nil && task.EndOn.Before(*task.StartOn) {
				add(location+".end_on", "start_on より前にできない")
			}
			if task.Assignee == "" {
				add(location+".assignee", "開始日固定タスクには必要")
			}
			if len(task.Dependencies) > 0 {
				add(location+".deps", "開始日固定タスクには指定できない")
			}
			if !task.Core {
				add(location+".core", "開始日固定タスクと併用できない")
			}
		}
		switch task.Status {
		case "done", "in_progress":
		case "":
			if task.StatusSpecified {
				add(location+".status", "done または in_progress で指定する")
			}
		default:
			add(location+".status", "done または in_progress で指定する")
		}
		if task.Status == "in_progress" && task.Assignee == "" {
			add(location+".assignee", "着手中タスクには必要")
		}
		if task.Status == "done" {
			if task.DoneOn == nil {
				add(location+".done_on", "完了タスクには必要")
			}
			if task.Assignee == "" {
				add(location+".assignee", "完了タスクには必要")
			}
		} else if task.DoneOn != nil {
			add(location+".done_on", "完了タスクにだけ指定する")
		}
	}
	if hasStatus && value.Settings.AsOf == nil {
		add("settings.as_of", "status がある場合は必要")
	}
	validateFixedWindows(value.Tasks, holidays, DateSet(value.Settings.HalfDays), add)
	if !hasDependencyCycle(value.Tasks, EffectiveDependencies(value.Tasks, value.Settings.Waves)) {
		return issues
	}
	add("tasks", "依存に循環がある")
	return issues
}

func validateFixedWindows(tasks []Task, holidays, halfDays map[string]bool, add func(string, string)) {
	type window struct {
		taskID   string
		assignee string
		start    time.Time
		end      time.Time
	}
	var windows []window
	for _, task := range tasks {
		if task.StartOn == nil || task.Assignee == "" {
			continue
		}
		end := FixedTaskEndDate(task, holidays, halfDays)
		windows = append(windows, window{task.ID, task.Assignee, *task.StartOn, end})
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].assignee != windows[j].assignee {
			return windows[i].assignee < windows[j].assignee
		}
		if !windows[i].start.Equal(windows[j].start) {
			return windows[i].start.Before(windows[j].start)
		}
		if !windows[i].end.Equal(windows[j].end) {
			return windows[i].end.Before(windows[j].end)
		}
		return windows[i].taskID < windows[j].taskID
	})
	for index, current := range windows {
		for previous := 0; previous < index; previous++ {
			other := windows[previous]
			if other.assignee != current.assignee {
				continue
			}
			if !other.end.Before(current.start) {
				add("tasks."+current.taskID+".start_on", fmt.Sprintf("worker %q の固定期間が tasks.%s と重複している", current.assignee, other.taskID))
			}
		}
	}
}

func validateSelectionScope(scope SelectionScope, location string, tasks map[string]Task, plans, taskLanes map[string]bool, add func(string, string)) {
	for _, id := range scope.TaskIDs {
		if _, exists := tasks[id]; !exists {
			add(location+".task_ids", fmt.Sprintf("task ID %q が未定義", id))
		}
	}
	for _, plan := range scope.Plans {
		if !plans[plan] {
			add(location+".plans", fmt.Sprintf("plan %q が未定義", plan))
		}
	}
	for _, lane := range scope.Lanes {
		if !taskLanes[lane] {
			add(location+".lanes", fmt.Sprintf("lane %q が未定義", lane))
		}
	}
}

func validateAllocationKeys(settings Settings, keys []string, add func(string, string)) {
	if len(keys) == 0 {
		return
	}
	isSprint := func(key string) bool {
		if len(key) < 2 || key[0] != 'S' || key[1] == '0' {
			return false
		}
		_, err := strconv.Atoi(key[1:])
		return err == nil
	}
	sprintCount := 0
	for _, key := range keys {
		if isSprint(key) {
			sprintCount++
		}
	}
	if sprintCount > 0 && sprintCount != len(keys) {
		add("settings.buffer_allocation", "月とスプリントの指定を混在できない")
		return
	}
	if sprintCount > 0 {
		if len(settings.SprintGoals) == 0 {
			add("settings.buffer_allocation", "スプリント指定には sprint_goals が必要")
			return
		}
		for _, key := range keys {
			number, _ := strconv.Atoi(key[1:])
			if number < 1 || number > len(settings.SprintGoals) {
				add("settings.buffer_allocation."+key, "sprint_goals の範囲にない")
			}
		}
		return
	}
	for _, key := range keys {
		if _, err := time.Parse("2006-01", key); err != nil || len(key) != 7 {
			add("settings.buffer_allocation."+key, "YYYY-MM で指定する")
		}
	}
}

func validateWaves(waves map[string][][]string, tasks map[string]Task, plans map[string]bool, add func(string, string)) {
	for _, plan := range sortedKeys(waves) {
		planWaves := waves[plan]
		location := "settings.waves." + plan
		if !plans[plan] {
			add(location, "該当するタスクがない")
		}
		seen := map[string]bool{}
		for _, wave := range planWaves {
			for _, taskID := range wave {
				task, exists := tasks[taskID]
				if !exists {
					add(location, fmt.Sprintf("タスク %q が未定義", taskID))
				} else if task.Plan != plan {
					add(location, fmt.Sprintf("タスク %q がこの計画に属さない", taskID))
				}
				if seen[taskID] {
					add(location, fmt.Sprintf("タスク %q が複数の波に重複している", taskID))
				}
				seen[taskID] = true
			}
		}
	}
}

func hasDependencyCycle(tasks []Task, dependencies map[string][]string) bool {
	byID := map[string]Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		_, exists := byID[id]
		if !exists {
			return false
		}
		visiting[id] = true
		for _, dependency := range dependencies[id] {
			if visit(dependency) {
				return true
			}
		}
		delete(visiting, id)
		visited[id] = true
		return false
	}
	for _, task := range tasks {
		if visit(task.ID) {
			return true
		}
	}
	return false
}

func hasWorkerForLane(ids []string, workers map[string]Worker, lane string) bool {
	for _, id := range ids {
		if worker, exists := workers[id]; exists && slices.Contains(worker.Lanes, lane) {
			return true
		}
	}
	return false
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
