package yarn

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrBadDate is returned when a date cannot be read.
var ErrBadDate = errors.New("unreadable date")

// dateLayouts are the forms accepted from a form field or a CSV cell. ISO is
// tried first; the day-first forms are the ones written by hand on Indian and
// Philippine mill paperwork. Month-first (US) order is deliberately not
// accepted: "03/04/2026" would be a silent six-week error.
var dateLayouts = []string{"2006-01-02", "2/1/2006", "2-1-2006"}

// isoDate is the one format this tool writes.
const isoDate = "2006-01-02"

// Day truncates a timestamp to its calendar date at UTC midnight. Every date
// in the book is stored this way: a dye-lot is received on a day, not at an
// instant, and anchoring to UTC keeps day arithmetic free of timezone and
// daylight-saving drift.
func Day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ParseDate reads a date as "2026-11-24", "24/11/2026" or "24-11-2026".
func ParseDate(s string) (time.Time, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return time.Time{}, fmt.Errorf("%w: empty", ErrBadDate)
	}
	for _, layout := range dateLayouts {
		if v, err := time.Parse(layout, t); err == nil {
			return Day(v), nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: %q, expected YYYY-MM-DD or DD/MM/YYYY", ErrBadDate, s)
}

// FormatDate renders a date as YYYY-MM-DD.
func FormatDate(t time.Time) string { return Day(t).Format(isoDate) }

// DaysBetween counts whole calendar days from one date to another. It counts
// real days, so it crosses a leap day or a 31-day month correctly:
// 2024-02-28 to 2024-03-01 is 2 days, but 2023-02-28 to 2023-03-01 is 1.
func DaysBetween(from, to time.Time) int {
	return int(Day(to).Sub(Day(from)).Hours() / 24)
}
