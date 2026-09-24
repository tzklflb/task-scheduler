package scheduling

import (
	"testing"
	"time"

	"github.com/tzklflb/task-scheduler/internal/input"
)

func TestDefaultSelectionStrategySelectsSameCandidateForSameInput(t *testing.T) {
	candidates := []AssignmentCandidate{
		{TaskID: "b", StartSlot: 4, CriticalLength: 2, TaskOrder: 1, WorkerOrder: 0},
		{TaskID: "a", StartSlot: 4, CriticalLength: 2, TaskOrder: 0, WorkerOrder: 1},
	}
	first := DefaultSelectionStrategy{}.Select(SelectionInput{Candidates: candidates})
	second := DefaultSelectionStrategy{}.Select(SelectionInput{Candidates: candidates})
	if first != second {
		t.Fatalf("DefaultSelectionStrategy.Select() is not deterministic: %#v vs %#v", first, second)
	}
}

func TestDefaultSelectionStrategyRejectsEmptyCandidates(t *testing.T) {
	requirePanicMessage(t, "selection strategy requires at least one candidate", func() {
		DefaultSelectionStrategy{}.Select(SelectionInput{})
	})
}

func TestDefaultSelectionStrategySelectionOrder(t *testing.T) {
	tests := []struct {
		name       string
		candidates []AssignmentCandidate
		want       string
	}{
		{
			name: "follow continuity outranks an earlier start",
			candidates: []AssignmentCandidate{
				{TaskID: "earlier", StartSlot: 0, CriticalLength: 1},
				{TaskID: "continued", StartSlot: 5, CriticalLength: 1, CanContinueWithoutGap: true},
			},
			want: "continued",
		},
		{
			name: "ties among continuity candidates go to the earliest task order",
			candidates: []AssignmentCandidate{
				{TaskID: "later-defined", StartSlot: 5, CanContinueWithoutGap: true, TaskOrder: 1},
				{TaskID: "earlier-defined", StartSlot: 9, CanContinueWithoutGap: true, TaskOrder: 0},
			},
			want: "earlier-defined",
		},
		{
			name: "earliest start wins among non-continuity candidates",
			candidates: []AssignmentCandidate{
				{TaskID: "late", StartSlot: 6, CriticalLength: 9},
				{TaskID: "early", StartSlot: 2, CriticalLength: 1},
			},
			want: "early",
		},
		{
			name: "longer critical length breaks a start-slot tie",
			candidates: []AssignmentCandidate{
				{TaskID: "short", StartSlot: 3, CriticalLength: 2},
				{TaskID: "long", StartSlot: 3, CriticalLength: 5},
			},
			want: "long",
		},
		{
			name: "task ID breaks a start-slot and critical-length tie",
			candidates: []AssignmentCandidate{
				{TaskID: "z", StartSlot: 3, CriticalLength: 2},
				{TaskID: "a", StartSlot: 3, CriticalLength: 2},
			},
			want: "a",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := (DefaultSelectionStrategy{followContinuity: FollowContinuityRule{Enabled: true}}).Select(SelectionInput{Candidates: test.candidates}).TaskID
			if got != test.want {
				t.Fatalf("Select() = %s, want %s", got, test.want)
			}
		})
	}
}

// recordingStrategyは、注入したSelectionStrategyが計算本体の選択を実際に制御することを確認する。
// TaskIDの辞書順で最大の候補を選び、同順位なら先に現れた候補を維持する。
// また、提示された候補集合に実行可能なタスクまたは担当者が複数含まれたかを記録する。
type recordingStrategy struct {
	sawMultiCandidateIteration bool
}

func (p *recordingStrategy) Select(input SelectionInput) AssignmentCandidate {
	if len(input.Candidates) > 1 {
		p.sawMultiCandidateIteration = true
	}
	candidates := append([]AssignmentCandidate(nil), input.Candidates...)
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.TaskID > best.TaskID {
			best = candidate
		}
	}
	return best
}

func TestCalculateWithStrategyUsesInjectedStrategy(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day, BufferDays: 0, Selection: input.SelectionSettings{DeferNonCore: input.DeferNonCoreSettings{Enabled: true}, PreferredWorker: input.PreferredWorkerSettings{DefaultThresholdDays: 3}}},
		Workers:  []input.Worker{{ID: "first", Lanes: []string{"backend"}}, {ID: "second", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "z", Name: "z", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
			{ID: "a", Name: "a", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}

	defaultResult := assignmentsByID(Calculate(value).Assignments)
	if defaultResult["a"].Assignee != "first" || defaultResult["z"].Assignee != "second" {
		t.Fatalf("default strategy result = %#v", defaultResult)
	}

	strategy := &recordingStrategy{}
	injected := assignmentsByID(CalculateWithStrategy(value, strategy).Assignments)
	if injected["z"].Assignee != "first" || injected["a"].Assignee != "first" {
		t.Fatalf("injected strategy result = %#v", injected)
	}
	if !strategy.sawMultiCandidateIteration {
		t.Fatal("strategy never saw more than one feasible candidate; core did not offer a real choice")
	}
}

// workerChoiceStrategyは、指定した担当者の候補を常に選ぶ。
// これにより、計算本体を書き換えずに選択Strategyが担当者を変更できることを確認する。
type workerChoiceStrategy struct {
	workerID string
}

func (p workerChoiceStrategy) Select(input SelectionInput) AssignmentCandidate {
	for _, candidate := range input.Candidates {
		if candidate.WorkerID == p.workerID {
			return candidate
		}
	}
	return input.Candidates[0]
}

func TestCalculateWithStrategyCanChangeWorkerForSameTask(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day},
		Workers:  []input.Worker{{ID: "first", Lanes: []string{"backend"}}, {ID: "second", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "solo", Name: "solo", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}

	defaultResult := assignmentsByID(Calculate(value).Assignments)
	if defaultResult["solo"].Assignee != "first" {
		t.Fatalf("default strategy result = %#v", defaultResult)
	}

	injected := assignmentsByID(CalculateWithStrategy(value, workerChoiceStrategy{workerID: "second"}).Assignments)
	if injected["solo"].Assignee != "second" {
		t.Fatalf("injected strategy result = %#v", injected)
	}
}

func TestDefaultSelectionStrategyWorkerSelection(t *testing.T) {
	tests := []struct {
		name       string
		candidates []AssignmentCandidate
		preferred  PreferredWorkerRule
		want       string
	}{
		{
			name: "earliest start wins, ties go to the earliest worker input order",
			candidates: []AssignmentCandidate{
				{TaskID: "t", WorkerID: "w1", StartSlot: 3, WorkerOrder: 0},
				{TaskID: "t", WorkerID: "w2", StartSlot: 3, WorkerOrder: 1},
			},
			want: "w1",
		},
		{
			name: "a slower preferred worker wins within its threshold",
			candidates: []AssignmentCandidate{
				{TaskID: "t", Plan: "p", WorkerID: "w1", StartSlot: 0, WorkerOrder: 0},
				{TaskID: "t", Plan: "p", WorkerID: "w2", StartSlot: 2, WorkerOrder: 1},
			},
			preferred: PreferredWorkerRule{PlanAssignees: map[string]string{"p": "w2"}, DefaultThresholdDays: 1},
			want:      "w2",
		},
		{
			name: "a preferred worker outside its threshold loses to the faster worker",
			candidates: []AssignmentCandidate{
				{TaskID: "t", Plan: "p", WorkerID: "w1", StartSlot: 0, WorkerOrder: 0},
				{TaskID: "t", Plan: "p", WorkerID: "w2", StartSlot: 3, WorkerOrder: 1},
			},
			preferred: PreferredWorkerRule{PlanAssignees: map[string]string{"p": "w2"}, DefaultThresholdDays: 1},
			want:      "w1",
		},
		{
			name: "worker order breaks a start-slot tie regardless of candidate array order",
			candidates: []AssignmentCandidate{
				{TaskID: "t", WorkerID: "w2", StartSlot: 3, WorkerOrder: 1},
				{TaskID: "t", WorkerID: "w1", StartSlot: 3, WorkerOrder: 0},
			},
			want: "w1",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := (DefaultSelectionStrategy{preferredWorker: test.preferred}).Select(SelectionInput{Candidates: test.candidates}).WorkerID
			if got != test.want {
				t.Fatalf("Select().WorkerID = %s, want %s", got, test.want)
			}
		})
	}
}

func TestSelectionRulesNarrowCandidates(t *testing.T) {
	core := AssignmentCandidate{TaskID: "core", Core: true, TaskOrder: 1}
	nonCore := AssignmentCandidate{TaskID: "non-core", Core: false, TaskOrder: 0}
	continuation := AssignmentCandidate{TaskID: "follow", TaskOrder: 2, CanContinueWithoutGap: true}
	tests := []struct {
		name       string
		rule       SelectionRule
		context    RuleContext
		candidates []AssignmentCandidate
		want       []AssignmentCandidate
		applied    bool
	}{
		{name: "defer non-core", rule: DeferNonCoreRule{Enabled: true}, context: RuleContext{PendingTasks: []TaskFacts{{ID: "pending-core", Core: true}}}, candidates: []AssignmentCandidate{nonCore, core}, want: []AssignmentCandidate{core}, applied: true},
		{name: "pending core applies when all candidates are core", rule: DeferNonCoreRule{Enabled: true}, context: RuleContext{PendingTasks: []TaskFacts{{ID: "pending-core", Core: true}}}, candidates: []AssignmentCandidate{core}, want: []AssignmentCandidate{core}, applied: true},
		{name: "defer non-core disabled", rule: DeferNonCoreRule{}, context: RuleContext{PendingTasks: []TaskFacts{{ID: "pending-core", Core: true}}}, candidates: []AssignmentCandidate{core, nonCore}, want: []AssignmentCandidate{core, nonCore}},
		{name: "follow disabled", rule: FollowContinuityRule{}, candidates: []AssignmentCandidate{continuation}, want: []AssignmentCandidate{continuation}},
		{name: "follow enabled", rule: FollowContinuityRule{Enabled: true}, candidates: []AssignmentCandidate{core, continuation}, want: []AssignmentCandidate{continuation}, applied: true},
		{name: "task order tie", rule: TaskOrderRule{}, candidates: []AssignmentCandidate{{TaskID: "late", TaskOrder: 2}, {TaskID: "early", TaskOrder: 1}, {TaskID: "same", TaskOrder: 1}}, want: []AssignmentCandidate{{TaskID: "early", TaskOrder: 1}, {TaskID: "same", TaskOrder: 1}}, applied: true},
		{name: "preferred follow worker outranks plan preference", rule: PreferredWorkerRule{PlanAssignees: map[string]string{"p": "plan"}, DefaultThresholdDays: 10}, candidates: []AssignmentCandidate{{TaskID: "t", Plan: "p", WorkerID: "plan", FollowWorkerID: "follow", StartSlot: 0}, {TaskID: "t", Plan: "p", WorkerID: "follow", FollowWorkerID: "follow", StartSlot: 2}}, want: []AssignmentCandidate{{TaskID: "t", Plan: "p", WorkerID: "follow", FollowWorkerID: "follow", StartSlot: 2}}, applied: true},
		{name: "preferred follow worker outside threshold does not fall back to plan", rule: PreferredWorkerRule{PlanAssignees: map[string]string{"p": "plan"}}, candidates: []AssignmentCandidate{{TaskID: "t", Plan: "p", WorkerID: "plan", FollowWorkerID: "follow", StartSlot: 0}, {TaskID: "t", Plan: "p", WorkerID: "follow", FollowWorkerID: "follow", StartSlot: 2}}, want: []AssignmentCandidate{{TaskID: "t", Plan: "p", WorkerID: "plan", FollowWorkerID: "follow", StartSlot: 0}, {TaskID: "t", Plan: "p", WorkerID: "follow", FollowWorkerID: "follow", StartSlot: 2}}},
		{name: "missing follow worker does not fall back to plan", rule: PreferredWorkerRule{PlanAssignees: map[string]string{"p": "plan"}, DefaultThresholdDays: 10}, candidates: []AssignmentCandidate{{TaskID: "t", Plan: "p", WorkerID: "plan", FollowWorkerID: "follow", StartSlot: 0}}, want: []AssignmentCandidate{{TaskID: "t", Plan: "p", WorkerID: "plan", FollowWorkerID: "follow", StartSlot: 0}}},
		{name: "earliest worker start", rule: EarliestWorkerStartRule{}, candidates: []AssignmentCandidate{{TaskID: "t", StartSlot: 2}, {TaskID: "t", StartSlot: 1}}, want: []AssignmentCandidate{{TaskID: "t", StartSlot: 1}}, applied: true},
		{name: "worker order tie", rule: WorkerOrderRule{}, candidates: []AssignmentCandidate{{TaskID: "t", WorkerOrder: 1}, {TaskID: "t", WorkerOrder: 0}, {TaskID: "t", WorkerOrder: 0}}, want: []AssignmentCandidate{{TaskID: "t", WorkerOrder: 0}, {TaskID: "t", WorkerOrder: 0}}, applied: true},
		{name: "earliest task start", rule: EarliestTaskStartRule{}, candidates: []AssignmentCandidate{{TaskID: "later", StartSlot: 2}, {TaskID: "early", StartSlot: 1}}, want: []AssignmentCandidate{{TaskID: "early", StartSlot: 1}}, applied: true},
		{name: "critical length descending", rule: CriticalLengthRule{}, candidates: []AssignmentCandidate{{TaskID: "short", CriticalLength: 2}, {TaskID: "long", CriticalLength: 3}}, want: []AssignmentCandidate{{TaskID: "long", CriticalLength: 3}}, applied: true},
		{name: "task ID tie", rule: TaskIDRule{}, candidates: []AssignmentCandidate{{TaskID: "z"}, {TaskID: "a"}}, want: []AssignmentCandidate{{TaskID: "a"}}, applied: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.rule.Narrow(test.context, test.candidates)
			if got.Applied != test.applied || !equalCandidates(got.Candidates, test.want) {
				t.Fatalf("Narrow() = %#v, want candidates %#v applied %v", got, test.want, test.applied)
			}
		})
	}
}

func TestFollowContinuityScopeControlsShortCircuit(t *testing.T) {
	candidates := []AssignmentCandidate{
		{TaskID: "outside", Plan: "other", Lane: "backend", StartSlot: 0},
		{TaskID: "inside", Plan: "target", Lane: "backend", StartSlot: 8, CanContinueWithoutGap: true},
	}
	for _, test := range []struct {
		name  string
		scope input.SelectionScope
		want  string
	}{
		{"scope includes continuation", input.SelectionScope{Plans: []string{"target"}}, "inside"},
		{"scope excludes continuation", input.SelectionScope{Plans: []string{"other"}}, "outside"},
	} {
		t.Run(test.name, func(t *testing.T) {
			strategy := DefaultSelectionStrategy{followContinuity: FollowContinuityRule{Enabled: true, Scope: test.scope}}
			if got := strategy.Select(SelectionInput{Candidates: candidates}).TaskID; got != test.want {
				t.Fatalf("selected %q, want %q", got, test.want)
			}
		})
	}
}

func TestPreferredWorkerScopeOnlyNarrowsMatchingTasks(t *testing.T) {
	candidates := []AssignmentCandidate{
		{TaskID: "in-scope", Plan: "target", Lane: "backend", WorkerID: "fast", StartSlot: 0, WorkerOrder: 0},
		{TaskID: "in-scope", Plan: "target", Lane: "backend", WorkerID: "preferred", StartSlot: 2, WorkerOrder: 1},
		{TaskID: "out-of-scope", Plan: "other", Lane: "backend", WorkerID: "fast", StartSlot: 0, WorkerOrder: 0},
		{TaskID: "out-of-scope", Plan: "other", Lane: "backend", WorkerID: "preferred", StartSlot: 2, WorkerOrder: 1},
	}
	rule := PreferredWorkerRule{PlanAssignees: map[string]string{"target": "preferred", "other": "preferred"}, DefaultThresholdDays: 2, Scope: input.SelectionScope{Plans: []string{"target"}}}
	got := narrowEachTask(RuleContext{}, candidates, rule)
	if len(got) != 3 {
		t.Fatalf("narrowEachTask returned %#v, want 3 candidates", got)
	}
	if got[0].WorkerID != "preferred" || got[1].TaskID != "out-of-scope" || got[1].WorkerID != "fast" || got[2].WorkerID != "preferred" {
		t.Fatalf("scope affected the wrong candidate group: %#v", got)
	}
}

func TestDefaultSelectionStrategyDefersNonCoreWhenPendingCoreHasNoCandidate(t *testing.T) {
	rule := DeferNonCoreRule{Enabled: true}
	requirePanicMessage(t, "non-core candidates deferred while core tasks remain", func() {
		applySelectionRule(rule, RuleContext{PendingTasks: []TaskFacts{{ID: "blocked-core", Core: true}}}, []AssignmentCandidate{{TaskID: "non-core", Core: false}})
	})
}

type invalidRule struct {
	result func([]AssignmentCandidate) []AssignmentCandidate
}

func (rule invalidRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	return RuleResult{Candidates: rule.result(candidates)}
}

func TestSelectionRuleContractRejectsInvalidCandidateSets(t *testing.T) {
	candidate := AssignmentCandidate{TaskID: "t", WorkerID: "w"}
	tests := []struct {
		name   string
		result func([]AssignmentCandidate) []AssignmentCandidate
		want   string
	}{
		{name: "addition", result: func(candidates []AssignmentCandidate) []AssignmentCandidate {
			return append(candidates, AssignmentCandidate{TaskID: "new"})
		}, want: "selection rule returned a candidate outside the offered set"},
		{name: "modification", result: func(candidates []AssignmentCandidate) []AssignmentCandidate {
			candidates[0].WorkerID = "changed"
			return candidates
		}, want: "selection rule returned a candidate outside the offered set"},
		{name: "empty", result: func([]AssignmentCandidate) []AssignmentCandidate { return nil }, want: "selection rule returned no candidates"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requirePanicMessage(t, test.want, func() {
				applySelectionRule(invalidRule{result: test.result}, RuleContext{}, []AssignmentCandidate{candidate})
			})
		})
	}
}

func equalCandidates(left, right []AssignmentCandidate) bool {
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

// rogueCandidateStrategyは、不正または不具合のあるSelectionStrategyを模倣し、
// 計算本体が提示していないAssignmentCandidateを返す。
type rogueCandidateStrategy struct{}

func (rogueCandidateStrategy) Select(SelectionInput) AssignmentCandidate {
	return AssignmentCandidate{TaskID: "does-not-exist", WorkerID: "nobody", StartSlot: 0}
}

func TestCalculateWithStrategyRejectsCandidateOutsideOfferedSet(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day},
		Workers:  []input.Worker{{ID: "first", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "solo", Name: "solo", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("CalculateWithStrategy did not reject a candidate outside the offered set")
		}
	}()
	CalculateWithStrategy(value, rogueCandidateStrategy{})
}

// mutatingStrategyは、受け取ったスライスの先頭要素を書き換えて返す。
// SelectionInputを読み取り専用として扱わず、入力へ書き戻す選択Strategyを模倣する。
type mutatingStrategy struct{}

func (mutatingStrategy) Select(input SelectionInput) AssignmentCandidate {
	input.Candidates[0].WorkerID = "mutated-worker"
	return input.Candidates[0]
}

func TestCalculateWithStrategyRejectsMutatedCandidate(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day},
		Workers:  []input.Worker{{ID: "first", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "solo", Name: "solo", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true},
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("CalculateWithStrategy did not reject a candidate produced by mutating the offered slice")
		}
	}()
	CalculateWithStrategy(value, mutatingStrategy{})
}

func TestCalculateWithStrategyRejectsNilStrategy(t *testing.T) {
	requirePanicMessage(t, "selection strategy must not be nil", func() {
		CalculateWithStrategy(input.Input{}, nil)
	})
}

// continuationOrOtherWorkerStrategyは、有効な継続候補があるタスクでも、
// 他の担当者候補が選択Strategyへ提示され、継続候補以外を選べることを確認する。
type continuationOrOtherWorkerStrategy struct {
	workerID             string
	followCandidateCount int
	sawPendingFollowTask bool
}

func (p *continuationOrOtherWorkerStrategy) Select(selection SelectionInput) AssignmentCandidate {
	count := 0
	for _, candidate := range selection.Candidates {
		if candidate.TaskID == "t2" {
			count++
		}
	}
	p.followCandidateCount = count
	for _, task := range selection.PendingTasks {
		if task.ID == "t2" {
			p.sawPendingFollowTask = true
		}
	}
	for _, candidate := range selection.Candidates {
		if candidate.WorkerID == p.workerID {
			return candidate
		}
	}
	return selection.Candidates[0]
}

func TestCalculateWithStrategyCanChooseOtherWorkerOverAvailableContinuation(t *testing.T) {
	day := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	value := input.Input{
		Settings: input.Settings{StartDate: day, Selection: input.SelectionSettings{DeferNonCore: input.DeferNonCoreSettings{Enabled: true}, FollowContinuity: input.FollowContinuitySettings{Enabled: true}, PreferredWorker: input.PreferredWorkerSettings{DefaultThresholdDays: 3}}},
		Workers:  []input.Worker{{ID: "first", Lanes: []string{"backend"}}, {ID: "second", Lanes: []string{"backend"}}},
		Tasks: []input.Task{
			{ID: "t1", Name: "t1", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true, Assignee: "first"},
			{ID: "t2", Name: "t2", Plan: "p", EffortSlots: 2, Lane: "backend", Core: true, ReleaseBlocking: true, QABlocking: true, Follow: "t1", Dependencies: []string{"t1"}},
		},
	}

	defaultResult := assignmentsByID(Calculate(value).Assignments)
	if defaultResult["t2"].Assignee != "first" {
		t.Fatalf("default strategy did not continue the follow predecessor's worker: %#v", defaultResult["t2"])
	}

	strategy := &continuationOrOtherWorkerStrategy{workerID: "second"}
	injected := assignmentsByID(CalculateWithStrategy(value, strategy).Assignments)
	if injected["t2"].Assignee != "second" {
		t.Fatalf("injected strategy did not pick the alternative worker over the offered continuation: %#v", injected["t2"])
	}
	if strategy.followCandidateCount != 2 || !strategy.sawPendingFollowTask {
		t.Fatalf("strategy saw %d t2 candidates with pending=%v, want two worker candidates and pending task facts", strategy.followCandidateCount, strategy.sawPendingFollowTask)
	}
}

func requirePanicMessage(t *testing.T, want string, run func()) {
	t.Helper()
	defer func() {
		if got := recover(); got != want {
			t.Fatalf("panic = %#v, want %q", got, want)
		}
	}()
	run()
}
