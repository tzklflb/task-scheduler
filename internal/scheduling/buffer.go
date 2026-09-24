package scheduling

import (
	"fmt"
	"sort"
	"time"

	"github.com/tzklflb/task-scheduler/internal/input"
)

func reservePeriodDays(settings input.Settings, lowerBound time.Time, holidays, halfDays map[string]bool) (map[string]bool, int) {
	reserved := map[string]bool{}
	tail := settings.BufferAllocation["tail"]
	keys := make([]string, 0, len(settings.BufferAllocation))
	for key := range settings.BufferAllocation {
		if key != "tail" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return reserved, tail
	}
	sort.Strings(keys)
	if keys[0][0] == 'S' {
		quotas := map[int]int{}
		firstNumber, lastNumber := 0, 0
		for index, key := range keys {
			var sprintNumber int
			fmt.Sscanf(key, "S%d", &sprintNumber)
			quotas[sprintNumber] = settings.BufferAllocation[key]
			if index == 0 || sprintNumber < firstNumber {
				firstNumber = sprintNumber
			}
			if index == 0 || sprintNumber > lastNumber {
				lastNumber = sprintNumber
			}
		}
		carry := 0
		for number := firstNumber; number <= lastNumber || carry > 0; number++ {
			quota := quotas[number] + carry
			start, end := sprintSpan(settings, number)
			if start.Before(lowerBound) {
				start = lowerBound
			}
			workdays := workdaysBetween(start, end, holidays, halfDays, nil)
			take := quota
			if take > len(workdays) {
				take = len(workdays)
			}
			for _, day := range workdays[len(workdays)-take:] {
				reserved[dateKey(day)] = true
			}
			carry = quota - take
		}
		return reserved, tail
	}
	current := keys[0]
	carry := 0
	for index := 0; index < len(keys) || carry > 0; index++ {
		quota := carry
		if index < len(keys) {
			current = keys[index]
			quota += settings.BufferAllocation[current]
		} else {
			parsed, _ := time.Parse("2006-01", current)
			current = parsed.AddDate(0, 1, 0).Format("2006-01")
		}
		monthStart, _ := time.Parse("2006-01", current)
		monthEnd := monthStart.AddDate(0, 1, -1)
		start := monthStart
		if start.Before(lowerBound) {
			start = lowerBound
		}
		workdays := workdaysBetween(start, monthEnd, holidays, halfDays, nil)
		take := quota
		if take > len(workdays) {
			take = len(workdays)
		}
		for _, day := range workdays[len(workdays)-take:] {
			reserved[dateKey(day)] = true
		}
		carry = quota - take
	}
	return reserved, tail
}

func sprintSpan(settings input.Settings, number int) (time.Time, time.Time) {
	if number <= len(settings.SprintGoals) {
		start := settings.StartDate
		if number > 1 {
			start = settings.SprintGoals[number-2].End.AddDate(0, 0, 1)
		}
		return start, settings.SprintGoals[number-1].End
	}
	last := settings.SprintGoals[len(settings.SprintGoals)-1].End
	start := last.AddDate(0, 0, 1+(number-len(settings.SprintGoals)-1)*settings.SprintLengthDays)
	return start, start.AddDate(0, 0, settings.SprintLengthDays-1)
}

func workdaysBetween(start, end time.Time, holidays, halfDays, reserved map[string]bool) []time.Time {
	var result []time.Time
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday && !holidays[dateKey(day)] && !halfDays[dateKey(day)] && (reserved == nil || !reserved[dateKey(day)]) {
			result = append(result, day)
		}
	}
	return result
}

func reserveTailDays(start time.Time, quota int, holidays, halfDays, reserved map[string]bool) []time.Time {
	var result []time.Time
	for day := start; len(result) < quota; day = day.AddDate(0, 0, 1) {
		if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday && !holidays[dateKey(day)] && !halfDays[dateKey(day)] && !reserved[dateKey(day)] {
			result = append(result, day)
		}
	}
	return result
}
