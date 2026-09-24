package output

import "encoding/json"

// Summary は、フィールド名を固定した外部連携用のJSONを返す。assignmentsは
// 並べ替えを行わずview.Tasksと同じ順序で並び、statusが未設定のタスクはnullになる。
func Summary(view View) (string, error) {
	type assignment struct {
		ID       string  `json:"id"`
		Assignee string  `json:"assignee"`
		Start    string  `json:"start"`
		End      string  `json:"end"`
		Status   *string `json:"status"`
	}
	value := struct {
		Assignments          []assignment `json:"assignments"`
		CoreEnd              string       `json:"core_end"`
		CompletionWithBuffer string       `json:"completion_with_buffer"`
		RemainingEffort      float64      `json:"remaining_effort"`
	}{
		CoreEnd:              view.CoreEnd,
		CompletionWithBuffer: view.CompletionWithBuffer,
		RemainingEffort:      view.RemainingEffort,
	}
	for _, task := range view.Tasks {
		value.Assignments = append(value.Assignments, assignment{
			ID: task.ID, Assignee: task.Assignee, Start: *task.Start, End: *task.End, Status: task.Status,
		})
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
