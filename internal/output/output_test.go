package output

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tzklflb/task-scheduler/internal/input"
	"github.com/tzklflb/task-scheduler/internal/scheduling"
)

func TestBuildNormalizesScheduleStates(t *testing.T) {
	view := genericView(t)
	tasks := map[string]Task{}
	for _, task := range view.Tasks {
		tasks[task.ID] = task
	}
	if tasks["completed"].Source != "actual" || tasks["fixed-qa"].Source != "fixed" || tasks["in-progress"].Source != "planned" {
		t.Fatalf("tasks = %#v", tasks)
	}
	if len(view.ExcludedTasks) != 1 || view.ExcludedTasks[0].ID != "outside" || view.ExcludedTasks[0].Source != "excluded" {
		t.Fatalf("excluded tasks = %#v", view.ExcludedTasks)
	}
	if tasks["completed"].DeliveryScope != DeliveryScopeCore || tasks["later"].DeliveryScope != DeliveryScopeReleaseRequired || view.ExcludedTasks[0].DeliveryScope != DeliveryScopePostRelease {
		t.Fatalf("delivery scopes = tasks:%#v excluded:%#v", tasks, view.ExcludedTasks)
	}
}

func TestBuildOmitsCompletedExcludedTasks(t *testing.T) {
	view := Build(input.Input{Tasks: []input.Task{
		{ID: "completed-outside", Status: "done"},
		{ID: "outside", Assignee: "owner"},
	}}, scheduling.Schedule{})
	if len(view.ExcludedTasks) != 1 || view.ExcludedTasks[0].ID != "outside" {
		t.Fatalf("excluded tasks = %#v", view.ExcludedTasks)
	}
	if view.ExcludedTasks[0].Assignee != "owner" {
		t.Fatalf("excluded assignee = %#v", view.ExcludedTasks[0])
	}
}

func TestDeliveryScopePrioritizesPostRelease(t *testing.T) {
	if got := deliveryScope(input.Task{Core: true, ReleaseBlocking: false}); got != DeliveryScopePostRelease {
		t.Fatalf("deliveryScope() = %q, want %q", got, DeliveryScopePostRelease)
	}
}

func TestBuildCarriesTimelineDisplayContract(t *testing.T) {
	value, issues := input.Load(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"), input.Overrides{})
	if len(issues) > 0 {
		t.Fatalf("Load() issues = %v", issues)
	}
	view := Build(*value, scheduling.Calculate(*value))
	if view.ReleaseDate == nil || *view.ReleaseDate != "2026-02-06" || view.SprintAnchor == nil || *view.SprintAnchor != "2026-01-05" {
		t.Fatalf("milestone fields = %#v", view)
	}
	if view.SprintLengthDays != 14 || len(view.SprintGoals) != 2 || view.SprintGoals[0].Goal == "" {
		t.Fatalf("sprint fields = %#v", view.SprintGoals)
	}
	if len(view.Holidays) == 0 || len(view.HalfDays) == 0 || len(view.Workers) != len(value.Workers) {
		t.Fatalf("calendar and workers = %#v", view)
	}
	if len(view.Plans) == 0 || view.Plans[0] != "delivery" {
		t.Fatalf("plans = %#v", view.Plans)
	}

	generic := genericView(t)
	if generic.Tasks[0].DeliveryScope == "" {
		t.Fatalf("delivery scope = %#v", generic.Tasks)
	}
	foundHalfDayBoundary := false
	for _, task := range generic.Tasks {
		if task.ID == "in-progress" {
			foundHalfDayBoundary = task.EndsAtNoon
		}
	}
	if !foundHalfDayBoundary {
		t.Fatalf("half-day boundary = %#v", generic.Tasks)
	}
}

func TestHTMLProvidesSelfContainedEscapedDocument(t *testing.T) {
	value, issues := input.Load(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"), input.Overrides{})
	if len(issues) > 0 {
		t.Fatalf("Load() issues = %v", issues)
	}
	value.Epic.Name = `悪意 </script><script>window.injected=true</script>`
	value.Tasks[0].Name = `名前 </script><script>window.injected=true</script>`
	html, err := HTML(Build(*value, scheduling.Calculate(*value)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>TEST-42 悪意 &lt;/script&gt;&lt;script&gt;window.injected=true&lt;/script&gt; 開発タイムライン</title>", "<style>", "<script id=\"schedule-data\" type=\"application/json\">", "<script>"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML() does not contain %q", want)
		}
	}
	if strings.Contains(html, "</script><script>window.injected=true</script>") {
		t.Fatalf("HTML() allows embedded data to escape its script element: %s", html)
	}
	payload := htmlPayload(t, html)
	for _, task := range append(embeddedTasks(t, payload.Tasks), embeddedTasks(t, payload.ExcludedTasks)...) {
		if task.DeliveryScope == "" {
			t.Errorf("DeliveryScope is empty for task payload %#v", task)
		}
	}
}

func TestHTMLEncodesEmptyCollectionsAsArrays(t *testing.T) {
	html, err := HTML(Build(input.Input{}, scheduling.Schedule{}))
	if err != nil {
		t.Fatal(err)
	}
	payload := htmlPayload(t, html)
	for name, value := range map[string]json.RawMessage{
		"Assumptions":   payload.Assumptions,
		"Holidays":      payload.Holidays,
		"HalfDays":      payload.HalfDays,
		"SprintGoals":   payload.SprintGoals,
		"BufferDates":   payload.BufferDates,
		"Workers":       payload.Workers,
		"Plans":         payload.Plans,
		"Tasks":         payload.Tasks,
		"ExcludedTasks": payload.ExcludedTasks,
	} {
		if string(value) != "[]" {
			t.Errorf("%s = %s, want []", name, value)
		}
	}
}

func genericView(t *testing.T) View {
	t.Helper()
	value, issues := input.Load(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"), input.Overrides{})
	if len(issues) > 0 {
		t.Fatalf("Load() issues = %v", issues)
	}
	return Build(*value, scheduling.Calculate(*value))
}

type embeddedPayload struct {
	Assumptions   json.RawMessage `json:"Assumptions"`
	Holidays      json.RawMessage `json:"Holidays"`
	HalfDays      json.RawMessage `json:"HalfDays"`
	SprintGoals   json.RawMessage `json:"SprintGoals"`
	BufferDates   json.RawMessage `json:"BufferDates"`
	Workers       json.RawMessage `json:"Workers"`
	Plans         json.RawMessage `json:"Plans"`
	Tasks         json.RawMessage `json:"Tasks"`
	ExcludedTasks json.RawMessage `json:"ExcludedTasks"`
}

type embeddedTask struct {
	DeliveryScope DeliveryScope `json:"DeliveryScope"`
}

func htmlPayload(t *testing.T, html string) embeddedPayload {
	t.Helper()
	const opening = `<script id="schedule-data" type="application/json">`
	start := strings.Index(html, opening)
	if start < 0 {
		t.Fatal("schedule data script is missing")
	}
	start += len(opening)
	end := strings.Index(html[start:], "</script>")
	if end < 0 {
		t.Fatal("schedule data script does not close")
	}
	var payload embeddedPayload
	if err := json.Unmarshal([]byte(html[start:start+end]), &payload); err != nil {
		t.Fatalf("schedule data is not JSON: %v", err)
	}
	return payload
}

func embeddedTasks(t *testing.T, value json.RawMessage) []embeddedTask {
	t.Helper()
	var tasks []embeddedTask
	if err := json.Unmarshal(value, &tasks); err != nil {
		t.Fatalf("tasks payload is not a JSON array: %v", err)
	}
	return tasks
}
