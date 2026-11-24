package yarn

import (
	"errors"
	"strings"
	"testing"
)

// threeLots builds the standard fixture: three dye-lots of 30s Navy received on
// three different days, deliberately keyed in out of date order.
//
//	DL-1  100 kg  received 2026-01-05  (oldest)
//	DL-2   50 kg  received 2026-01-10
//	DL-3   75 kg  received 2026-01-20  (newest)
//
// Total on hand: 225 kg across three lots.
func threeLots(t *testing.T) *Book {
	t.Helper()
	b := NewBook()
	receive(t, b, navy30("DL-2"), 50, "2026-01-10")
	receive(t, b, navy30("DL-3"), 75, "2026-01-20")
	receive(t, b, navy30("DL-1"), 100, "2026-01-05")
	return b
}

// alloc is an expected allocation line.
type alloc struct {
	lot string
	kg  float64
}

func checkPlan(t *testing.T, got []Allocation, want []alloc) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("plan has %d lines, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Key.DyeLot != w.lot {
			t.Errorf("line %d is from %s, want %s", i, got[i].Key.DyeLot, w.lot)
		}
		if wantG := g(t, w.kg); got[i].QtyG != wantG {
			t.Errorf("line %d (%s) takes %s, want %s", i, w.lot, got[i].QtyG, wantG)
		}
	}
}

// TestIssueFIFOAllocation is the central allocation table: the quantity on the
// slip is spread across dye-lots oldest first.
func TestIssueFIFOAllocation(t *testing.T) {
	tests := []struct {
		name string
		qty  float64
		want []alloc
	}{
		{
			"well inside the oldest lot",
			60,
			[]alloc{{"DL-1", 60}},
		},
		{
			"exact fit on the oldest lot",
			100,
			[]alloc{{"DL-1", 100}},
		},
		{
			"one gram into the second lot",
			100.001,
			[]alloc{{"DL-1", 100}, {"DL-2", 0.001}},
		},
		{
			"partial spill across two lots",
			120,
			[]alloc{{"DL-1", 100}, {"DL-2", 20}},
		},
		{
			"exact fit across two lots",
			150,
			[]alloc{{"DL-1", 100}, {"DL-2", 50}},
		},
		{
			"partial spill across three lots",
			160,
			[]alloc{{"DL-1", 100}, {"DL-2", 50}, {"DL-3", 10}},
		},
		{
			"exact fit across all three lots",
			225,
			[]alloc{{"DL-1", 100}, {"DL-2", 50}, {"DL-3", 75}},
		},
		{
			"the smallest possible issue",
			0.001,
			[]alloc{{"DL-1", 0.001}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := threeLots(t)
			plan, err := b.IssueFIFO("30s", "Navy", g(t, tc.qty))
			if err != nil {
				t.Fatalf("IssueFIFO(%v kg): %v", tc.qty, err)
			}
			checkPlan(t, plan, tc.want)
			if got, want := AllocatedTotal(plan), g(t, tc.qty); got != want {
				t.Errorf("plan totals %s, want %s", got, want)
			}
			// Planning writes nothing.
			if got, want := b.GroupBalance("30s", "Navy"), g(t, 225); got != want {
				t.Errorf("planning changed the balance to %s, want %s", got, want)
			}
		})
	}
}

// TestIssueFIFOAllocationDetail checks the per-line figures the UI shows the
// operator: the lot balance before, what is left after, and the flag that says
// the weaver is about to cross a shade join.
func TestIssueFIFOAllocationDetail(t *testing.T) {
	b := threeLots(t)
	plan, err := b.IssueFIFO("30s", "Navy", g(t, 160))
	if err != nil {
		t.Fatalf("IssueFIFO: %v", err)
	}
	want := []struct {
		lot          string
		before, left float64
		exhausts     bool
		received     string
	}{
		{"DL-1", 100, 0, true, "2026-01-05"},
		{"DL-2", 50, 0, true, "2026-01-10"},
		{"DL-3", 75, 65, false, "2026-01-20"},
	}
	for i, w := range want {
		a := plan[i]
		if got := a.LotBalanceG; got != g(t, w.before) {
			t.Errorf("%s balance before = %s, want %s", w.lot, got, g(t, w.before))
		}
		if got := a.RemainingG; got != g(t, w.left) {
			t.Errorf("%s remaining = %s, want %s", w.lot, got, g(t, w.left))
		}
		if a.Exhausts != w.exhausts {
			t.Errorf("%s Exhausts = %v, want %v", w.lot, a.Exhausts, w.exhausts)
		}
		if got := FormatDate(a.FirstReceived); got != w.received {
			t.Errorf("%s received = %s, want %s", w.lot, got, w.received)
		}
		if a.LotBalanceG-a.QtyG != a.RemainingG {
			t.Errorf("%s: before - taken != remaining (%s - %s != %s)", w.lot, a.LotBalanceG, a.QtyG, a.RemainingG)
		}
	}
}

// TestIssueFIFOInsufficient checks an over-issue is refused outright. A
// part-allocated plan would hand the floor a slip it cannot fill, so the whole
// plan is withheld and the shortfall named across all lots.
func TestIssueFIFOInsufficient(t *testing.T) {
	tests := []struct {
		name          string
		qty           float64
		wantShortfall float64
	}{
		{"one gram over the total", 225.001, 0.001},
		{"well over the total", 300, 75},
		{"absurdly over", 10000, 9775},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := threeLots(t)
			plan, err := b.IssueFIFO("30s", "Navy", g(t, tc.qty))
			if plan != nil {
				t.Errorf("plan = %+v, want nil on a shortfall", plan)
			}
			var se *ShortfallError
			if !errors.As(err, &se) {
				t.Fatalf("error = %v, want a *ShortfallError", err)
			}
			if got, want := se.Available, g(t, 225); got != want {
				t.Errorf("available = %s, want %s", got, want)
			}
			if got, want := se.Shortfall(), g(t, tc.wantShortfall); got != want {
				t.Errorf("shortfall = %s, want %s", got, want)
			}
			if !se.Group {
				t.Error("Group = false, want true: the shortfall is across all dye-lots")
			}
			msg := se.Error()
			for _, want := range []string{"30s", "Navy", "all dye-lots", "short by"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not mention %q", msg, want)
				}
			}
		})
	}
}

// TestIssueFIFOEmptyStock covers the count+shade that has nothing at all,
// which must report a shortfall rather than an empty plan.
func TestIssueFIFOEmptyStock(t *testing.T) {
	b := threeLots(t)
	tests := []struct{ name, count, shade string }{
		{"shade never stocked", "30s", "Emerald"},
		{"count never stocked", "2/80s", "Navy"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := b.IssueFIFO(tc.count, tc.shade, g(t, 10))
			var se *ShortfallError
			if !errors.As(err, &se) {
				t.Fatalf("error = %v, want a *ShortfallError", err)
			}
			if se.Available != 0 {
				t.Errorf("available = %s, want 0", se.Available)
			}
			if got, want := se.Shortfall(), g(t, 10); got != want {
				t.Errorf("shortfall = %s, want %s", got, want)
			}
		})
	}
}

// TestIssueFIFOSkipsExhaustedLots checks a lot already run down to zero is not
// offered an allocation line.
func TestIssueFIFOSkipsExhaustedLots(t *testing.T) {
	b := threeLots(t)
	if _, err := b.Issue(navy30("DL-1"), g(t, 100), onDate(t, "2026-02-01"), "WO-0"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	plan, err := b.IssueFIFO("30s", "Navy", g(t, 60))
	if err != nil {
		t.Fatalf("IssueFIFO: %v", err)
	}
	checkPlan(t, plan, []alloc{{"DL-2", 50}, {"DL-3", 10}})
}

// TestIssueFIFOUsesRemainingBalance checks a part-issued lot is allocated on
// what is left in it, not on what was received.
func TestIssueFIFOUsesRemainingBalance(t *testing.T) {
	b := threeLots(t)
	if _, err := b.Issue(navy30("DL-1"), g(t, 70), onDate(t, "2026-02-01"), "WO-0"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	plan, err := b.IssueFIFO("30s", "Navy", g(t, 50))
	if err != nil {
		t.Fatalf("IssueFIFO: %v", err)
	}
	checkPlan(t, plan, []alloc{{"DL-1", 30}, {"DL-2", 20}})
}

// TestIssueFIFOCountsAdjustments checks a write-off reduces what FIFO can
// allocate from that lot.
func TestIssueFIFOCountsAdjustments(t *testing.T) {
	b := threeLots(t)
	if _, err := b.Adjust(navy30("DL-1"), g(t, -90), onDate(t, "2026-02-01"), "water damage"); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	plan, err := b.IssueFIFO("30s", "Navy", g(t, 40))
	if err != nil {
		t.Fatalf("IssueFIFO: %v", err)
	}
	checkPlan(t, plan, []alloc{{"DL-1", 10}, {"DL-2", 30}})
}

// TestIssueFIFOIgnoresOtherGroups checks allocation never reaches across a
// count or a shade. Taking 30s Navy from a 30s Royal lot would be the exact
// mistake this tool exists to prevent.
func TestIssueFIFOIgnoresOtherGroups(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 10, "2026-01-05")
	receive(t, b, lotKey("30s", "Royal", "DL-R", "Sakthi Spinners"), 500, "2026-01-01")
	receive(t, b, lotKey("2/60s", "Navy", "DL-F", "Sakthi Spinners"), 500, "2026-01-01")

	plan, err := b.IssueFIFO("30s", "Navy", g(t, 10))
	if err != nil {
		t.Fatalf("IssueFIFO: %v", err)
	}
	checkPlan(t, plan, []alloc{{"DL-1", 10}})

	if _, err := b.IssueFIFO("30s", "Navy", g(t, 11)); err == nil {
		t.Error("IssueFIFO found 11 kg of 30s Navy, but only 10 kg exists in that group")
	}
}

// TestIssueFIFOCaseInsensitive checks the floor can write the order in any
// capitalisation.
func TestIssueFIFOCaseInsensitive(t *testing.T) {
	b := threeLots(t)
	plan, err := b.IssueFIFO(" 30S ", "  navy ", g(t, 120))
	if err != nil {
		t.Fatalf("IssueFIFO: %v", err)
	}
	checkPlan(t, plan, []alloc{{"DL-1", 100}, {"DL-2", 20}})
}

// TestIssueFIFOSameDayTieBreak checks two lots received on the same day get a
// stable, repeatable order rather than whatever the map iteration gives.
func TestIssueFIFOSameDayTieBreak(t *testing.T) {
	for i := 0; i < 20; i++ {
		b := NewBook()
		receive(t, b, navy30("DL-A"), 10, "2026-01-05")
		receive(t, b, navy30("DL-B"), 10, "2026-01-05")
		receive(t, b, navy30("DL-C"), 10, "2026-01-05")
		plan, err := b.IssueFIFO("30s", "Navy", g(t, 25))
		if err != nil {
			t.Fatalf("IssueFIFO: %v", err)
		}
		checkPlan(t, plan, []alloc{{"DL-A", 10}, {"DL-B", 10}, {"DL-C", 5}})
	}
}

// TestIssueFIFOLotWithNoReceipt covers opening stock entered as an adjustment
// rather than a receipt. Such a lot has no receipt date, so it cannot be aged
// and it has no place in the receipt order: it is allocated last, after every
// lot whose age is actually known, and it is never flagged for shade drift
// because the book does not know how old it is.
func TestIssueFIFOLotWithNoReceipt(t *testing.T) {
	b := threeLots(t)
	opening := navy30("DL-OPENING")
	if _, err := b.Adjust(opening, g(t, 30), onDate(t, "2020-01-01"), "opening stock"); err != nil {
		t.Fatalf("Adjust: %v", err)
	}

	lots := b.Lots()
	if len(lots) != 4 {
		t.Fatalf("got %d lots, want 4", len(lots))
	}
	if last := lots[3]; last.Key.DyeLot != "DL-OPENING" {
		t.Errorf("lot with no receipt sorted to position of %s, want it last", last.Key.DyeLot)
	}
	if !lots[3].FirstReceived.IsZero() {
		t.Error("a lot opened by an adjustment should have no receipt date")
	}

	// It is still real stock, so it is still allocated once the dated lots run
	// out: 225 kg dated plus 30 kg undated.
	plan, err := b.IssueFIFO("30s", "Navy", g(t, 235))
	if err != nil {
		t.Fatalf("IssueFIFO: %v", err)
	}
	checkPlan(t, plan, []alloc{{"DL-1", 100}, {"DL-2", 50}, {"DL-3", 75}, {"DL-OPENING", 10}})

	if _, err := b.IssueFIFO("30s", "Navy", g(t, 256)); err == nil {
		t.Error("IssueFIFO allocated more than the 255 kg on hand")
	}

	// Aged-lot alerts must skip it: with no receipt date there is no age.
	a := b.ReorderAlerts(nil, onDate(t, "2026-12-31"), 90)
	for _, al := range a.AgedLots {
		if al.Key.DyeLot == "DL-OPENING" {
			t.Errorf("a lot with no receipt date was flagged as %d days old", al.AgeDays)
		}
	}
	if len(a.AgedLots) != 3 {
		t.Errorf("got %d aged lots, want 3 (the three dated ones)", len(a.AgedLots))
	}
}

// TestIssueFIFOValidation covers the inputs the planner must refuse.
func TestIssueFIFOValidation(t *testing.T) {
	tests := []struct {
		name  string
		count string
		shade string
		qty   float64
		want  error
	}{
		{"zero quantity", "30s", "Navy", 0, ErrNonPositiveQty},
		{"negative quantity", "30s", "Navy", -10, ErrNonPositiveQty},
		{"no count", "", "Navy", 10, ErrIncompleteKey},
		{"blank count", "   ", "Navy", 10, ErrIncompleteKey},
		{"no shade", "30s", "", 10, ErrIncompleteKey},
		{"blank shade", "30s", "  ", 10, ErrIncompleteKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := threeLots(t)
			_, err := b.IssueFIFO(tc.count, tc.shade, g(t, tc.qty))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestPostFIFOIssue checks committing a plan writes one issue per allocation
// line and moves the balances by exactly the allocated amounts.
func TestPostFIFOIssue(t *testing.T) {
	b := threeLots(t)
	plan, err := b.PostFIFOIssue("30s", "Navy", g(t, 160), onDate(t, "2026-02-01"), "WO-77")
	if err != nil {
		t.Fatalf("PostFIFOIssue: %v", err)
	}
	checkPlan(t, plan, []alloc{{"DL-1", 100}, {"DL-2", 50}, {"DL-3", 10}})

	wantBalances := map[string]float64{"DL-1": 0, "DL-2": 0, "DL-3": 65}
	for lot, kg := range wantBalances {
		if got, want := b.Balance(navy30(lot)), g(t, kg); got != want {
			t.Errorf("%s balance = %s, want %s", lot, got, want)
		}
	}
	if got, want := b.GroupBalance("30s", "Navy"), g(t, 65); got != want {
		t.Errorf("group balance = %s, want %s", got, want)
	}

	// The register must show which lot each kilogram actually came off: that
	// trace is what a shade complaint from the buyer is answered with.
	reg := b.Register()
	if len(reg) != 3 {
		t.Fatalf("register has %d entries, want 3", len(reg))
	}
	seen := map[string]Grams{}
	for _, e := range reg {
		if e.Ref != "WO-77" {
			t.Errorf("register entry ref = %q, want WO-77", e.Ref)
		}
		if FormatDate(e.Date) != "2026-02-01" {
			t.Errorf("register entry date = %s, want 2026-02-01", FormatDate(e.Date))
		}
		seen[e.Key.DyeLot] += e.QtyG
	}
	for lot, kg := range map[string]float64{"DL-1": 100, "DL-2": 50, "DL-3": 10} {
		if got, want := seen[lot], g(t, kg); got != want {
			t.Errorf("register shows %s off %s, want %s", got, lot, want)
		}
	}
}

// TestPostFIFOIssueWritesNothingOnShortfall is the atomicity rule: a refused
// issue must leave the ledger exactly as it was.
func TestPostFIFOIssueWritesNothingOnShortfall(t *testing.T) {
	b := threeLots(t)
	before := len(b.Txns)

	_, err := b.PostFIFOIssue("30s", "Navy", g(t, 226), onDate(t, "2026-02-01"), "WO-78")
	var se *ShortfallError
	if !errors.As(err, &se) {
		t.Fatalf("error = %v, want a *ShortfallError", err)
	}
	if len(b.Txns) != before {
		t.Errorf("ledger grew from %d to %d movements on a refused issue", before, len(b.Txns))
	}
	if got, want := b.GroupBalance("30s", "Navy"), g(t, 225); got != want {
		t.Errorf("group balance = %s, want %s", got, want)
	}
	if len(b.Register()) != 0 {
		t.Errorf("register has %d entries, want 0", len(b.Register()))
	}
}

// TestPostFIFOIssueRepeated checks consecutive issues keep walking forward
// through the lots and that the last one empties the group to exactly zero.
func TestPostFIFOIssueRepeated(t *testing.T) {
	b := threeLots(t)
	steps := []struct {
		qty  float64
		want []alloc
	}{
		{80, []alloc{{"DL-1", 80}}},
		{40, []alloc{{"DL-1", 20}, {"DL-2", 20}}},
		{100, []alloc{{"DL-2", 30}, {"DL-3", 70}}},
		{5, []alloc{{"DL-3", 5}}},
	}
	for i, s := range steps {
		plan, err := b.PostFIFOIssue("30s", "Navy", g(t, s.qty), onDate(t, "2026-02-01"), "WO")
		if err != nil {
			t.Fatalf("step %d (%v kg): %v", i, s.qty, err)
		}
		checkPlan(t, plan, s.want)
	}
	if got := b.GroupBalance("30s", "Navy"); got != 0 {
		t.Errorf("after issuing all 225 kg, group balance = %s, want exactly 0", got)
	}
	if _, err := b.IssueFIFO("30s", "Navy", g(t, 0.001)); err == nil {
		t.Error("IssueFIFO succeeded against an empty group")
	}
}

// TestAllocatedTotal covers the plan summation helper.
func TestAllocatedTotal(t *testing.T) {
	tests := []struct {
		name string
		in   []Allocation
		want Grams
	}{
		{"nil", nil, 0},
		{"empty", []Allocation{}, 0},
		{"one line", []Allocation{{QtyG: 1000}}, 1000},
		{"three lines", []Allocation{{QtyG: 1000}, {QtyG: 500}, {QtyG: 1}}, 1501},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := AllocatedTotal(tc.in); got != tc.want {
				t.Errorf("AllocatedTotal = %s, want %s", got, tc.want)
			}
		})
	}
}
