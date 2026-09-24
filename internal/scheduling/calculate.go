package scheduling

import (
	"sort"
	"time"

	"github.com/tzklflb/task-scheduler/internal/input"
)

// Calculate は、検証済みの入力に既定の選択Strategyを適用してスケジュールを生成する。
func Calculate(value input.Input) Schedule {
	return CalculateWithStrategy(value, defaultSelectionStrategy(value.Settings.Selection))
}

func defaultSelectionStrategy(settings input.SelectionSettings) DefaultSelectionStrategy {
	return DefaultSelectionStrategy{
		deferNonCore:     DeferNonCoreRule{Enabled: settings.DeferNonCore.Enabled},
		followContinuity: FollowContinuityRule{Enabled: settings.FollowContinuity.Enabled, Scope: settings.FollowContinuity.Scope},
		preferredWorker: PreferredWorkerRule{
			PlanAssignees:        settings.PreferredWorker.PlanAssignees,
			DefaultThresholdDays: settings.PreferredWorker.DefaultThresholdDays,
			PlanThresholdDays:    settings.PreferredWorker.PlanThresholdDays,
			Scope:                settings.PreferredWorker.Scope,
		},
	}
}

// CalculateWithStrategy は、検証済みの入力と選択Strategyからスケジュールを生成する。
func CalculateWithStrategy(value input.Input, strategy SelectionStrategy) Schedule {
	if strategy == nil {
		panic("selection strategy must not be nil")
	}
	settings := value.Settings
	holidaySet, halfDaySet := input.DateSet(settings.Holidays), input.DateSet(settings.HalfDays)
	lowerBound := settings.StartDate
	if settings.AsOf != nil {
		lowerBound = *settings.AsOf
	}
	reserved, tailQuota := reservePeriodDays(settings, lowerBound, holidaySet, halfDaySet)
	cal := newCalendar(settings.StartDate, holidaySet, halfDaySet, reserved)
	dependencies := input.EffectiveDependencies(value.Tasks, settings.Waves)
	criticalLength := criticalLengths(value.Tasks, dependencies)
	byID := map[string]input.Task{}
	for _, task := range value.Tasks {
		byID[task.ID] = task
	}
	workerOrder := map[string]int{}
	for index, worker := range value.Workers {
		workerOrder[worker.ID] = index
	}
	asOfSlot := 0
	if settings.AsOf != nil {
		asOfSlot = cal.dateToSlot(*settings.AsOf)
	}
	scheduled := make([]input.Task, 0, len(value.Tasks))
	for _, task := range value.Tasks {
		if task.ReleaseBlocking {
			scheduled = append(scheduled, task)
		}
	}

	state := newAllocationState(value.Workers, cal, asOfSlot)
	remainingSlots, reflected := reflectCompletedTasks(scheduled, settings, holidaySet, halfDaySet, asOfSlot, state)
	placeFixedTasks(scheduled, cal, reflected, state)
	placeInProgressTasks(scheduled, dependencies, cal, reflected, state)
	buildPendingTasks(scheduled, reflected, state)
	schedulePendingTasks(scheduled, value, workerOrder, byID, dependencies, criticalLength, cal, state, strategy)

	return buildSchedule(value, cal, byID, holidaySet, halfDaySet, reserved, tailQuota, asOfSlot, remainingSlots, state)
}

func buildSchedule(value input.Input, cal *calendar, byID map[string]input.Task, holidaySet, halfDaySet map[string]bool, reserved map[string]bool, tailQuota int, asOfSlot int, remainingSlots int, state *allocationState) Schedule {
	settings := value.Settings
	maxEndSlot := 0
	for _, assignment := range state.assigned {
		if assignment.EndSlot > maxEndSlot {
			maxEndSlot = assignment.EndSlot
		}
	}
	if maxEndSlot > 0 {
		cal.ensure(maxEndSlot + 1)
		for id, assignment := range state.assigned {
			if assignment.EndSlot == 0 {
				continue
			}
			assignment.StartsInAfternoon = assignment.StartSlot > 0 && cal.slots[assignment.StartSlot-1].Equal(cal.slots[assignment.StartSlot])
			assignment.EndsAtNoon = halfDaySet[dateKey(assignment.End)] || cal.slots[assignment.EndSlot].Equal(cal.slots[assignment.EndSlot-1])
			state.assigned[id] = assignment
		}
	}

	var assignments []Assignment
	for _, task := range value.Tasks {
		if assignment, exists := state.assigned[task.ID]; exists {
			assignments = append(assignments, assignment)
		}
	}
	lastAny := asOfSlot
	for _, assignment := range assignments {
		if !assignment.Fixed && assignment.EndSlot > lastAny {
			lastAny = assignment.EndSlot
		}
	}
	cal.ensure(max(lastAny, 1))
	tailDates := reserveTailDays(cal.slots[max(lastAny-1, 0)].AddDate(0, 0, 1), tailQuota, holidaySet, halfDaySet, reserved)
	for _, day := range tailDates {
		reserved[dateKey(day)] = true
	}
	completion := completionDate(assignments, byID, false, settings.AsOf, settings.StartDate)
	coreEnd := completionDate(assignments, byID, true, settings.AsOf, settings.StartDate)
	if settings.CoreEndMin != nil && coreEnd.Before(*settings.CoreEndMin) {
		coreEnd = *settings.CoreEndMin
	}
	for _, day := range tailDates {
		if day.After(completion) {
			completion = day
		}
	}
	for day := range reserved {
		parsed, _ := time.Parse(dateLayout, day)
		if parsed.After(completion) {
			completion = parsed
		}
	}
	bufferDates := make([]time.Time, 0, len(reserved))
	for day := range reserved {
		parsed, _ := time.Parse(dateLayout, day)
		bufferDates = append(bufferDates, parsed)
	}
	sort.Slice(bufferDates, func(i, j int) bool { return bufferDates[i].Before(bufferDates[j]) })
	return Schedule{Assignments: assignments, BufferDates: bufferDates, TailBufferDates: tailDates, CoreEnd: coreEnd, CompletionWithBuffer: completion, RemainingEffort: float64(remainingSlots) / 2}
}

func completionDate(assignments []Assignment, tasks map[string]input.Task, coreOnly bool, asOf *time.Time, start time.Time) time.Time {
	result := time.Time{}
	doneResult := time.Time{}
	for _, assignment := range assignments {
		task := tasks[assignment.ID]
		if assignment.Fixed || (coreOnly && !task.Core) || (!coreOnly && !task.QABlocking) {
			continue
		}
		if assignment.Status == "done" && assignment.EndSlot == 0 {
			if assignment.End.After(doneResult) {
				doneResult = assignment.End
			}
			continue
		}
		if assignment.End.After(result) {
			result = assignment.End
		}
	}
	if !result.IsZero() {
		return result
	}
	if !doneResult.IsZero() {
		return doneResult
	}
	result = start
	if asOf != nil {
		result = *asOf
	}
	return result
}
