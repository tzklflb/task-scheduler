package scheduling

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tzklflb/task-scheduler/internal/input"
)

func TestCalculateRepresentativeFixture(t *testing.T) {
	value, issues := input.Load(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"), input.Overrides{})
	if len(issues) > 0 {
		t.Fatalf("Load() issues = %v", issues)
	}
	result := Calculate(*value)
	assertScheduleInvariants(t, *value, result)
	assignments := assignmentsByID(result.Assignments)
	if got := assignments["frontend-after-join"].Start.Format(dateLayout); got != "2026-01-08" {
		t.Fatalf("frontend-after-join start = %s", got)
	}
	if got := assignments["earliest-start"].Start.Format(dateLayout); got < "2026-01-14" {
		t.Fatalf("earliest-start start = %s", got)
	}
	if assignments["wave-two"].StartSlot < assignments["wave-one"].EndSlot || assignments["wave-unlisted"].StartSlot < assignments["wave-two"].EndSlot {
		t.Fatalf("wave order = %#v", assignments)
	}
	if assignments["follow-next"].Assignee != "backend-peer" || assignments["follow-next"].StartSlot != assignments["follow-source"].EndSlot {
		t.Fatalf("follow assignments = %#v", assignments)
	}
	if assignments["fixed-qa"].Start.Format(dateLayout) != "2026-01-09" || assignments["fixed-qa"].End.Format(dateLayout) != "2026-01-12" {
		t.Fatalf("fixed-qa = %#v", assignments["fixed-qa"])
	}
	if result.CoreEnd.Format(dateLayout) != "2026-01-20" || result.CompletionWithBuffer.Format(dateLayout) != "2026-01-30" {
		t.Fatalf("completion = core %s, buffer %s", result.CoreEnd, result.CompletionWithBuffer)
	}
	if result.RemainingEffort != 8.5 {
		t.Fatalf("RemainingEffort = %v", result.RemainingEffort)
	}
	if len(result.BufferDates) != 2 || result.BufferDates[1].Format(dateLayout) != "2026-01-30" || result.TailBufferDates[0].Format(dateLayout) != "2026-01-20" {
		t.Fatalf("buffer = %#v, tail = %#v", result.BufferDates, result.TailBufferDates)
	}
	if first, second := Calculate(*value), Calculate(*value); !sameAssignments(first.Assignments, second.Assignments) || !sameDates(first.BufferDates, second.BufferDates) || !first.CoreEnd.Equal(second.CoreEnd) || !first.CompletionWithBuffer.Equal(second.CompletionWithBuffer) {
		t.Fatal("Calculate() is not deterministic")
	}
}

func TestCalculateBreaksTiesByTaskIDThenWorkerOrder(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day, BufferDays: 0, Selection: input.SelectionSettings{DeferNonCore: input.DeferNonCoreSettings{Enabled: true}, PreferredWorker: input.PreferredWorkerSettings{DefaultThresholdDays: 3}}},
		Workers:  []input.Worker{{ID: "first", Lanes: []string{"backend"}}, {ID: "second", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "z", Name: "z", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "a", Name: "a", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}
	assignments := assignmentsByID(Calculate(value).Assignments)
	if assignments["a"].Assignee != "first" || assignments["z"].Assignee != "second" {
		t.Fatalf("assignments = %#v", assignments)
	}
}

func TestCalculateFixedAssigneeOverridesPlanWorkersInNormalPlacement(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{
			StartDate:   day,
			BufferDays:  0,
			Selection:   input.SelectionSettings{DeferNonCore: input.DeferNonCoreSettings{Enabled: true}, PreferredWorker: input.PreferredWorkerSettings{DefaultThresholdDays: 3}},
			PlanWorkers: map[string][]string{"restricted": {"insider"}},
		},
		Workers: []input.Worker{
			{ID: "outsider", Lanes: []string{"backend"}},
			{ID: "insider", Lanes: []string{"backend"}},
		},
		Tasks: []input.Task{
			{ID: "fixed-task", Name: "fixed-task", Plan: "restricted", EffortSlots: 2, Lane: "backend", Assignee: "outsider", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "plan-restricted", Name: "plan-restricted", Plan: "restricted", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}
	result := Calculate(value)
	assertScheduleInvariants(t, value, result)
	assignments := assignmentsByID(result.Assignments)
	if assignments["fixed-task"].Assignee != "outsider" {
		t.Fatalf("fixed-task assigned to %s, want fixed outsider despite plan_workers", assignments["fixed-task"].Assignee)
	}
	if assignments["plan-restricted"].Assignee != "insider" {
		t.Fatalf("plan-restricted assigned to %s, want plan_workers-restricted insider", assignments["plan-restricted"].Assignee)
	}
}

// 競合タスクを先に選べる条件を作り、担当継続が働いた場合だけ後続タスクが直後に始まるようにする。
func TestCalculateFixedAssigneeOverridesPlanWorkersInContinuationPlacement(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{
			StartDate:   day,
			BufferDays:  0,
			Selection:   input.SelectionSettings{DeferNonCore: input.DeferNonCoreSettings{Enabled: true}, FollowContinuity: input.FollowContinuitySettings{Enabled: true}, PreferredWorker: input.PreferredWorkerSettings{DefaultThresholdDays: 3}},
			PlanWorkers: map[string][]string{"restricted": {"insider"}},
		},
		Workers: []input.Worker{
			{ID: "outsider", Lanes: []string{"backend"}},
			{ID: "insider", Lanes: []string{"backend"}},
		},
		Tasks: []input.Task{
			{ID: "source", Name: "source", Plan: "restricted", EffortSlots: 2, Lane: "backend", Assignee: "outsider", StartOn: &day, Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "insider-blocker", Name: "insider-blocker", Plan: "restricted", EffortSlots: 4, Lane: "backend", Assignee: "insider", StartOn: &day, Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "decoy", Name: "decoy", Plan: "decoy", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "follow-task", Name: "follow-task", Plan: "restricted", EffortSlots: 2, Lane: "backend", Follow: "source", Assignee: "outsider", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}
	result := Calculate(value)
	assertScheduleInvariants(t, value, result)
	assignments := assignmentsByID(result.Assignments)
	if assignments["follow-task"].Assignee != "outsider" || assignments["follow-task"].StartSlot != assignments["source"].EndSlot {
		t.Fatalf("follow-task = %#v, want continuous outsider right after source despite a competing decoy task and plan_workers", assignments["follow-task"])
	}
	if assignments["decoy"].Assignee != "outsider" {
		t.Fatalf("decoy assigned to %s, want it to yield outsider to the continued follow-task and take outsider only once outsider is free again", assignments["decoy"].Assignee)
	}
}

func TestCalculateKeepsDoneAfterAsOfInPlannedSlot(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	doneOn := day.AddDate(0, 0, 2)
	value := input.Input{
		Settings: input.Settings{StartDate: day, AsOf: &day, BufferDays: 0},
		Workers:  []input.Worker{{ID: "a", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "done-later", Name: "done-later", Plan: "p", EffortSlots: 2, Dependencies: nil, Lane: "backend", Assignee: "a", Status: "done", DoneOn: &doneOn, Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "dependent", Name: "dependent", Plan: "p", EffortSlots: 2, Dependencies: []string{"done-later"}, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}
	result := Calculate(value)
	assertScheduleInvariants(t, value, result)
	assignments := assignmentsByID(result.Assignments)
	if assignments["done-later"].EndSlot == 0 || assignments["done-later"].Status != "done" {
		t.Fatalf("done-later = %#v", assignments["done-later"])
	}
	if assignments["dependent"].StartSlot != assignments["done-later"].EndSlot {
		t.Fatalf("assignments = %#v", assignments)
	}
	if result.RemainingEffort != 1 {
		t.Fatalf("RemainingEffort = %v", result.RemainingEffort)
	}
}

func TestCalculatePlacesDependencyReflectedInProgressTaskBeforeRegularTasksAtAsOf(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day, AsOf: &day, BufferDays: 0},
		Workers:  []input.Worker{{ID: "a", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "regular", Name: "regular", Plan: "p", EffortSlots: 2, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "completed", Name: "completed", Plan: "p", EffortSlots: 2, Lane: "backend", Assignee: "a", Status: "done", DoneOn: &day, Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "in-progress", Name: "in-progress", Plan: "p", EffortSlots: 2, Dependencies: []string{"completed"}, Lane: "backend", Assignee: "a", Status: "in_progress", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}

	assignments := assignmentsByID(Calculate(value).Assignments)
	if completed := assignments["completed"]; completed.Status != "done" || completed.EndSlot != 0 {
		t.Fatalf("completed dependency = %#v, want reflected completion", completed)
	}
	if got := assignments["in-progress"].Start.Format(dateLayout); got != "2026-01-05" {
		t.Fatalf("in-progress start = %s, want as-of date 2026-01-05", got)
	}
	if got := assignments["regular"].Start.Format(dateLayout); got != "2026-01-06" {
		t.Fatalf("regular start = %s, want 2026-01-06 after in-progress task", got)
	}
}

func TestCalculateCoreEndMinDoesNotChangeTaskPlacement(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	minimum := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day, BufferDays: 0},
		Workers:  []input.Worker{{ID: "a", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "task", Name: "task", Plan: "p", EffortSlots: 2, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}
	withoutMinimum := Calculate(value)
	value.Settings.CoreEndMin = &minimum
	withMinimum := Calculate(value)

	if !sameAssignments(withoutMinimum.Assignments, withMinimum.Assignments) {
		t.Fatalf("CoreEndMin changed task placement: without = %#v, with = %#v", withoutMinimum.Assignments, withMinimum.Assignments)
	}
	if got := withoutMinimum.CoreEnd.Format(dateLayout); got != "2026-01-05" {
		t.Fatalf("CoreEnd without minimum = %s, want 2026-01-05", got)
	}
	if got := withMinimum.CoreEnd.Format(dateLayout); got != "2026-01-12" {
		t.Fatalf("CoreEnd with minimum = %s, want 2026-01-12", got)
	}
}

func TestReservePeriodDaysCarriesMonthlyBufferShortageIntoNextMonth(t *testing.T) {
	start := time.Date(2026, 1, 30, 0, 0, 0, 0, time.UTC)
	reserved, _ := reservePeriodDays(input.Settings{
		StartDate:        start,
		BufferAllocation: map[string]int{"2026-01": 2},
	}, start, map[string]bool{}, map[string]bool{})

	if len(reserved) != 2 || !reserved["2026-01-30"] || !reserved["2026-02-27"] {
		t.Fatalf("reserved = %v, want Jan 30 and carried Feb 27", reserved)
	}
}

func TestReservePeriodDaysCarriesSprintBufferShortageIntoNextSprint(t *testing.T) {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	reserved, _ := reservePeriodDays(input.Settings{
		StartDate:        start,
		BufferAllocation: map[string]int{"S1": 2},
		SprintGoals: []input.SprintGoal{
			{End: start},
			{End: start.AddDate(0, 0, 2)},
		},
		SprintLengthDays: 2,
	}, start, map[string]bool{}, map[string]bool{})

	if len(reserved) != 2 || !reserved["2026-01-05"] || !reserved["2026-01-07"] {
		t.Fatalf("reserved = %v, want Jan 5 and carried Jan 7", reserved)
	}
}

func TestCalculateIncludesQANonBlockingTaskInCoreEnd(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day, BufferDays: 0},
		Workers: []input.Worker{
			{ID: "a", Lanes: []string{"backend"}},
			{ID: "b", Lanes: []string{"backend"}},
		},
		Tasks: []input.Task{
			{ID: "normal", Name: "normal", Plan: "p", EffortSlots: 2, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "non-blocking", Name: "non-blocking", Plan: "p", EffortSlots: 4, Lane: "backend", Assignee: "b", Core: true, ReleaseBlocking: true, QABlocking: false},
		},
	}
	result := Calculate(value)
	if result.CompletionWithBuffer.Format(dateLayout) != "2026-01-05" || result.CoreEnd.Format(dateLayout) != "2026-01-06" {
		t.Fatalf("completion = %s, core end = %s", result.CompletionWithBuffer, result.CoreEnd)
	}
}

func TestCalculateSkipsReservedBufferDaysForRegularTasks(t *testing.T) {
	day := time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{
			StartDate:        day,
			BufferDays:       1,
			BufferAllocation: map[string]int{"2026-01": 1},
		},
		Workers: []input.Worker{{ID: "a", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "task", Name: "task", Plan: "p", EffortSlots: 4, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}
	assignment := assignmentsByID(Calculate(value).Assignments)["task"]
	if assignment.End.Format(dateLayout) != "2026-02-02" {
		t.Fatalf("task end = %s, want 2026-02-02", assignment.End)
	}
}

func TestCalculateAllowsFixedTaskOnReservedBufferDays(t *testing.T) {
	start := time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{
			StartDate:        start,
			BufferDays:       1,
			BufferAllocation: map[string]int{"2026-01": 1},
		},
		Workers: []input.Worker{{ID: "a", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "fixed", Name: "fixed", Plan: "p", EffortSlots: 4, Lane: "backend", Assignee: "a", StartOn: &start, Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}

	assignment := Calculate(value).Assignments[0]
	if got := assignment.End.Format(dateLayout); got != "2026-01-30" {
		t.Fatalf("fixed task end = %s, want 2026-01-30", got)
	}
}

func TestCalculateRecordsHalfDayBoundaries(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day, HalfDays: []time.Time{day.AddDate(0, 0, 1)}},
		Workers:  []input.Worker{{ID: "a", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "morning", Name: "morning", Plan: "p", EffortSlots: 1, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "afternoon", Name: "afternoon", Plan: "p", EffortSlots: 1, Dependencies: []string{"morning"}, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "half-day", Name: "half-day", Plan: "p", EffortSlots: 1, Dependencies: []string{"afternoon"}, Lane: "backend", Assignee: "a", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}
	assignments := assignmentsByID(Calculate(value).Assignments)
	if !assignments["morning"].EndsAtNoon || !assignments["afternoon"].StartsInAfternoon || !assignments["half-day"].EndsAtNoon {
		t.Fatalf("half-day boundaries = %#v", assignments)
	}
}

func TestReservePeriodDaysOrdersSprintNumbersNumerically(t *testing.T) {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	goals := make([]input.SprintGoal, 10)
	for index := range goals {
		goals[index].End = start.AddDate(0, 0, (index+1)*14-1)
	}
	reserved, _ := reservePeriodDays(input.Settings{
		StartDate:        start,
		BufferAllocation: map[string]int{"S2": 1, "S10": 1},
		SprintGoals:      goals,
		SprintLengthDays: 14,
	}, start, map[string]bool{}, map[string]bool{})
	if len(reserved) != 2 {
		t.Fatalf("reserved = %v", reserved)
	}
	foundSprint10 := false
	for value := range reserved {
		day, err := time.Parse(dateLayout, value)
		if err != nil {
			t.Fatal(err)
		}
		if day.After(goals[8].End) {
			foundSprint10 = true
		}
	}
	if !foundSprint10 {
		t.Fatalf("S10 reservation is missing: %v", reserved)
	}
}

func assertScheduleInvariants(t *testing.T, value input.Input, result Schedule) {
	t.Helper()
	assignments := assignmentsByID(result.Assignments)
	workers := map[string]input.Worker{}
	for _, worker := range value.Workers {
		workers[worker.ID] = worker
	}
	tasks := map[string]input.Task{}
	for _, task := range value.Tasks {
		tasks[task.ID] = task
		if task.ReleaseBlocking {
			if _, exists := assignments[task.ID]; !exists {
				t.Errorf("%s was not assigned", task.ID)
			}
		} else if _, exists := assignments[task.ID]; exists {
			t.Errorf("%s is release-blocking=false but was assigned", task.ID)
		}
	}
	dependencies := input.EffectiveDependencies(value.Tasks, value.Settings.Waves)
	for id, assignment := range assignments {
		task := tasks[id]
		worker := workers[assignment.Assignee]
		if !slices.Contains(worker.Lanes, task.Lane) {
			t.Errorf("%s assigned outside lane", id)
		}
		if task.Assignee != "" && task.Assignee != assignment.Assignee {
			t.Errorf("%s assigned to %s, want fixed %s", id, assignment.Assignee, task.Assignee)
		}
		if worker.AvailableFrom != nil && assignment.Start.Before(*worker.AvailableFrom) && assignment.EndSlot != 0 {
			t.Errorf("%s starts before %s is available", id, worker.ID)
		}
		for _, dependency := range dependencies[id] {
			if assignment.Status == "done" && assignment.EndSlot == 0 {
				continue
			}
			if previous, exists := assignments[dependency]; exists && previous.EndSlot != 0 && assignment.StartSlot < previous.EndSlot {
				t.Errorf("%s starts before %s completes", id, dependency)
			}
		}
	}
	for leftID, left := range assignments {
		if left.Status == "done" && left.EndSlot == 0 {
			continue
		}
		for rightID, right := range assignments {
			if leftID >= rightID || (right.Status == "done" && right.EndSlot == 0) || left.Assignee != right.Assignee {
				continue
			}
			if left.StartSlot < right.EndSlot && right.StartSlot < left.EndSlot {
				t.Errorf("%s and %s overlap for %s", leftID, rightID, left.Assignee)
			}
		}
	}
}

func assignmentsByID(values []Assignment) map[string]Assignment {
	result := make(map[string]Assignment, len(values))
	for _, value := range values {
		if _, exists := result[value.ID]; exists {
			panic("duplicate assignment")
		}
		result[value.ID] = value
	}
	return result
}

func sameAssignments(left, right []Assignment) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameDates(left, right []time.Time) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !left[index].Equal(right[index]) {
			return false
		}
	}
	return true
}
