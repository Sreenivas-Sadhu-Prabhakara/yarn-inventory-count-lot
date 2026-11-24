package yarn

import (
	"errors"
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"ISO", "2026-11-24", "2026-11-24", false},
		{"day first with slashes", "24/11/2026", "2026-11-24", false},
		{"day first with dashes", "24-11-2026", "2026-11-24", false},
		{"single digit day and month", "4/2/2026", "2026-02-04", false},
		{"padded day first", "04/02/2026", "2026-02-04", false},
		{"surrounding space", "  2026-11-24  ", "2026-11-24", false},
		{"leap day in a leap year", "2024-02-29", "2024-02-29", false},
		{"leap day day-first", "29/02/2024", "2024-02-29", false},
		{"century leap year", "2000-02-29", "2000-02-29", false},

		{"leap day in a common year", "2026-02-29", "", true},
		{"non-leap century", "1900-02-29", "", true},
		{"day 30 of February", "2026-02-30", "", true},
		{"month 13", "2026-13-01", "", true},
		{"day 45", "2026-01-45", "", true},
		{"month-first US order", "11/24/2026", "", true},
		{"empty", "", "", true},
		{"blank", "   ", "", true},
		{"words", "yesterday", "", true},
		{"two digit year", "24/11/26", "", true},
		{"no separators", "20261124", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDate(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrBadDate) {
					t.Fatalf("ParseDate(%q) = %v, error %v; want ErrBadDate", tc.in, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate(%q): unexpected error %v", tc.in, err)
			}
			if FormatDate(got) != tc.want {
				t.Errorf("ParseDate(%q) = %s, want %s", tc.in, FormatDate(got), tc.want)
			}
			if got.Location() != time.UTC {
				t.Errorf("ParseDate(%q) location = %v, want UTC", tc.in, got.Location())
			}
		})
	}
}

// TestDay checks a timestamp is reduced to the calendar date the operator would
// write on the slip, in their own zone, and anchored at UTC midnight.
func TestDay(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	pht := time.FixedZone("PHT", 8*3600)

	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"already midnight UTC", time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC), "2026-11-24"},
		{"late evening UTC", time.Date(2026, 11, 24, 23, 59, 59, 0, time.UTC), "2026-11-24"},
		{"morning shift in India", time.Date(2026, 11, 24, 7, 30, 0, 0, ist), "2026-11-24"},
		{"night shift in Manila", time.Date(2026, 11, 24, 22, 15, 0, 0, pht), "2026-11-24"},
		{"first minute of a month", time.Date(2026, 3, 1, 0, 1, 0, 0, ist), "2026-03-01"},
		{"leap day", time.Date(2024, 2, 29, 18, 0, 0, 0, pht), "2024-02-29"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Day(tc.in)
			if FormatDate(got) != tc.want {
				t.Errorf("Day(%v) = %s, want %s", tc.in, FormatDate(got), tc.want)
			}
			if h, m, s := got.Clock(); h != 0 || m != 0 || s != 0 {
				t.Errorf("Day(%v) clock = %02d:%02d:%02d, want midnight", tc.in, h, m, s)
			}
		})
	}
	// Day must be idempotent, since it is applied on write and again on load.
	once := Day(time.Date(2026, 11, 24, 13, 45, 0, 0, ist))
	if !Day(once).Equal(once) {
		t.Error("Day is not idempotent")
	}
}

// TestDaysBetween covers the date arithmetic the aged-lot alert depends on,
// including leap years and the month lengths that trip naive 30-day maths.
func TestDaysBetween(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time {
		return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
	}
	tests := []struct {
		name     string
		from, to time.Time
		want     int
	}{
		{"same day", d(2026, 11, 24), d(2026, 11, 24), 0},
		{"next day", d(2026, 11, 24), d(2026, 11, 25), 1},
		{"across a leap day", d(2024, 2, 28), d(2024, 3, 1), 2},
		{"across February in a common year", d(2023, 2, 28), d(2023, 3, 1), 1},
		{"end of a 31-day month", d(2024, 1, 31), d(2024, 2, 1), 1},
		{"end of a 30-day month", d(2024, 4, 30), d(2024, 5, 1), 1},
		{"across a year end", d(2024, 12, 31), d(2025, 1, 1), 1},
		{"a full leap year", d(2024, 1, 1), d(2025, 1, 1), 366},
		{"a full common year", d(2023, 1, 1), d(2024, 1, 1), 365},
		{"February to February over a leap day", d(2024, 2, 1), d(2024, 3, 1), 29},
		{"February to March in a common year", d(2023, 2, 1), d(2023, 3, 1), 28},
		{"ninety days", d(2026, 1, 1), d(2026, 4, 1), 90},
		{"backwards is negative", d(2026, 11, 25), d(2026, 11, 24), -1},
		{"a century", d(1926, 11, 24), d(2026, 11, 24), 36525},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DaysBetween(tc.from, tc.to); got != tc.want {
				t.Errorf("DaysBetween(%s, %s) = %d, want %d",
					FormatDate(tc.from), FormatDate(tc.to), got, tc.want)
			}
		})
	}
}

// TestDaysBetweenIgnoresClockTime checks the count is in calendar days, so a
// lot received in the evening is not a day younger than one received at dawn.
func TestDaysBetweenIgnoresClockTime(t *testing.T) {
	morning := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	night := time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC)
	later := time.Date(2026, 4, 1, 9, 15, 0, 0, time.UTC)
	if a, b := DaysBetween(morning, later), DaysBetween(night, later); a != b || a != 90 {
		t.Errorf("DaysBetween gave %d and %d, want both 90", a, b)
	}
}

func TestFormatDate(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	tests := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC), "2026-11-24"},
		{time.Date(2026, 1, 2, 15, 4, 5, 0, time.UTC), "2026-01-02"},
		{time.Date(2024, 2, 29, 23, 59, 0, 0, ist), "2024-02-29"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := FormatDate(tc.in); got != tc.want {
				t.Errorf("FormatDate(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
