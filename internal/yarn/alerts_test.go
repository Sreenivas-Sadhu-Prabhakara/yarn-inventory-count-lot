package yarn

import (
	"errors"
	"testing"
)

// rule builds a reorder rule in kilograms.
func rule(t *testing.T, count, shade string, levelKg, orderUpToKg float64) ReorderRule {
	t.Helper()
	r := ReorderRule{Count: count, Shade: shade, LevelG: g(t, levelKg)}
	if orderUpToKg > 0 {
		r.OrderUpToG = g(t, orderUpToKg)
	}
	return r
}

// TestReorderAlertThreshold covers the trigger boundary: the alert fires at or
// below the level, and not one gram above it.
func TestReorderAlertThreshold(t *testing.T) {
	tests := []struct {
		name      string
		onHandKg  float64
		levelKg   float64
		wantAlert bool
		wantShort float64
	}{
		{"well below the level", 20, 100, true, 80},
		{"one gram below the level", 99.999, 100, true, 0.001},
		{"exactly at the level", 100, 100, true, 0},
		{"one gram above the level", 100.001, 100, false, 0},
		{"well above the level", 500, 100, false, 0},
		{"nothing on hand", 0, 100, true, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()
			if tc.onHandKg > 0 {
				receive(t, b, navy30("DL-1"), tc.onHandKg, "2026-01-05")
			}
			a := b.ReorderAlerts([]ReorderRule{rule(t, "30s", "Navy", tc.levelKg, 0)}, onDate(t, "2026-01-10"), 90)

			if !tc.wantAlert {
				if len(a.Reorder) != 0 {
					t.Fatalf("got %d reorder alerts, want 0: %+v", len(a.Reorder), a.Reorder)
				}
				return
			}
			if len(a.Reorder) != 1 {
				t.Fatalf("got %d reorder alerts, want 1", len(a.Reorder))
			}
			al := a.Reorder[0]
			if got, want := al.OnHandG, g(t, tc.onHandKg); got != want {
				t.Errorf("on hand = %s, want %s", got, want)
			}
			if got, want := al.ShortByG, g(t, tc.wantShort); got != want {
				t.Errorf("short by = %s, want %s", got, want)
			}
			if al.Count != "30s" || al.Shade != "Navy" {
				t.Errorf("alert names %s %s, want 30s Navy", al.Count, al.Shade)
			}
		})
	}
}

// TestReorderSuggestedQuantity covers the suggested order, both with an
// explicit order-up-to level and with the derived default.
func TestReorderSuggestedQuantity(t *testing.T) {
	tests := []struct {
		name          string
		onHandKg      float64
		levelKg       float64
		orderUpToKg   float64
		wantTargetKg  float64
		wantSuggestKg float64
		wantDerived   bool
	}{
		{
			"derived target is twice the level",
			60, 100, 0,
			200, 140, true,
		},
		{
			"derived target with nothing on hand",
			0, 100, 0,
			200, 200, true,
		},
		{
			"explicit target",
			60, 100, 300,
			300, 240, false,
		},
		{
			"explicit target equal to the level",
			60, 100, 100,
			100, 40, false,
		},
		{
			"at the level with a derived target",
			100, 100, 0,
			200, 100, true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()
			if tc.onHandKg > 0 {
				receive(t, b, navy30("DL-1"), tc.onHandKg, "2026-01-05")
			}
			a := b.ReorderAlerts(
				[]ReorderRule{rule(t, "30s", "Navy", tc.levelKg, tc.orderUpToKg)},
				onDate(t, "2026-01-10"), 90)
			if len(a.Reorder) != 1 {
				t.Fatalf("got %d alerts, want 1", len(a.Reorder))
			}
			al := a.Reorder[0]
			if got, want := al.OrderUpToG, g(t, tc.wantTargetKg); got != want {
				t.Errorf("order up to = %s, want %s", got, want)
			}
			if got, want := al.SuggestedOrderG, g(t, tc.wantSuggestKg); got != want {
				t.Errorf("suggested order = %s, want %s", got, want)
			}
			if al.TargetDerived != tc.wantDerived {
				t.Errorf("TargetDerived = %v, want %v", al.TargetDerived, tc.wantDerived)
			}
			// The suggestion must actually clear the reorder point.
			if al.OnHandG+al.SuggestedOrderG < al.LevelG {
				t.Errorf("ordering %s on top of %s still leaves stock under the level %s",
					al.SuggestedOrderG, al.OnHandG, al.LevelG)
			}
		})
	}
}

// TestReorderSuggestionNeverNegative covers the case where stock is at or
// below the level but already above the order-up-to target, which must suggest
// nothing rather than a negative order.
func TestReorderSuggestionNeverNegative(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 100, "2026-01-05")
	// A deliberately odd rule: the target equals the level, and stock sits
	// exactly on both.
	a := b.ReorderAlerts([]ReorderRule{rule(t, "30s", "Navy", 100, 100)}, onDate(t, "2026-01-10"), 90)
	if len(a.Reorder) != 1 {
		t.Fatalf("got %d alerts, want 1", len(a.Reorder))
	}
	if got := a.Reorder[0].SuggestedOrderG; got != 0 {
		t.Errorf("suggested order = %s, want 0", got)
	}
}

// TestReorderPoolsAcrossLots checks the reorder level is judged on the total
// across every dye-lot of that count and shade.
func TestReorderPoolsAcrossLots(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 40, "2026-01-05")
	receive(t, b, navy30("DL-2"), 40, "2026-01-06")
	receive(t, b, navy30("DL-3"), 40, "2026-01-07")

	a := b.ReorderAlerts([]ReorderRule{rule(t, "30s", "Navy", 100, 0)}, onDate(t, "2026-01-10"), 90)
	if len(a.Reorder) != 0 {
		t.Fatalf("120 kg across three lots is above a 100 kg level, got %d alerts", len(a.Reorder))
	}

	a = b.ReorderAlerts([]ReorderRule{rule(t, "30s", "Navy", 150, 0)}, onDate(t, "2026-01-10"), 90)
	if len(a.Reorder) != 1 {
		t.Fatalf("got %d alerts, want 1", len(a.Reorder))
	}
	if got, want := a.Reorder[0].OnHandG, g(t, 120); got != want {
		t.Errorf("on hand = %s, want %s", got, want)
	}
	// The open-lot count is the banding warning that rides along with it.
	if got := a.Reorder[0].OpenLots; got != 3 {
		t.Errorf("open lots = %d, want 3", got)
	}
}

// TestReorderRuleHandling covers dormant, invalid and duplicate rules.
func TestReorderRuleHandling(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 10, "2026-01-05")
	asOf := onDate(t, "2026-01-10")

	tests := []struct {
		name      string
		rules     []ReorderRule
		wantCount int
	}{
		{"no rules at all", nil, 0},
		{"a zero level is dormant", []ReorderRule{rule(t, "30s", "Navy", 0, 0)}, 0},
		{"a rule with no shade is skipped", []ReorderRule{{Count: "30s", LevelG: 100000}}, 0},
		{"a rule with no count is skipped", []ReorderRule{{Shade: "Navy", LevelG: 100000}}, 0},
		{"a negative level is skipped", []ReorderRule{{Count: "30s", Shade: "Navy", LevelG: -1}}, 0},
		{"a rule for yarn never stocked still fires", []ReorderRule{rule(t, "2/80s", "Ecru", 50, 0)}, 1},
		{
			"duplicate rules produce one alert",
			[]ReorderRule{rule(t, "30s", "Navy", 100, 0), rule(t, " 30S ", " navy ", 500, 0)},
			1,
		},
		{
			"two different items produce two alerts",
			[]ReorderRule{rule(t, "30s", "Navy", 100, 0), rule(t, "30s", "White", 100, 0)},
			2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := b.ReorderAlerts(tc.rules, asOf, 90)
			if len(a.Reorder) != tc.wantCount {
				t.Errorf("got %d alerts, want %d: %+v", len(a.Reorder), tc.wantCount, a.Reorder)
			}
		})
	}
}

// TestReorderAlertOrdering checks the biggest shortfall is listed first, so a
// purchase officer works the list from the top.
func TestReorderAlertOrdering(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 90, "2026-01-05")
	receive(t, b, lotKey("30s", "White", "RFD-1", "S"), 10, "2026-01-05")
	receive(t, b, lotKey("40s", "Ecru", "E-1", "S"), 50, "2026-01-05")

	a := b.ReorderAlerts([]ReorderRule{
		rule(t, "30s", "Navy", 100, 0),  // short by 10
		rule(t, "30s", "White", 100, 0), // short by 90
		rule(t, "40s", "Ecru", 100, 0),  // short by 50
	}, onDate(t, "2026-01-10"), 90)

	if len(a.Reorder) != 3 {
		t.Fatalf("got %d alerts, want 3", len(a.Reorder))
	}
	wantShades := []string{"White", "Ecru", "Navy"}
	for i, want := range wantShades {
		if a.Reorder[i].Shade != want {
			t.Errorf("alert %d is %s, want %s (biggest shortfall first)", i, a.Reorder[i].Shade, want)
		}
	}
}

// TestAgedLotAlerts covers the shade-drift flag, including the exact day the
// threshold is crossed.
func TestAgedLotAlerts(t *testing.T) {
	tests := []struct {
		name      string
		received  string
		asOf      string
		threshold int
		balanceKg float64
		issueKg   float64
		wantAged  bool
		wantAge   int
	}{
		{"well inside the threshold", "2026-01-01", "2026-02-01", 90, 100, 0, false, 0},
		{"one day short of the threshold", "2026-01-01", "2026-03-31", 90, 100, 0, false, 0},
		{"exactly on the threshold", "2026-01-01", "2026-04-01", 90, 100, 0, false, 0},
		{"one day past the threshold", "2026-01-01", "2026-04-02", 90, 100, 0, true, 91},
		{"long past the threshold", "2026-01-01", "2026-12-31", 90, 100, 0, true, 364},
		{"a tighter threshold", "2026-01-01", "2026-02-01", 30, 100, 0, true, 31},
		{"a looser threshold", "2026-01-01", "2026-04-02", 180, 100, 0, false, 0},
		{"aged but fully consumed", "2026-01-01", "2026-12-31", 90, 100, 100, false, 0},
		{"aged with one gram left", "2026-01-01", "2026-12-31", 90, 100, 99.999, true, 364},
		{"aged across a leap day", "2024-01-01", "2024-03-31", 90, 100, 0, false, 0},
		{"aged across a leap day by one", "2024-01-01", "2024-04-01", 90, 100, 0, true, 91},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()
			k := navy30("DL-1")
			receive(t, b, k, tc.balanceKg, tc.received)
			if tc.issueKg > 0 {
				if _, err := b.Issue(k, g(t, tc.issueKg), onDate(t, tc.asOf), "WO"); err != nil {
					t.Fatalf("Issue: %v", err)
				}
			}
			a := b.ReorderAlerts(nil, onDate(t, tc.asOf), tc.threshold)

			if !tc.wantAged {
				if len(a.AgedLots) != 0 {
					t.Fatalf("got %d aged lots, want 0: %+v", len(a.AgedLots), a.AgedLots)
				}
				return
			}
			if len(a.AgedLots) != 1 {
				t.Fatalf("got %d aged lots, want 1", len(a.AgedLots))
			}
			al := a.AgedLots[0]
			if al.AgeDays != tc.wantAge {
				t.Errorf("age = %d days, want %d", al.AgeDays, tc.wantAge)
			}
			if al.ThresholdDays != tc.threshold {
				t.Errorf("threshold = %d, want %d", al.ThresholdDays, tc.threshold)
			}
			if al.Key.DyeLot != "DL-1" {
				t.Errorf("aged lot = %s, want DL-1", al.Key.DyeLot)
			}
			if al.BalanceG <= 0 {
				t.Errorf("aged lot balance = %s, want more than zero", al.BalanceG)
			}
		})
	}
}

// TestAgedLotOrdering checks the oldest lot is listed first.
func TestAgedLotOrdering(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-MID"), 10, "2026-03-01")
	receive(t, b, navy30("DL-OLD"), 10, "2026-01-01")
	receive(t, b, navy30("DL-NEW"), 10, "2026-05-01")

	a := b.ReorderAlerts(nil, onDate(t, "2026-12-31"), 90)
	if len(a.AgedLots) != 3 {
		t.Fatalf("got %d aged lots, want 3", len(a.AgedLots))
	}
	want := []string{"DL-OLD", "DL-MID", "DL-NEW"}
	for i, w := range want {
		if a.AgedLots[i].Key.DyeLot != w {
			t.Errorf("aged lot %d is %s, want %s (oldest first)", i, a.AgedLots[i].Key.DyeLot, w)
		}
	}
	if a.AgedLots[0].AgeDays <= a.AgedLots[2].AgeDays {
		t.Error("ages are not descending")
	}
}

// TestAlertsDefaults checks a zero or negative threshold falls back to the
// documented default rather than flagging every lot.
func TestAlertsDefaults(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 10, "2026-01-01")

	for _, threshold := range []int{0, -1, -90} {
		a := b.ReorderAlerts(nil, onDate(t, "2026-02-01"), threshold)
		if a.AgedAfterDays != DefaultAgedLotDays {
			t.Errorf("threshold %d gave AgedAfterDays = %d, want the default %d",
				threshold, a.AgedAfterDays, DefaultAgedLotDays)
		}
		if len(a.AgedLots) != 0 {
			t.Errorf("threshold %d flagged a 31-day-old lot against a %d-day default",
				threshold, DefaultAgedLotDays)
		}
	}
	if DefaultAgedLotDays != 90 {
		t.Errorf("DefaultAgedLotDays = %d, want 90", DefaultAgedLotDays)
	}
	if DefaultOrderUpToMultiple != 2 {
		t.Errorf("DefaultOrderUpToMultiple = %d, want 2", DefaultOrderUpToMultiple)
	}
}

// TestAlertsEmpty checks the empty case reports cleanly and returns empty
// slices rather than nils, so the JSON surface is stable.
func TestAlertsEmpty(t *testing.T) {
	b := NewBook()
	a := b.ReorderAlerts(nil, onDate(t, "2026-01-10"), 90)
	if !a.Empty() {
		t.Error("Empty() = false on an empty book")
	}
	if a.Reorder == nil || a.AgedLots == nil {
		t.Error("alert slices should be empty, not nil")
	}
	if FormatDate(a.AsOf) != "2026-01-10" {
		t.Errorf("AsOf = %s, want 2026-01-10", FormatDate(a.AsOf))
	}

	receive(t, b, navy30("DL-1"), 10, "2026-01-01")
	a = b.ReorderAlerts([]ReorderRule{rule(t, "30s", "Navy", 100, 0)}, onDate(t, "2026-01-10"), 90)
	if a.Empty() {
		t.Error("Empty() = true when a reorder alert is present")
	}
}

// TestCurrentAlerts checks the convenience path uses the rules and threshold
// stored in the book.
func TestCurrentAlerts(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 10, "2026-01-01")
	if err := b.SetRule(rule(t, "30s", "Navy", 100, 0)); err != nil {
		t.Fatalf("SetRule: %v", err)
	}
	b.AgedAfterDays = 30

	a := b.CurrentAlerts(onDate(t, "2026-02-15"))
	if len(a.Reorder) != 1 {
		t.Fatalf("got %d reorder alerts, want 1", len(a.Reorder))
	}
	if len(a.AgedLots) != 1 {
		t.Fatalf("got %d aged lots, want 1 (45 days against a 30-day threshold)", len(a.AgedLots))
	}
	if a.AgedAfterDays != 30 {
		t.Errorf("AgedAfterDays = %d, want 30", a.AgedAfterDays)
	}
}

func TestReorderRuleValidate(t *testing.T) {
	tests := []struct {
		name    string
		r       ReorderRule
		wantErr bool
	}{
		{"complete", ReorderRule{Count: "30s", Shade: "Navy", LevelG: 100000}, false},
		{"zero level is allowed", ReorderRule{Count: "30s", Shade: "Navy"}, false},
		{"with a target", ReorderRule{Count: "30s", Shade: "Navy", LevelG: 100000, OrderUpToG: 200000}, false},
		{"target equal to the level", ReorderRule{Count: "30s", Shade: "Navy", LevelG: 100000, OrderUpToG: 100000}, false},
		{"no count", ReorderRule{Shade: "Navy", LevelG: 100000}, true},
		{"no shade", ReorderRule{Count: "30s", LevelG: 100000}, true},
		{"blank count", ReorderRule{Count: "  ", Shade: "Navy", LevelG: 100000}, true},
		{"negative level", ReorderRule{Count: "30s", Shade: "Navy", LevelG: -1}, true},
		{"negative target", ReorderRule{Count: "30s", Shade: "Navy", LevelG: 100000, OrderUpToG: -1}, true},
		{"target below the level", ReorderRule{Count: "30s", Shade: "Navy", LevelG: 200000, OrderUpToG: 100000}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.Validate()
			if tc.wantErr {
				if !errors.Is(err, ErrBadRule) {
					t.Errorf("error = %v, want ErrBadRule", err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error %v", err)
			}
		})
	}
}

// TestSetAndDeleteRule checks a rule is replaced rather than duplicated, and
// that lookups are normalised.
func TestSetAndDeleteRule(t *testing.T) {
	b := NewBook()
	if err := b.SetRule(rule(t, "30s", "Navy", 100, 0)); err != nil {
		t.Fatalf("SetRule: %v", err)
	}
	if err := b.SetRule(rule(t, " 30S ", " navy ", 250, 500)); err != nil {
		t.Fatalf("SetRule replace: %v", err)
	}
	if len(b.Rules) != 1 {
		t.Fatalf("got %d rules, want 1 (the second should replace the first)", len(b.Rules))
	}
	if got, want := b.Rules[0].LevelG, g(t, 250); got != want {
		t.Errorf("level = %s, want %s", got, want)
	}
	if b.Rules[0].Count != "30S" || b.Rules[0].Shade != "navy" {
		t.Errorf("rule not normalised: %+v", b.Rules[0])
	}

	if err := b.SetRule(ReorderRule{Shade: "Navy", LevelG: 1}); !errors.Is(err, ErrBadRule) {
		t.Errorf("SetRule with no count: error = %v, want ErrBadRule", err)
	}

	if err := b.SetRule(rule(t, "40s", "Ecru", 50, 0)); err != nil {
		t.Fatalf("SetRule second item: %v", err)
	}
	if len(b.Rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(b.Rules))
	}

	if !b.DeleteRule("30s", "NAVY") {
		t.Error("DeleteRule returned false for a rule that exists")
	}
	if len(b.Rules) != 1 || b.Rules[0].Shade != "Ecru" {
		t.Errorf("rules after delete = %+v, want only the Ecru rule", b.Rules)
	}
	if b.DeleteRule("30s", "Navy") {
		t.Error("DeleteRule returned true for an already-deleted rule")
	}
}
