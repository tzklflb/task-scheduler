package scheduling

import "github.com/tzklflb/task-scheduler/internal/input"

// criticalLengths は、各タスクを起点に依存先を辿ったときの工数合計の最大値を返す。
// release_blockingがfalseのタスクも依存グラフに残るため、下流にあると
// 割当対象タスクのCriticalLengthと選択優先度に影響することがある。
func criticalLengths(tasks []input.Task, dependencies map[string][]string) map[string]int {
	byID := map[string]input.Task{}
	children := map[string][]string{}
	for _, task := range tasks {
		byID[task.ID] = task
		children[task.ID] = nil
	}
	for _, task := range tasks {
		for _, dependency := range dependencies[task.ID] {
			if _, exists := children[dependency]; exists {
				children[dependency] = append(children[dependency], task.ID)
			}
		}
	}
	memo := map[string]int{}
	var visit func(string) int
	visit = func(id string) int {
		if value, exists := memo[id]; exists {
			return value
		}
		value := byID[id].EffortSlots
		for _, child := range children[id] {
			candidate := byID[id].EffortSlots + visit(child)
			if candidate > value {
				value = candidate
			}
		}
		memo[id] = value
		return value
	}
	for _, task := range tasks {
		visit(task.ID)
	}
	return memo
}
