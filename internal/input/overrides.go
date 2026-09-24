package input

import (
	"fmt"
	"strings"
	"time"
)

func applyOverrides(value *Input, overrides Overrides) []Issue {
	var issues []Issue
	for _, override := range overrides.Available {
		workerID, dateText, ok := strings.Cut(override, "=")
		if !ok || workerID == "" || dateText == "" {
			issues = append(issues, Issue{"--available", "WORKER=YYYY-MM-DD で指定する"})
			continue
		}
		date, err := time.Parse(dateLayout, dateText)
		if err != nil || date.Format(dateLayout) != dateText {
			issues = append(issues, Issue{"--available", "YYYY-MM-DD として解釈できない"})
			continue
		}
		found := false
		for index := range value.Workers {
			if value.Workers[index].ID == workerID {
				value.Workers[index].AvailableFrom = &date
				value.Settings.Assumptions = append(value.Settings.Assumptions,
					fmt.Sprintf("（このシナリオ試算では %s の参画日を %s に上書き）", workerID, dateText))
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, Issue{"--available", fmt.Sprintf("worker %q が見つからない", workerID)})
		}
	}
	if overrides.AsOf != "" {
		date, err := time.Parse(dateLayout, overrides.AsOf)
		if err != nil || date.Format(dateLayout) != overrides.AsOf {
			issues = append(issues, Issue{"--as-of", "YYYY-MM-DD として解釈できない"})
		} else {
			value.Settings.AsOf = &date
		}
	}
	return issues
}
