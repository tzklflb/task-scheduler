// scheduling パッケージは、検証済みの input.Input から実行可能なスケジュールを計算する。
// 時間は半日を1スロットとする整数で扱い、通常の営業日は2スロット、半休日は1スロットとして数える。
package scheduling

import "time"

// Assignment は、計画または実績として確定した一つのタスク期間を表す。
type Assignment struct {
	ID                string
	Assignee          string
	Start             time.Time
	End               time.Time
	Status            string
	StartSlot         int
	EndSlot           int
	StartsInAfternoon bool
	EndsAtNoon        bool
	Fixed             bool
}

// Schedule は、完成したタスク割当とその集計値を表す。
type Schedule struct {
	Assignments          []Assignment
	BufferDates          []time.Time
	TailBufferDates      []time.Time
	CoreEnd              time.Time
	CompletionWithBuffer time.Time
	RemainingEffort      float64
}
