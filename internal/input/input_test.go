package input

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadGenericFixture(t *testing.T) {
	value, issues := Load(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"), Overrides{})
	if len(issues) > 0 {
		t.Fatalf("Load() issues = %v", issues)
	}
	if value.Epic.ID != "TEST-42" || len(value.Tasks) != 12 {
		t.Fatalf("Load() = %+v", value)
	}
	if value.Tasks[0].EffortSlots != 2 {
		t.Fatalf("EffortSlots = %d, want 2", value.Tasks[0].EffortSlots)
	}
	if got := value.Settings.StartDate.Format(dateLayout); got != "2026-01-05" {
		t.Fatalf("StartDate = %q", got)
	}
}

func TestLoadRejectsOverlappingFixedWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedule.yaml")
	content := `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: [], half_days: ["2026-01-06"]}
workers: [{id: a, lanes: [backend]}]
tasks:
  - {id: z, name: derived, plan: p, effort: 1.5, deps: [], lane: backend, assignee: a, start_on: "2026-01-05"}
  - {id: a, name: fixed, plan: p, effort: 1, deps: [], lane: backend, assignee: a, start_on: "2026-01-06", end_on: "2026-01-06"}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, issues := Load(path, Overrides{})
	if len(issues) != 1 || issues[0].String() != `tasks.a.start_on: worker "a" の固定期間が tasks.z と重複している` {
		t.Fatalf("Load() issues = %v", issues)
	}
}

func TestLoadRejectsRestrictedYAML(t *testing.T) {
	for name, input := range map[string]string{
		"unknown key": `epic: {id: TEST, name: test, extra: no}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks: [{id: t, name: test, plan: p, effort: 1, deps: [], lane: be}]
`,
		"duplicate key": `epic: {id: TEST, id: AGAIN, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks: [{id: t, name: test, plan: p, effort: 1, deps: [], lane: be}]
`,
		"duplicate arbitrary map key": `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: 2, buffer_allocation: {"2026-01": 1, "2026-01": 1}, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks: [{id: t, name: test, plan: p, effort: 1, deps: [], lane: be}]
`,
		"unquoted date": `epic: {id: TEST, name: test}
settings: {start_date: 2026-01-05, buffer_days: 0, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks: [{id: t, name: test, plan: p, effort: 1, deps: [], lane: be}]
`,
		"wrong type": `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: zero, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks: [{id: t, name: test, plan: p, effort: 1, deps: [], lane: be}]
`,
	} {
		t.Run(name, func(t *testing.T) {
			_, issues := loadText(t, input)
			if len(issues) == 0 {
				t.Fatal("Load() succeeded")
			}
		})
	}
}

func TestLoadReturnsIndependentSemanticProblems(t *testing.T) {
	_, issues := loadText(t, `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: []}
workers: [{id: a, lanes: [be]}, {id: a, lanes: [be]}]
tasks:
  - {id: t1, name: one, plan: p, effort: 1, deps: [missing], lane: ios}
  - {id: t2, name: two, plan: p, effort: 1, deps: [], lane: be, status: done}
  - {id: t3, name: three, plan: p, effort: 1, deps: [], lane: be, status: ""}
`)
	joined := issuesText(issues)
	for _, want := range []string{"worker ID", "依存", "lane", "done_on", "tasks.t3.status", "settings.as_of"} {
		if !strings.Contains(joined, want) {
			t.Errorf("issues = %s, want %q", joined, want)
		}
	}
}

func TestLoadExplainsCoreTaskDependencyWithoutMisclassifyingReleaseScope(t *testing.T) {
	_, issues := loadText(t, `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: []}
workers: [{id: a, lanes: [be]}]
tasks:
  - {id: core, name: core, plan: p, effort: 1, deps: [outside], lane: be}
  - {id: outside, name: outside, plan: p, effort: 1, deps: [], lane: be, core: false, release_blocking: false}
`)
	if got := issuesText(issues); !strings.Contains(got, "tasks.core.deps: コア対象タスクはコア外タスクに依存できない") {
		t.Fatalf("issues = %s", got)
	}
}

func TestLoadSemanticIssueOrderIsStable(t *testing.T) {
	input := `epic: {id: TEST, name: test}
settings:
  start_date: "2026-01-05"
  buffer_days: 3
  buffer_allocation: {z: -1, a: -2}
  holidays: []
  plan_assignees: {z: missing-z, a: missing-a}
  plan_workers: {z: [], a: []}
  plan_prefer_threshold_days: {z: -1, a: -2}
  waves: {z: [[missing-z]], a: [[missing-a]]}
workers: [{id: contributor, lanes: [development]}]
tasks: [{id: task, name: task, plan: plan, effort: 1, deps: [], lane: development}]
`
	_, issues := loadText(t, input)
	want := issuesText(issues)
	if want == "" {
		t.Fatal("Load() succeeded")
	}
	for range 20 {
		_, issues := loadText(t, input)
		if got := issuesText(issues); got != want {
			t.Fatalf("issue order changed:\nwant: %s\n got: %s", want, got)
		}
	}
}

func TestLoadAppliesOverridesBeforeSemanticValidation(t *testing.T) {
	value, issues := Load(filepath.Join("..", "..", "testdata", "generic-epic", "schedule.yaml"), Overrides{
		Available: []string{"contributor=2026-01-08"},
		AsOf:      "2026-01-07",
	})
	if len(issues) > 0 {
		t.Fatalf("Load() issues = %v", issues)
	}
	if got := value.Workers[0].AvailableFrom.Format(dateLayout); got != "2026-01-08" {
		t.Fatalf("AvailableFrom = %q", got)
	}
	if got := value.Settings.Assumptions; len(got) != 1 || got[0] != "（このシナリオ試算では contributor の参画日を 2026-01-08 に上書き）" {
		t.Fatalf("Assumptions = %#v", got)
	}
	if got := value.Settings.AsOf.Format(dateLayout); got != "2026-01-07" {
		t.Fatalf("AsOf = %q", got)
	}
}

func TestSelectionSettingsDefaultsAndEnabledValues(t *testing.T) {
	base := `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: []}
workers: [{id: worker, lanes: [backend]}]
tasks: [{id: task, name: task, plan: plan, effort: 1, deps: [], lane: backend}]
`
	value, issues := loadText(t, base)
	if len(issues) != 0 {
		t.Fatalf("default load issues = %v", issues)
	}
	selection := value.Settings.Selection
	if !selection.DeferNonCore.Enabled || selection.FollowContinuity.Enabled || selection.PreferredWorker.DefaultThresholdDays != 3 || len(selection.PreferredWorker.PlanAssignees) != 0 || len(selection.PreferredWorker.PlanThresholdDays) != 0 || !selection.PreferredWorker.Scope.Matches("task", "plan", "backend") {
		t.Fatalf("default selection = %#v", selection)
	}
	configured := strings.Replace(base, "settings: {", "settings: {selection: {defer_non_core: {enabled: false}, follow_continuity: {enabled: true}, preferred_worker: {default_threshold_days: 0}}, ", 1)
	value, issues = loadText(t, configured)
	if len(issues) != 0 {
		t.Fatalf("configured load issues = %v", issues)
	}
	if value.Settings.Selection.DeferNonCore.Enabled || !value.Settings.Selection.FollowContinuity.Enabled || value.Settings.Selection.PreferredWorker.DefaultThresholdDays != 0 {
		t.Fatalf("explicit selection values were not retained: %#v", value.Settings.Selection)
	}
}

func TestSelectionScopeMatchesAndOr(t *testing.T) {
	scope := SelectionScope{Plans: []string{"p1", "p2"}, Lanes: []string{"backend", "mobile"}, TaskIDs: []string{"t1", "t2"}}
	for _, test := range []struct {
		id, plan, lane string
		want           bool
	}{
		{"t1", "p2", "mobile", true},
		{"t1", "other", "backend", false},
		{"other", "p1", "backend", false},
		{"t2", "p1", "other", false},
	} {
		if got := scope.Matches(test.id, test.plan, test.lane); got != test.want {
			t.Errorf("Matches(%q, %q, %q) = %v, want %v", test.id, test.plan, test.lane, got, test.want)
		}
	}
}

func TestLoadRejectsInvalidSelectionSettings(t *testing.T) {
	base := `epic: {id: TEST, name: test}
settings:
  start_date: "2026-01-05"
  buffer_days: 0
  holidays: []
  selection: %s
workers: [{id: worker, lanes: [backend]}]
tasks: [{id: task, name: task, plan: plan, effort: 1, deps: [], lane: backend}]
`
	for name, selection := range map[string]string{
		"empty plans":                "{follow_continuity: {scope: {plans: []}}}",
		"empty lanes":                "{preferred_worker: {scope: {lanes: []}}}",
		"empty task ids":             "{preferred_worker: {scope: {task_ids: []}}}",
		"unknown task":               "{follow_continuity: {scope: {task_ids: [missing]}}}",
		"unknown plan":               "{follow_continuity: {scope: {plans: [missing]}}}",
		"unknown lane":               "{follow_continuity: {scope: {lanes: [missing]}}}",
		"undefined worker":           "{preferred_worker: {plan_assignees: {plan: missing}}}",
		"negative default threshold": "{preferred_worker: {default_threshold_days: -1}}",
		"negative plan threshold":    "{preferred_worker: {plan_threshold_days: {plan: -1}}}",
		"unknown plan key":           "{preferred_worker: {plan_threshold_days: {missing: 1}}}",
		"unknown selection key":      "{follow_continuity: {mystery: true}}",
		"defer scope forbidden":      "{defer_non_core: {enabled: true, scope: {plans: [plan]}}}",
	} {
		t.Run(name, func(t *testing.T) {
			_, issues := loadText(t, fmt.Sprintf(base, selection))
			if len(issues) == 0 {
				t.Fatal("Load() succeeded")
			}
		})
	}
}

func TestLoadRejectsScopeLaneUsedOnlyByWorker(t *testing.T) {
	_, issues := loadText(t, `epic: {id: TEST, name: test}
settings:
  start_date: "2026-01-05"
  buffer_days: 0
  holidays: []
  selection:
    follow_continuity:
      scope: {lanes: [frontend]}
workers: [{id: worker, lanes: [backend, frontend]}]
tasks: [{id: task, name: task, plan: plan, effort: 1, deps: [], lane: backend}]
`)
	if got := issuesText(issues); !strings.Contains(got, `settings.selection.follow_continuity.scope.lanes: lane "frontend" が未定義`) {
		t.Fatalf("Load() issues = %s, want scope lane to be rejected because no task uses it", got)
	}
}

func TestLoadRejectsLegacySelectionSettings(t *testing.T) {
	for _, old := range []string{"follow_continuity: true", "plan_assignees: {plan: worker}", "prefer_threshold_days: 3", "plan_prefer_threshold_days: {plan: 1}"} {
		_, issues := loadText(t, `epic: {id: TEST, name: test}
settings: {start_date: "2026-01-05", buffer_days: 0, holidays: [], `+old+`}
workers: [{id: worker, lanes: [backend]}]
tasks: [{id: task, name: task, plan: plan, effort: 1, deps: [], lane: backend}]
`)
		if len(issues) == 0 {
			t.Errorf("legacy setting %q was accepted", old)
		}
	}
}

func loadText(t *testing.T, content string) (*Input, []Issue) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schedule.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path, Overrides{})
}

func issuesText(issues []Issue) string {
	values := make([]string, 0, len(issues))
	for _, issue := range issues {
		values = append(values, issue.String())
	}
	return strings.Join(values, "\n")
}
