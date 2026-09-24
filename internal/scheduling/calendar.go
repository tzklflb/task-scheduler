package scheduling

import "time"

const dateLayout = "2006-01-02"

type calendar struct {
	start    time.Time
	holidays map[string]bool
	halfDays map[string]bool
	reserved map[string]bool
	slots    []time.Time
}

func newCalendar(start time.Time, holidays, halfDays, reserved map[string]bool) *calendar {
	return &calendar{start: start, holidays: holidays, halfDays: halfDays, reserved: reserved}
}

func (c *calendar) ensure(count int) {
	current := c.start
	if len(c.slots) > 0 {
		current = c.slots[len(c.slots)-1].AddDate(0, 0, 1)
	}
	for len(c.slots) < count {
		if c.workday(current) {
			c.slots = append(c.slots, current)
			if !c.halfDays[dateKey(current)] {
				c.slots = append(c.slots, current)
			}
		}
		current = current.AddDate(0, 0, 1)
	}
}

func (c *calendar) dateToSlot(day time.Time) int {
	for len(c.slots) == 0 || c.slots[len(c.slots)-1].Before(day) {
		c.ensure(len(c.slots) + 1)
	}
	for index, slot := range c.slots {
		if !slot.Before(day) {
			return index
		}
	}
	panic("calendar slot was not created")
}

func (c *calendar) workday(day time.Time) bool {
	return day.Weekday() != time.Saturday && day.Weekday() != time.Sunday && !c.holidays[dateKey(day)] && !c.reserved[dateKey(day)]
}

func dateKey(day time.Time) string { return day.Format(dateLayout) }

func pastSpan(end time.Time, effort int, holidays, halfDays map[string]bool) (time.Time, time.Time, bool) {
	day := end
	for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday || holidays[dateKey(day)] {
		day = day.AddDate(0, 0, -1)
	}
	last := day
	remaining := effort
	for {
		available := 2
		if halfDays[dateKey(day)] {
			available = 1
		}
		if available >= remaining {
			return day, last, available == 2 && remaining == 1
		}
		remaining -= available
		day = day.AddDate(0, 0, -1)
		for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday || holidays[dateKey(day)] {
			day = day.AddDate(0, 0, -1)
		}
	}
}
