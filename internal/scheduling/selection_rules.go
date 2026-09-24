package scheduling

import "github.com/tzklflb/task-scheduler/internal/input"

type DeferNonCoreRule struct{ Enabled bool }

func (rule DeferNonCoreRule) Narrow(context RuleContext, candidates []AssignmentCandidate) RuleResult {
	if !rule.Enabled {
		return RuleResult{Candidates: candidates}
	}
	corePending := false
	for _, task := range context.PendingTasks {
		if task.Core {
			corePending = true
			break
		}
	}
	if !corePending {
		return RuleResult{Candidates: candidates}
	}
	var coreCandidates []AssignmentCandidate
	for _, candidate := range candidates {
		if candidate.Core {
			coreCandidates = append(coreCandidates, candidate)
		}
	}
	if len(coreCandidates) == 0 || len(coreCandidates) == len(candidates) {
		if len(coreCandidates) == 0 {
			panic("non-core candidates deferred while core tasks remain")
		}
		return RuleResult{Candidates: candidates, Applied: true}
	}
	return RuleResult{Candidates: coreCandidates, Applied: true}
}

type FollowContinuityRule struct {
	Enabled bool
	Scope   input.SelectionScope
}

func (rule FollowContinuityRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	if !rule.Enabled {
		return RuleResult{Candidates: candidates}
	}
	var continuations []AssignmentCandidate
	for _, candidate := range candidates {
		if rule.Scope.Matches(candidate.TaskID, candidate.Plan, candidate.Lane) && candidate.CanContinueWithoutGap {
			continuations = append(continuations, candidate)
		}
	}
	if len(continuations) == 0 {
		return RuleResult{Candidates: candidates}
	}
	return RuleResult{Candidates: continuations, Applied: true}
}

type TaskOrderRule struct{}

func (TaskOrderRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	minimum := candidates[0].TaskOrder
	for _, candidate := range candidates[1:] {
		minimum = min(minimum, candidate.TaskOrder)
	}
	return RuleResult{Candidates: filterCandidates(candidates, func(candidate AssignmentCandidate) bool { return candidate.TaskOrder == minimum }), Applied: true}
}

type PreferredWorkerRule struct {
	PlanAssignees        map[string]string
	DefaultThresholdDays float64
	PlanThresholdDays    map[string]float64
	Scope                input.SelectionScope
}

func (rule PreferredWorkerRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	if len(candidates) == 0 {
		return RuleResult{Candidates: candidates}
	}
	if !rule.Scope.Matches(candidates[0].TaskID, candidates[0].Plan, candidates[0].Lane) {
		return RuleResult{Candidates: candidates}
	}
	preferredID := candidates[0].FollowWorkerID
	if preferredID == "" {
		preferredID = rule.PlanAssignees[candidates[0].Plan]
	}
	if preferredID == "" {
		return RuleResult{Candidates: candidates}
	}
	thresholdDays := rule.DefaultThresholdDays
	if planThreshold, ok := rule.PlanThresholdDays[candidates[0].Plan]; ok {
		thresholdDays = planThreshold
	}
	thresholdSlots := int(thresholdDays*2 + 0.5)
	minimumStart := candidates[0].StartSlot
	for _, candidate := range candidates[1:] {
		minimumStart = min(minimumStart, candidate.StartSlot)
	}
	for _, candidate := range candidates {
		if candidate.WorkerID == preferredID && candidate.StartSlot-minimumStart <= thresholdSlots {
			return RuleResult{Candidates: []AssignmentCandidate{candidate}, Applied: true}
		}
	}
	return RuleResult{Candidates: candidates}
}

type EarliestWorkerStartRule struct{}

func (EarliestWorkerStartRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	minimum := candidates[0].StartSlot
	for _, candidate := range candidates[1:] {
		minimum = min(minimum, candidate.StartSlot)
	}
	return RuleResult{Candidates: filterCandidates(candidates, func(candidate AssignmentCandidate) bool { return candidate.StartSlot == minimum }), Applied: true}
}

type WorkerOrderRule struct{}

func (WorkerOrderRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	minimum := candidates[0].WorkerOrder
	for _, candidate := range candidates[1:] {
		minimum = min(minimum, candidate.WorkerOrder)
	}
	return RuleResult{Candidates: filterCandidates(candidates, func(candidate AssignmentCandidate) bool { return candidate.WorkerOrder == minimum }), Applied: true}
}

type EarliestTaskStartRule struct{}

func (EarliestTaskStartRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	minimum := candidates[0].StartSlot
	for _, candidate := range candidates[1:] {
		minimum = min(minimum, candidate.StartSlot)
	}
	return RuleResult{Candidates: filterCandidates(candidates, func(candidate AssignmentCandidate) bool { return candidate.StartSlot == minimum }), Applied: true}
}

type CriticalLengthRule struct{}

func (CriticalLengthRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	maximum := candidates[0].CriticalLength
	for _, candidate := range candidates[1:] {
		maximum = max(maximum, candidate.CriticalLength)
	}
	return RuleResult{Candidates: filterCandidates(candidates, func(candidate AssignmentCandidate) bool { return candidate.CriticalLength == maximum }), Applied: true}
}

type TaskIDRule struct{}

func (TaskIDRule) Narrow(_ RuleContext, candidates []AssignmentCandidate) RuleResult {
	minimum := candidates[0].TaskID
	for _, candidate := range candidates[1:] {
		minimum = min(minimum, candidate.TaskID)
	}
	return RuleResult{Candidates: filterCandidates(candidates, func(candidate AssignmentCandidate) bool { return candidate.TaskID == minimum }), Applied: true}
}

func filterCandidates(candidates []AssignmentCandidate, keep func(AssignmentCandidate) bool) []AssignmentCandidate {
	result := make([]AssignmentCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if keep(candidate) {
			result = append(result, candidate)
		}
	}
	if len(result) == 0 {
		panic("selection rule filtered all candidates")
	}
	return result
}
