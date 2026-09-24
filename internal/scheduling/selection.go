package scheduling

// AssignmentCandidateは、Schedulerが実行可能と判定したタスクと担当者の組み合わせを表す。
// 各値は選択判断に使う事実であり、Ruleの結論を含まない。
type AssignmentCandidate struct {
	TaskID                string
	Plan                  string
	Lane                  string
	Core                  bool
	WorkerID              string
	StartSlot             int
	CriticalLength        int
	TaskOrder             int
	WorkerOrder           int
	FollowWorkerID        string
	CanContinueWithoutGap bool
}

// TaskFactsは、まだ割り当てられていないタスクの選択に必要な事実を表す。
type TaskFacts struct {
	ID    string
	Plan  string
	Lane  string
	Core  bool
	Order int
}

// SelectionInputは、次の割当候補と未割当タスクの事実をStrategyへ渡す。
// Strategyは入力を変更せず、提示された候補から一件を選ぶ。
type SelectionInput struct {
	Candidates   []AssignmentCandidate
	PendingTasks []TaskFacts
}

// SelectionStrategyは、Schedulerが提示した候補から次に反映する一件を選ぶ。
type SelectionStrategy interface {
	Select(SelectionInput) AssignmentCandidate
}

// RuleContextは、Ruleがcoreタスクの残存などを判断するための未割当タスク情報を持つ。
type RuleContext struct {
	PendingTasks []TaskFacts
}

// RuleResultは、Rule適用後の候補と、そのRuleの対象があり分岐を採用したかを返す。
// Appliedは候補数が減ったかどうかを表さない。
type RuleResult struct {
	Candidates []AssignmentCandidate
	Applied    bool
}

// SelectionRuleは、候補を変更・追加せず、入力候補の空でない部分集合へ絞る。
type SelectionRule interface {
	Narrow(RuleContext, []AssignmentCandidate) RuleResult
}

// DefaultSelectionStrategyは、core後回し、follow短絡、タスクごとの担当者選択、
// タスク間比較の固定四段階と適用順を所有する。
type DefaultSelectionStrategy struct {
	deferNonCore     DeferNonCoreRule
	followContinuity FollowContinuityRule
	preferredWorker  PreferredWorkerRule
}

func (strategy DefaultSelectionStrategy) Select(selection SelectionInput) AssignmentCandidate {
	if len(selection.Candidates) == 0 {
		panic("selection strategy requires at least one candidate")
	}
	context := RuleContext{PendingTasks: selection.PendingTasks}
	candidates := applySelectionRule(strategy.deferNonCore, context, selection.Candidates).Candidates
	follow := applySelectionRule(strategy.followContinuity, context, candidates)
	if follow.Applied {
		return chooseOne(applySelectionRule(TaskOrderRule{}, context, follow.Candidates).Candidates)
	}

	candidates = narrowEachTask(context, candidates, strategy.preferredWorker)
	candidates = narrowEachTask(context, candidates, EarliestWorkerStartRule{})
	candidates = narrowEachTask(context, candidates, WorkerOrderRule{})
	candidates = applySelectionRule(EarliestTaskStartRule{}, context, candidates).Candidates
	candidates = applySelectionRule(CriticalLengthRule{}, context, candidates).Candidates
	candidates = applySelectionRule(TaskIDRule{}, context, candidates).Candidates
	return chooseOne(candidates)
}

func narrowEachTask(context RuleContext, candidates []AssignmentCandidate, rule SelectionRule) []AssignmentCandidate {
	order := make([]string, 0, len(candidates))
	groups := make(map[string][]AssignmentCandidate, len(candidates))
	for _, candidate := range candidates {
		if _, exists := groups[candidate.TaskID]; !exists {
			order = append(order, candidate.TaskID)
		}
		groups[candidate.TaskID] = append(groups[candidate.TaskID], candidate)
	}

	result := make([]AssignmentCandidate, 0, len(candidates))
	for _, taskID := range order {
		result = append(result, applySelectionRule(rule, context, groups[taskID]).Candidates...)
	}
	return result
}

func applySelectionRule(rule SelectionRule, context RuleContext, candidates []AssignmentCandidate) RuleResult {
	// Ruleが引数のスライスを書き換えても候補を汚さず、返却値を元の集合と照合する。
	result := rule.Narrow(context, append([]AssignmentCandidate(nil), candidates...))
	if len(result.Candidates) == 0 {
		panic("selection rule returned no candidates")
	}
	available := make(map[AssignmentCandidate]int, len(candidates))
	for _, candidate := range candidates {
		available[candidate]++
	}
	for _, candidate := range result.Candidates {
		if available[candidate] == 0 {
			panic("selection rule returned a candidate outside the offered set")
		}
		available[candidate]--
	}
	return result
}

func chooseOne(candidates []AssignmentCandidate) AssignmentCandidate {
	if len(candidates) != 1 {
		panic("selection rules did not narrow candidates to one")
	}
	return candidates[0]
}
