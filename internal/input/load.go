package input

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Load は、制限付きYAMLドキュメントをファイルから読み込み、検証・正規化する。
func Load(path string, overrides Overrides) (*Input, []Issue) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []Issue{{Location: "input", Message: fmt.Sprintf("読み込めません: %v", err)}}
	}
	return LoadBytes(data, overrides)
}

// LoadBytes は、制限付きYAML入力を検証・正規化する。
func LoadBytes(data []byte, overrides Overrides) (*Input, []Issue) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, []Issue{{Location: "input", Message: fmt.Sprintf("YAML を読み込めません: %v", err)}}
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, []Issue{{Location: "input", Message: fmt.Sprintf("YAML を読み込めません: %v", err)}}
		}
		return nil, []Issue{{Location: "input", Message: "YAML document は一つだけ指定する"}}
	}

	value, issues := parseInput(&document)
	if len(issues) > 0 {
		return nil, issues
	}
	issues = append(issues, applyOverrides(&value, overrides)...)
	if len(issues) > 0 {
		return nil, issues
	}
	issues = validateSemantics(value)
	if len(issues) > 0 {
		return nil, issues
	}
	return &value, nil
}

func parseInput(document *yaml.Node) (Input, []Issue) {
	var issues []Issue
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return Input{}, []Issue{{Location: "input", Message: "YAML document は一つの値を持つ必要がある"}}
	}
	root := document.Content[0]
	fields := fieldsOf(root, "input", []string{"epic", "settings", "workers", "tasks"}, &issues)
	value := Input{}
	value.Epic = parseEpic(fields["epic"], "epic", &issues)
	value.Settings = parseSettings(fields["settings"], "settings", &issues)
	value.Workers = parseWorkers(fields["workers"], "workers", &issues)
	value.Tasks = parseTasks(fields["tasks"], "tasks", &issues)
	return value, issues
}

func fieldsOf(node *yaml.Node, location string, allowed []string, issues *[]Issue) map[string]*yaml.Node {
	result := map[string]*yaml.Node{}
	if node == nil || node.Kind != yaml.MappingNode {
		*issues = append(*issues, Issue{location, "mapping で指定する"})
		return result
	}
	allowedSet := map[string]bool{}
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			*issues = append(*issues, Issue{location, "mapping のキーは文字列で指定する"})
			continue
		}
		if _, exists := result[key.Value]; exists {
			*issues = append(*issues, Issue{location, fmt.Sprintf("キー %q が重複している", key.Value)})
			continue
		}
		if !allowedSet[key.Value] {
			*issues = append(*issues, Issue{location, fmt.Sprintf("未知の項目 %q がある", key.Value)})
			continue
		}
		result[key.Value] = value
	}
	return result
}

func required(fields map[string]*yaml.Node, key, location string, issues *[]Issue) *yaml.Node {
	node := fields[key]
	if node == nil {
		*issues = append(*issues, Issue{location + "." + key, "必須項目がない"})
	}
	return node
}

func parseEpic(node *yaml.Node, location string, issues *[]Issue) Epic {
	fields := fieldsOf(node, location, []string{"id", "name"}, issues)
	return Epic{
		ID:   stringValue(required(fields, "id", location, issues), location+".id", true, issues),
		Name: stringValue(required(fields, "name", location, issues), location+".name", true, issues),
	}
}

func parseSettings(node *yaml.Node, location string, issues *[]Issue) Settings {
	allowed := []string{"start_date", "buffer_days", "buffer_allocation", "release_date", "sprint_anchor", "sprint_length_days", "selection", "waves", "sprint_goals", "holidays", "half_days", "as_of", "plan_workers", "assumptions", "core_end_min"}
	fields := fieldsOf(node, location, allowed, issues)
	settings := Settings{
		StartDate:        dateValue(required(fields, "start_date", location, issues), location+".start_date", issues),
		BufferDays:       integerValue(required(fields, "buffer_days", location, issues), location+".buffer_days", issues),
		Holidays:         datesValue(required(fields, "holidays", location, issues), location+".holidays", issues),
		HalfDays:         datesValue(fields["half_days"], location+".half_days", issues),
		BufferAllocation: intMap(fields["buffer_allocation"], location+".buffer_allocation", issues),
		SprintGoals:      sprintGoals(fields["sprint_goals"], location+".sprint_goals", issues),
		Assumptions:      stringsValue(fields["assumptions"], location+".assumptions", issues),
		PlanWorkers:      stringListsMap(fields["plan_workers"], location+".plan_workers", issues),
		Waves:            wavesMap(fields["waves"], location+".waves", issues),
		SprintLengthDays: 14,
		Selection: SelectionSettings{
			DeferNonCore:    DeferNonCoreSettings{Enabled: true},
			PreferredWorker: PreferredWorkerSettings{DefaultThresholdDays: 3, PlanAssignees: map[string]string{}, PlanThresholdDays: map[string]float64{}},
		},
	}
	if node := fields["release_date"]; node != nil {
		settings.ReleaseDate = datePointer(node, location+".release_date", issues)
	}
	if node := fields["sprint_anchor"]; node != nil {
		settings.SprintAnchor = datePointer(node, location+".sprint_anchor", issues)
	}
	if node := fields["as_of"]; node != nil {
		settings.AsOf = datePointer(node, location+".as_of", issues)
	}
	if node := fields["core_end_min"]; node != nil {
		settings.CoreEndMin = datePointer(node, location+".core_end_min", issues)
	}
	if node := fields["sprint_length_days"]; node != nil {
		settings.SprintLengthDays = integerValue(node, location+".sprint_length_days", issues)
	}
	if node := fields["selection"]; node != nil {
		settings.Selection = parseSelection(node, location+".selection", issues)
	}
	return settings
}

func parseSelection(node *yaml.Node, location string, issues *[]Issue) SelectionSettings {
	settings := SelectionSettings{
		DeferNonCore:    DeferNonCoreSettings{Enabled: true},
		PreferredWorker: PreferredWorkerSettings{DefaultThresholdDays: 3, PlanAssignees: map[string]string{}, PlanThresholdDays: map[string]float64{}},
	}
	fields := fieldsOf(node, location, []string{"defer_non_core", "follow_continuity", "preferred_worker"}, issues)
	if item := fields["defer_non_core"]; item != nil {
		values := fieldsOf(item, location+".defer_non_core", []string{"enabled"}, issues)
		if enabled := values["enabled"]; enabled != nil {
			settings.DeferNonCore.Enabled = boolValue(enabled, location+".defer_non_core.enabled", issues)
		}
	}
	if item := fields["follow_continuity"]; item != nil {
		values := fieldsOf(item, location+".follow_continuity", []string{"enabled", "scope"}, issues)
		if enabled := values["enabled"]; enabled != nil {
			settings.FollowContinuity.Enabled = boolValue(enabled, location+".follow_continuity.enabled", issues)
		}
		settings.FollowContinuity.Scope = parseSelectionScope(values["scope"], location+".follow_continuity.scope", issues)
	}
	if item := fields["preferred_worker"]; item != nil {
		values := fieldsOf(item, location+".preferred_worker", []string{"default_threshold_days", "plan_assignees", "plan_threshold_days", "scope"}, issues)
		preferred := &settings.PreferredWorker
		if threshold := values["default_threshold_days"]; threshold != nil {
			preferred.DefaultThresholdDays = numberValue(threshold, location+".preferred_worker.default_threshold_days", issues)
		}
		preferred.PlanAssignees = stringMap(values["plan_assignees"], location+".preferred_worker.plan_assignees", issues)
		preferred.PlanThresholdDays = floatMap(values["plan_threshold_days"], location+".preferred_worker.plan_threshold_days", issues)
		preferred.Scope = parseSelectionScope(values["scope"], location+".preferred_worker.scope", issues)
	}
	return settings
}

func parseSelectionScope(node *yaml.Node, location string, issues *[]Issue) SelectionScope {
	var scope SelectionScope
	if node == nil {
		return scope
	}
	fields := fieldsOf(node, location, []string{"plans", "lanes", "task_ids"}, issues)
	for _, dimension := range []struct {
		key    string
		target *[]string
	}{{"plans", &scope.Plans}, {"lanes", &scope.Lanes}, {"task_ids", &scope.TaskIDs}} {
		key, target := dimension.key, dimension.target
		if value := fields[key]; value != nil {
			parsed := stringsValue(value, location+"."+key, issues)
			if len(parsed) == 0 {
				*issues = append(*issues, Issue{location + "." + key, "明示する配列は空にできない"})
			}
			*target = parsed
		}
	}
	return scope
}

func parseWorkers(node *yaml.Node, location string, issues *[]Issue) []Worker {
	if node == nil || node.Kind != yaml.SequenceNode {
		*issues = append(*issues, Issue{location, "配列で指定する"})
		return nil
	}
	workers := make([]Worker, 0, len(node.Content))
	for index, item := range node.Content {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		fields := fieldsOf(item, itemLocation, []string{"id", "lanes", "available_from"}, issues)
		worker := Worker{
			ID:    stringValue(required(fields, "id", itemLocation, issues), itemLocation+".id", true, issues),
			Lanes: stringsValue(required(fields, "lanes", itemLocation, issues), itemLocation+".lanes", issues),
		}
		if date := fields["available_from"]; date != nil {
			worker.AvailableFrom = datePointer(date, itemLocation+".available_from", issues)
		}
		workers = append(workers, worker)
	}
	return workers
}

func parseTasks(node *yaml.Node, location string, issues *[]Issue) []Task {
	if node == nil || node.Kind != yaml.SequenceNode {
		*issues = append(*issues, Issue{location, "配列で指定する"})
		return nil
	}
	tasks := make([]Task, 0, len(node.Content))
	for index, item := range node.Content {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		allowed := []string{"id", "name", "plan", "effort", "deps", "lane", "assignee", "follow", "release_blocking", "not_before", "core", "qa_blocking", "start_on", "end_on", "status", "done_on"}
		fields := fieldsOf(item, itemLocation, allowed, issues)
		effort := numberValue(required(fields, "effort", itemLocation, issues), itemLocation+".effort", issues)
		if effort <= 0 {
			*issues = append(*issues, Issue{itemLocation + ".effort", "0 より大きく指定する"})
		}
		if math.Abs(effort*2-math.Round(effort*2)) >= 1e-9 {
			*issues = append(*issues, Issue{itemLocation + ".effort", "0.5 人日単位で指定する"})
		}
		task := Task{
			ID:              stringValue(required(fields, "id", itemLocation, issues), itemLocation+".id", true, issues),
			Name:            stringValue(required(fields, "name", itemLocation, issues), itemLocation+".name", true, issues),
			Plan:            stringValue(required(fields, "plan", itemLocation, issues), itemLocation+".plan", true, issues),
			EffortSlots:     int(math.Round(effort * 2)),
			Dependencies:    stringsValue(required(fields, "deps", itemLocation, issues), itemLocation+".deps", issues),
			Lane:            stringValue(required(fields, "lane", itemLocation, issues), itemLocation+".lane", true, issues),
			Core:            true,
			ReleaseBlocking: true,
			QABlocking:      true,
		}
		if field := fields["assignee"]; field != nil {
			task.Assignee = stringValue(field, itemLocation+".assignee", false, issues)
		}
		if field := fields["follow"]; field != nil {
			task.Follow = stringValue(field, itemLocation+".follow", false, issues)
		}
		if field := fields["status"]; field != nil {
			task.Status = stringValue(field, itemLocation+".status", false, issues)
			task.StatusSpecified = true
		}
		if field := fields["core"]; field != nil {
			task.Core = boolValue(field, itemLocation+".core", issues)
		}
		if field := fields["release_blocking"]; field != nil {
			task.ReleaseBlocking = boolValue(field, itemLocation+".release_blocking", issues)
		}
		if field := fields["qa_blocking"]; field != nil {
			task.QABlocking = boolValue(field, itemLocation+".qa_blocking", issues)
		}
		for _, dateField := range []struct {
			name   string
			target **time.Time
		}{{"start_on", &task.StartOn}, {"end_on", &task.EndOn}, {"not_before", &task.NotBefore}, {"done_on", &task.DoneOn}} {
			if field := fields[dateField.name]; field != nil {
				*dateField.target = datePointer(field, itemLocation+"."+dateField.name, issues)
			}
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func stringValue(node *yaml.Node, location string, nonempty bool, issues *[]Issue) string {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		*issues = append(*issues, Issue{location, "文字列で指定する"})
		return ""
	}
	if nonempty && strings.TrimSpace(node.Value) == "" {
		*issues = append(*issues, Issue{location, "空でない文字列で指定する"})
	}
	return node.Value
}

func numberValue(node *yaml.Node, location string, issues *[]Issue) float64 {
	if node == nil || node.Kind != yaml.ScalarNode || (node.Tag != "!!int" && node.Tag != "!!float") {
		*issues = append(*issues, Issue{location, "number で指定する"})
		return 0
	}
	value, err := strconv.ParseFloat(node.Value, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		*issues = append(*issues, Issue{location, "有限の number で指定する"})
		return 0
	}
	return value
}

func integerValue(node *yaml.Node, location string, issues *[]Issue) int {
	value := numberValue(node, location, issues)
	if math.Trunc(value) != value {
		*issues = append(*issues, Issue{location, "整数で指定する"})
		return 0
	}
	return int(value)
}

func boolValue(node *yaml.Node, location string, issues *[]Issue) bool {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		*issues = append(*issues, Issue{location, "boolean で指定する"})
		return false
	}
	value, err := strconv.ParseBool(node.Value)
	if err != nil {
		*issues = append(*issues, Issue{location, "boolean で指定する"})
	}
	return value
}

func dateValue(node *yaml.Node, location string, issues *[]Issue) time.Time {
	value := datePointer(node, location, issues)
	if value == nil {
		return time.Time{}
	}
	return *value
}

func datePointer(node *yaml.Node, location string, issues *[]Issue) *time.Time {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" || node.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) == 0 {
		*issues = append(*issues, Issue{location, "引用した YYYY-MM-DD 文字列で指定する"})
		return nil
	}
	value, err := time.Parse(dateLayout, node.Value)
	if err != nil || value.Format(dateLayout) != node.Value {
		*issues = append(*issues, Issue{location, "YYYY-MM-DD として解釈できない"})
		return nil
	}
	return &value
}

func stringsValue(node *yaml.Node, location string, issues *[]Issue) []string {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		*issues = append(*issues, Issue{location, "文字列の配列で指定する"})
		return nil
	}
	values := make([]string, 0, len(node.Content))
	for index, item := range node.Content {
		values = append(values, stringValue(item, fmt.Sprintf("%s[%d]", location, index), false, issues))
	}
	return values
}

func datesValue(node *yaml.Node, location string, issues *[]Issue) []time.Time {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		*issues = append(*issues, Issue{location, "日付の配列で指定する"})
		return nil
	}
	values := make([]time.Time, 0, len(node.Content))
	for index, item := range node.Content {
		values = append(values, dateValue(item, fmt.Sprintf("%s[%d]", location, index), issues))
	}
	return values
}

func stringMap(node *yaml.Node, location string, issues *[]Issue) map[string]string {
	return mapValues(node, location, issues, func(value *yaml.Node, valueLocation string) string {
		return stringValue(value, valueLocation, false, issues)
	})
}

func intMap(node *yaml.Node, location string, issues *[]Issue) map[string]int {
	return mapValues(node, location, issues, func(value *yaml.Node, valueLocation string) int {
		return integerValue(value, valueLocation, issues)
	})
}

func floatMap(node *yaml.Node, location string, issues *[]Issue) map[string]float64 {
	return mapValues(node, location, issues, func(value *yaml.Node, valueLocation string) float64 {
		return numberValue(value, valueLocation, issues)
	})
}

func mapValues[T any](node *yaml.Node, location string, issues *[]Issue, parse func(*yaml.Node, string) T) map[string]T {
	values := map[string]T{}
	if node == nil {
		return values
	}
	if node.Kind != yaml.MappingNode {
		*issues = append(*issues, Issue{location, "mapping で指定する"})
		return values
	}
	seen := map[string]bool{}
	for index := 0; index < len(node.Content); index += 2 {
		key := stringValue(node.Content[index], location, false, issues)
		if seen[key] {
			*issues = append(*issues, Issue{location, fmt.Sprintf("キー %q が重複している", key)})
			continue
		}
		seen[key] = true
		values[key] = parse(node.Content[index+1], location+"."+key)
	}
	return values
}

func stringListsMap(node *yaml.Node, location string, issues *[]Issue) map[string][]string {
	return mapValues(node, location, issues, func(value *yaml.Node, valueLocation string) []string {
		return stringsValue(value, valueLocation, issues)
	})
}

func wavesMap(node *yaml.Node, location string, issues *[]Issue) map[string][][]string {
	return mapValues(node, location, issues, func(value *yaml.Node, valueLocation string) [][]string {
		if value.Kind != yaml.SequenceNode {
			*issues = append(*issues, Issue{valueLocation, "タスク ID 配列の配列で指定する"})
			return nil
		}
		waves := make([][]string, 0, len(value.Content))
		for index, wave := range value.Content {
			waves = append(waves, stringsValue(wave, fmt.Sprintf("%s[%d]", valueLocation, index), issues))
		}
		return waves
	})
}

func sprintGoals(node *yaml.Node, location string, issues *[]Issue) []SprintGoal {
	if node == nil {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		*issues = append(*issues, Issue{location, "配列で指定する"})
		return nil
	}
	values := make([]SprintGoal, 0, len(node.Content))
	for index, item := range node.Content {
		itemLocation := fmt.Sprintf("%s[%d]", location, index)
		fields := fieldsOf(item, itemLocation, []string{"end", "goal"}, issues)
		values = append(values, SprintGoal{
			End:  dateValue(required(fields, "end", itemLocation, issues), itemLocation+".end", issues),
			Goal: stringValue(required(fields, "goal", itemLocation, issues), itemLocation+".goal", true, issues),
		})
	}
	return values
}
