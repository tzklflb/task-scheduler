package input

import (
	"sort"
	"time"
)

// EffectiveDependencies は、明示された依存に、同じplanの先行waveに属する
// 全タスクを追加する。同じwave内のタスク同士は依存にしない。
func EffectiveDependencies(tasks []Task, waves map[string][][]string) map[string][]string {
	result := make(map[string][]string, len(tasks))
	for _, task := range tasks {
		seen := map[string]bool{}
		for _, dependency := range task.Dependencies {
			seen[dependency] = true
		}
		for _, wave := range waves[task.Plan] {
			found := false
			for _, taskID := range wave {
				if taskID == task.ID {
					found = true
					break
				}
			}
			if found {
				break
			}
			for _, taskID := range wave {
				seen[taskID] = true
			}
		}
		for dependency := range seen {
			result[task.ID] = append(result[task.ID], dependency)
		}
		sort.Strings(result[task.ID])
	}
	return result
}

// FixedTaskEndDate は、固定タスクの明示終了日または工数から求めた終了日を返す。
// task.EndOnが未設定の場合、呼び出し元はtask.StartOnが設定済みであることを保証する。
func FixedTaskEndDate(task Task, holidays, halfDays map[string]bool) time.Time {
	if task.EndOn != nil {
		return *task.EndOn
	}
	remaining := task.EffortSlots
	for day := *task.StartOn; ; day = day.AddDate(0, 0, 1) {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday || holidays[day.Format(dateLayout)] {
			continue
		}
		remaining -= 2
		if halfDays[day.Format(dateLayout)] {
			remaining++
		}
		if remaining <= 0 {
			return day
		}
	}
}

func DateSet(dates []time.Time) map[string]bool {
	result := make(map[string]bool, len(dates))
	for _, date := range dates {
		result[date.Format(dateLayout)] = true
	}
	return result
}
