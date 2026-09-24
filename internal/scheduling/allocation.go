package scheduling

import (
	"slices"

	"github.com/tzklflb/task-scheduler/internal/input"
)

// allocationState は、タスクの配置確定時に一体で更新する索引を保持する。
// 割当、担当者の次の空き、タスクの完了時刻、未配置かどうかは常に整合していなければならない。
type allocationState struct {
	assigned map[string]Assignment
	freeAt   map[string]int
	doneAt   map[string]int
	pending  map[string]bool
}

func newAllocationState(allWorkers []input.Worker, cal *calendar, asOfSlot int) *allocationState {
	freeAt := map[string]int{}
	for _, worker := range allWorkers {
		freeAt[worker.ID] = asOfSlot
		if worker.AvailableFrom != nil {
			if slot := cal.dateToSlot(*worker.AvailableFrom); slot > freeAt[worker.ID] {
				freeAt[worker.ID] = slot
			}
		}
	}
	return &allocationState{
		assigned: map[string]Assignment{},
		freeAt:   freeAt,
		doneAt:   map[string]int{},
		pending:  map[string]bool{},
	}
}

// 完了実績は基準日以前の記録なので、現在の担当者の空きには影響させない。
func (state *allocationState) recordActual(id string, assignment Assignment, doneAtSlot int) {
	state.assigned[id] = assignment
	state.doneAt[id] = doneAtSlot
}

// 固定日程が既存の空き時刻を越える場合だけ、担当者の空き時刻を延ばす。
func (state *allocationState) placeFixed(id, assignee string, assignment Assignment, end int) {
	if end > state.freeAt[assignee] {
		state.freeAt[assignee] = end
	}
	state.doneAt[id] = end
	state.assigned[id] = assignment
}

// 計算した配置を確定し、割当、担当者の空き、完了時刻、未配置状態を一体で更新する。
func (state *allocationState) confirm(id, assignee string, assignment Assignment, end int) {
	state.freeAt[assignee] = end
	state.doneAt[id] = end
	state.assigned[id] = assignment
	delete(state.pending, id)
}

func (state *allocationState) markPending(id string) {
	state.pending[id] = true
}

// 基準日までに判明している完了実績を反映し、残工数と反映済みのタスクを返す。
func reflectCompletedTasks(scheduled []input.Task, settings input.Settings, holidaySet, halfDaySet map[string]bool, asOfSlot int, state *allocationState) (int, map[string]bool) {
	remainingSlots := 0
	reflected := map[string]bool{}
	for _, task := range scheduled {
		if task.Status != "done" {
			remainingSlots += task.EffortSlots
		}
		if task.Status == "done" && task.DoneOn != nil && settings.AsOf != nil && !task.DoneOn.After(*settings.AsOf) {
			start, end, startsInAfternoon := pastSpan(*task.DoneOn, task.EffortSlots, holidaySet, halfDaySet)
			state.recordActual(task.ID, Assignment{ID: task.ID, Assignee: task.Assignee, Start: start, End: end, Status: "done", StartsInAfternoon: startsInAfternoon, EndsAtNoon: halfDaySet[dateKey(end)]}, asOfSlot)
			reflected[task.ID] = true
		}
	}
	return remainingSlots, reflected
}

func placeFixedTasks(scheduled []input.Task, cal *calendar, reflected map[string]bool, state *allocationState) {
	for _, task := range scheduled {
		if task.StartOn == nil || reflected[task.ID] {
			continue
		}
		begin := cal.dateToSlot(*task.StartOn)
		endDate := input.FixedTaskEndDate(task, cal.holidays, cal.halfDays)
		end := cal.dateToSlot(endDate) + 1
		cal.ensure(end + 1)
		for cal.slots[end].Equal(endDate) {
			end++
			cal.ensure(end + 1)
		}
		state.placeFixed(task.ID, task.Assignee, Assignment{ID: task.ID, Assignee: task.Assignee, Start: *task.StartOn, End: endDate, Status: task.Status, StartSlot: begin, EndSlot: end, Fixed: true}, end)
	}
}

func dependenciesReflected(task input.Task, dependencies map[string][]string, reflected map[string]bool) bool {
	for _, dependency := range dependencies[task.ID] {
		if !reflected[dependency] {
			return false
		}
	}
	return true
}

func placeInProgressTasks(scheduled []input.Task, dependencies map[string][]string, cal *calendar, reflected map[string]bool, state *allocationState) {
	for _, task := range scheduled {
		if task.Status != "in_progress" || task.StartOn != nil || reflected[task.ID] || !dependenciesReflected(task, dependencies, reflected) {
			continue
		}
		begin := state.freeAt[task.Assignee]
		end := begin + task.EffortSlots
		cal.ensure(end)
		state.confirm(task.ID, task.Assignee, Assignment{ID: task.ID, Assignee: task.Assignee, Start: cal.slots[begin], End: cal.slots[end-1], Status: "in_progress", StartSlot: begin, EndSlot: end}, end)
	}
}

func buildPendingTasks(scheduled []input.Task, reflected map[string]bool, state *allocationState) {
	for _, task := range scheduled {
		if task.StartOn == nil && !reflected[task.ID] {
			if _, exists := state.assigned[task.ID]; !exists {
				state.markPending(task.ID)
			}
		}
	}
}

func dependenciesCompleted(task input.Task, dependencies map[string][]string, state *allocationState) bool {
	for _, dependency := range dependencies[task.ID] {
		if _, exists := state.doneAt[dependency]; !exists {
			return false
		}
	}
	return true
}

func isReadyToSchedule(task input.Task, dependencies map[string][]string, state *allocationState) bool {
	if !state.pending[task.ID] {
		return false
	}
	return dependenciesCompleted(task, dependencies, state)
}

// 固定担当は plan_workers より優先し、plan_workers は担当未指定のタスクだけに適用する。
func isEligibleWorker(worker input.Worker, task input.Task, settings input.Settings) bool {
	if task.Assignee != "" {
		return worker.ID == task.Assignee
	}
	return slices.Contains(worker.Lanes, task.Lane) && (len(settings.PlanWorkers[task.Plan]) == 0 || slices.Contains(settings.PlanWorkers[task.Plan], worker.ID))
}

func eligibleWorkers(task input.Task, allWorkers []input.Worker, settings input.Settings) []input.Worker {
	var candidates []input.Worker
	for _, worker := range allWorkers {
		if isEligibleWorker(worker, task, settings) {
			candidates = append(candidates, worker)
		}
	}
	return candidates
}

func earliestAllowedStart(task input.Task, dependencies map[string][]string, cal *calendar, state *allocationState) int {
	dependencyReady := 0
	for _, dependency := range dependencies[task.ID] {
		if state.doneAt[dependency] > dependencyReady {
			dependencyReady = state.doneAt[dependency]
		}
	}
	if task.NotBefore != nil {
		if slot := cal.dateToSlot(*task.NotBefore); slot > dependencyReady {
			dependencyReady = slot
		}
	}
	return dependencyReady
}

func followWorker(task input.Task, byID map[string]input.Task, state *allocationState) string {
	if task.Follow != "" {
		if assignment, exists := state.assigned[task.Follow]; exists {
			return assignment.Assignee
		} else if followed := byID[task.Follow]; followed.Status == "done" {
			return followed.Assignee
		}
	}
	return ""
}

// assignmentCandidates は、制約を満たす担当者ごとに実行可能な割当候補を作る。
func assignmentCandidates(taskOrder int, task input.Task, value input.Input, workerOrder map[string]int, byID map[string]input.Task, dependencies map[string][]string, cal *calendar, criticalLength map[string]int, state *allocationState) []AssignmentCandidate {
	dependencyReady := earliestAllowedStart(task, dependencies, cal, state)
	eligible := eligibleWorkers(task, value.Workers, value.Settings)
	followID := followWorker(task, byID, state)
	canContinue := false
	followAssignment, hasFollowAssignment := state.assigned[task.Follow]
	if followID != "" && hasFollowAssignment && state.freeAt[followID] == followAssignment.EndSlot && dependencyReady <= state.freeAt[followID] {
		for _, worker := range eligible {
			if worker.ID == followID {
				canContinue = true
				break
			}
		}
	}
	candidates := make([]AssignmentCandidate, 0, len(eligible))
	for _, worker := range eligible {
		candidates = append(candidates, AssignmentCandidate{
			TaskID:                task.ID,
			Plan:                  task.Plan,
			Lane:                  task.Lane,
			Core:                  task.Core,
			WorkerID:              worker.ID,
			StartSlot:             max(state.freeAt[worker.ID], dependencyReady),
			CriticalLength:        criticalLength[task.ID],
			TaskOrder:             taskOrder,
			WorkerOrder:           workerOrder[worker.ID],
			FollowWorkerID:        followID,
			CanContinueWithoutGap: worker.ID == followID && canContinue,
		})
	}
	return candidates
}

func readyCandidates(scheduled []input.Task, value input.Input, workerOrder map[string]int, byID map[string]input.Task, dependencies map[string][]string, criticalLength map[string]int, cal *calendar, state *allocationState) []AssignmentCandidate {
	var candidates []AssignmentCandidate
	for taskOrder, task := range scheduled {
		if !isReadyToSchedule(task, dependencies, state) {
			continue
		}
		candidates = append(candidates, assignmentCandidates(taskOrder, task, value, workerOrder, byID, dependencies, cal, criticalLength, state)...)
	}
	return candidates
}

func pendingTaskFacts(scheduled []input.Task, state *allocationState) []TaskFacts {
	var pending []TaskFacts
	for order, task := range scheduled {
		if state.pending[task.ID] {
			pending = append(pending, TaskFacts{ID: task.ID, Plan: task.Plan, Lane: task.Lane, Core: task.Core, Order: order})
		}
	}
	return pending
}

func assignChosenCandidate(chosen AssignmentCandidate, task input.Task, cal *calendar, state *allocationState) {
	end := chosen.StartSlot + task.EffortSlots
	cal.ensure(end)
	state.confirm(task.ID, chosen.WorkerID, Assignment{ID: task.ID, Assignee: chosen.WorkerID, Start: cal.slots[chosen.StartSlot], End: cal.slots[end-1], Status: task.Status, StartSlot: chosen.StartSlot, EndSlot: end}, end)
}

// 未配置タスクがなくなるまで、実行可能な候補を選択Strategyへ提示して一件ずつ確定する。
func schedulePendingTasks(scheduled []input.Task, value input.Input, workerOrder map[string]int, byID map[string]input.Task, dependencies map[string][]string, criticalLength map[string]int, cal *calendar, state *allocationState, strategy SelectionStrategy) {
	for len(state.pending) > 0 {
		candidates := readyCandidates(scheduled, value, workerOrder, byID, dependencies, criticalLength, cal, state)
		if len(candidates) == 0 {
			panic("no schedulable task")
		}
		offered := append([]AssignmentCandidate(nil), candidates...)
		chosen := strategy.Select(SelectionInput{Candidates: offered, PendingTasks: pendingTaskFacts(scheduled, state)})
		valid := slices.Contains(candidates, chosen)
		if !valid {
			panic("selection strategy returned a candidate outside the offered set")
		}
		assignChosenCandidate(chosen, byID[chosen.TaskID], cal, state)
	}
}
