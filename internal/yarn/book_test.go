package yarn

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// --- shared test helpers ----------------------------------------------------

// g converts kilograms to Grams, failing the test on a bad figure.
func g(t *testing.T, kg float64) Grams {
	t.Helper()
	v, err := KgToGrams(kg)
	if err != nil {
		t.Fatalf("KgToGrams(%v): %v", kg, err)
	}
	return v
}

// onDate parses a test date, failing the test on a bad one.
func onDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

// lotKey builds a stock key from the four parts, in reading order.
func lotKey(count, shade, dyeLot, supplier string) YarnKey {
	return YarnKey{Count: count, Shade: shade, DyeLot: dyeLot, Supplier: supplier}
}

// receive posts a receipt, failing the test if it is refused.
func receive(t *testing.T, b *Book, k YarnKey, kg float64, date string) {
	t.Helper()
	if _, err := b.Receive(k, g(t, kg), onDate(t, date), "GRN"); err != nil {
		t.Fatalf("Receive(%s, %v kg): %v", k.Label(), kg, err)
	}
}

// navy30 is the lot used across the ledger tests.
func navy30(lot string) YarnKey { return lotKey("30s", "Navy", lot, "Sakthi Spinners") }

// --- tests ------------------------------------------------------------------

// TestBalance is the core arithmetic: receipts less issues, plus or minus
// adjustments, per dye-lot.
func TestBalance(t *testing.T) {
	b := NewBook()
	k := navy30("DL-1142")

	if got := b.Balance(k); got != 0 {
		t.Errorf("empty book balance = %s, want 0", got)
	}

	receive(t, b, k, 100, "2026-01-05")
	if got, want := b.Balance(k), g(t, 100); got != want {
		t.Errorf("after one receipt, balance = %s, want %s", got, want)
	}

	receive(t, b, k, 48.5, "2026-01-12")
	if got, want := b.Balance(k), g(t, 148.5); got != want {
		t.Errorf("after two receipts, balance = %s, want %s", got, want)
	}

	if _, err := b.Issue(k, g(t, 60.25), onDate(t, "2026-01-15"), "WO-1"); err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if got, want := b.Balance(k), g(t, 88.25); got != want {
		t.Errorf("after an issue, balance = %s, want %s", got, want)
	}

	// A negative adjustment for damaged cones.
	if _, err := b.Adjust(k, g(t, -0.25), onDate(t, "2026-01-16"), "damaged"); err != nil {
		t.Fatalf("Adjust down: %v", err)
	}
	if got, want := b.Balance(k), g(t, 88); got != want {
		t.Errorf("after a write-off, balance = %s, want %s", got, want)
	}

	// A positive adjustment for yarn found at stocktake.
	if _, err := b.Adjust(k, g(t, 2), onDate(t, "2026-01-17"), "stocktake"); err != nil {
		t.Fatalf("Adjust up: %v", err)
	}
	if got, want := b.Balance(k), g(t, 90); got != want {
		t.Errorf("after a stocktake gain, balance = %s, want %s", got, want)
	}

	// A different dye-lot is a different balance, even at the same count and
	// shade. This is the whole premise of the tool.
	other := navy30("DL-1143")
	if got := b.Balance(other); got != 0 {
		t.Errorf("a different dye-lot has balance %s, want 0", got)
	}
	if got, want := b.GroupBalance("30s", "Navy"), g(t, 90); got != want {
		t.Errorf("group balance = %s, want %s", got, want)
	}
}

// TestIssueNeverGoesNegative is the hard rule: physical yarn cannot go
// negative, so an over-issue is refused with the shortfall named.
func TestIssueNeverGoesNegative(t *testing.T) {
	tests := []struct {
		name          string
		onHand        float64
		issue         float64
		wantErr       bool
		wantShortfall float64
	}{
		{"well under", 100, 40, false, 0},
		{"exact fit", 100, 100, false, 0},
		{"one gram under", 100, 99.999, false, 0},
		{"one gram over", 100, 100.001, true, 0.001},
		{"well over", 100, 150, true, 50},
		{"nothing on hand", 0, 10, true, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()
			k := navy30("DL-1142")
			if tc.onHand > 0 {
				receive(t, b, k, tc.onHand, "2026-01-05")
			}
			before := b.Balance(k)

			_, err := b.Issue(k, g(t, tc.issue), onDate(t, "2026-01-10"), "WO-1")
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Issue(%v kg) refused: %v", tc.issue, err)
				}
				return
			}

			var se *ShortfallError
			if !errors.As(err, &se) {
				t.Fatalf("Issue(%v kg) error = %v, want a *ShortfallError", tc.issue, err)
			}
			if got, want := se.Shortfall(), g(t, tc.wantShortfall); got != want {
				t.Errorf("shortfall = %s, want %s", got, want)
			}
			if got, want := se.Available, g(t, tc.onHand); got != want {
				t.Errorf("available = %s, want %s", got, want)
			}
			// The refusal must name the lot and the numbers, so the store
			// clerk can act on it without opening the ledger.
			msg := se.Error()
			for _, want := range []string{"DL-1142", "30s", "Navy", "short by"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not mention %q", msg, want)
				}
			}
			// Nothing may be written when the issue is refused.
			if got := b.Balance(k); got != before {
				t.Errorf("balance moved to %s after a refused issue, want %s", got, before)
			}
			if len(b.Register()) != 0 {
				t.Errorf("a refused issue left %d entries in the register, want 0", len(b.Register()))
			}
		})
	}
}

// TestAdjustNeverGoesNegative holds a write-off to the same rule as an issue:
// a correction may not invent negative yarn either.
func TestAdjustNeverGoesNegative(t *testing.T) {
	b := NewBook()
	k := navy30("DL-1142")
	receive(t, b, k, 10, "2026-01-05")

	if _, err := b.Adjust(k, g(t, -10), onDate(t, "2026-01-06"), "write-off"); err != nil {
		t.Fatalf("writing the whole lot off should be allowed: %v", err)
	}
	if got := b.Balance(k); got != 0 {
		t.Fatalf("balance = %s, want 0", got)
	}

	var se *ShortfallError
	_, err := b.Adjust(k, g(t, -0.001), onDate(t, "2026-01-07"), "one more gram")
	if !errors.As(err, &se) {
		t.Fatalf("over-adjustment error = %v, want a *ShortfallError", err)
	}
	if got, want := se.Shortfall(), g(t, 0.001); got != want {
		t.Errorf("shortfall = %s, want %s", got, want)
	}

	// A positive adjustment is never blocked: yarn can always be found.
	if _, err := b.Adjust(k, g(t, 5), onDate(t, "2026-01-08"), "found"); err != nil {
		t.Fatalf("positive adjustment refused: %v", err)
	}
	if got, want := b.Balance(k), g(t, 5); got != want {
		t.Errorf("balance = %s, want %s", got, want)
	}
}

// TestMovementValidation covers the quantities and keys a movement must refuse.
func TestMovementValidation(t *testing.T) {
	good := navy30("DL-1142")

	tests := []struct {
		name string
		post func(b *Book) error
		want error
	}{
		{"zero receipt", func(b *Book) error {
			_, e := b.Receive(good, 0, onDate(t, "2026-01-05"), "")
			return e
		}, ErrNonPositiveQty},
		{"negative receipt", func(b *Book) error {
			_, e := b.Receive(good, g(t, -5), onDate(t, "2026-01-05"), "")
			return e
		}, ErrNonPositiveQty},
		{"zero issue", func(b *Book) error {
			_, e := b.Issue(good, 0, onDate(t, "2026-01-05"), "")
			return e
		}, ErrNonPositiveQty},
		{"negative issue", func(b *Book) error {
			_, e := b.Issue(good, g(t, -5), onDate(t, "2026-01-05"), "")
			return e
		}, ErrNonPositiveQty},
		{"zero adjustment", func(b *Book) error {
			_, e := b.Adjust(good, 0, onDate(t, "2026-01-05"), "")
			return e
		}, ErrZeroAdjust},
		{"receipt with no dye-lot", func(b *Book) error {
			_, e := b.Receive(lotKey("30s", "Navy", "", "S"), g(t, 10), onDate(t, "2026-01-05"), "")
			return e
		}, ErrIncompleteKey},
		{"issue with no count", func(b *Book) error {
			_, e := b.Issue(lotKey("", "Navy", "DL-1", "S"), g(t, 10), onDate(t, "2026-01-05"), "")
			return e
		}, ErrIncompleteKey},
		{"adjustment with no shade", func(b *Book) error {
			_, e := b.Adjust(lotKey("30s", "", "DL-1", "S"), g(t, 10), onDate(t, "2026-01-05"), "")
			return e
		}, ErrIncompleteKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBook()
			err := tc.post(b)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if len(b.Txns) != 0 {
				t.Errorf("a refused movement was written: %d txns", len(b.Txns))
			}
		})
	}
}

// TestTxnDelta pins the sign rule: receipts and issues are both entered as
// positive magnitudes, and only the delta carries the sign.
func TestTxnDelta(t *testing.T) {
	tests := []struct {
		typ  TxnType
		qty  Grams
		want Grams
	}{
		{TxnReceipt, 1000, 1000},
		{TxnIssue, 1000, -1000},
		{TxnAdjust, 500, 500},
		{TxnAdjust, -500, -500},
	}
	for _, tc := range tests {
		t.Run(string(tc.typ), func(t *testing.T) {
			got := Txn{Type: tc.typ, QtyG: tc.qty}.Delta()
			if got != tc.want {
				t.Errorf("Txn{%s, %s}.Delta() = %s, want %s", tc.typ, tc.qty, got, tc.want)
			}
		})
	}
	for _, typ := range []TxnType{TxnReceipt, TxnIssue, TxnAdjust} {
		if !typ.Valid() {
			t.Errorf("%q should be a valid movement type", typ)
		}
	}
	for _, typ := range []TxnType{"", "transfer", "RECEIPT"} {
		if TxnType(typ).Valid() {
			t.Errorf("%q should not be a valid movement type", typ)
		}
	}
}

// TestLots checks the per-lot roll-up, its ordering, and that a fully consumed
// lot stays on the sheet at zero so the file still reconciles.
func TestLots(t *testing.T) {
	b := NewBook()
	// Received deliberately out of date order, to prove the ordering is by
	// receipt date and not by the order someone keyed the entries in.
	receive(t, b, navy30("DL-3"), 75, "2026-01-20")
	receive(t, b, navy30("DL-1"), 100, "2026-01-05")
	receive(t, b, navy30("DL-2"), 50, "2026-01-10")

	if _, err := b.Issue(navy30("DL-1"), g(t, 100), onDate(t, "2026-02-01"), "WO-9"); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	lots := b.Lots()
	if len(lots) != 3 {
		t.Fatalf("got %d lots, want 3", len(lots))
	}
	wantOrder := []string{"DL-1", "DL-2", "DL-3"}
	for i, want := range wantOrder {
		if lots[i].Key.DyeLot != want {
			t.Errorf("lot %d is %s, want %s (oldest receipt first)", i, lots[i].Key.DyeLot, want)
		}
	}

	first := lots[0]
	if got, want := first.BalanceG, Grams(0); got != want {
		t.Errorf("DL-1 balance = %s, want %s", got, want)
	}
	if got, want := first.ReceivedG, g(t, 100); got != want {
		t.Errorf("DL-1 received = %s, want %s", got, want)
	}
	if got, want := first.IssuedG, g(t, 100); got != want {
		t.Errorf("DL-1 issued = %s, want %s", got, want)
	}
	if got, want := FormatDate(first.FirstReceived), "2026-01-05"; got != want {
		t.Errorf("DL-1 first received = %s, want %s", got, want)
	}
	if got, want := FormatDate(first.LastMovement), "2026-02-01"; got != want {
		t.Errorf("DL-1 last movement = %s, want %s", got, want)
	}

	if l, ok := b.Lot(navy30("dl-2")); !ok {
		t.Error("Lot lookup should be case-insensitive")
	} else if got, want := l.BalanceG, g(t, 50); got != want {
		t.Errorf("DL-2 balance = %s, want %s", got, want)
	}
	if _, ok := b.Lot(navy30("DL-nope")); ok {
		t.Error("Lot found a dye-lot that was never received")
	}
}

// TestLotsFirstReceivedUsesEarliestReceipt checks a lot topped up later keeps
// its original age, since the oldest yarn in it is what drifts.
func TestLotsFirstReceivedUsesEarliestReceipt(t *testing.T) {
	b := NewBook()
	k := navy30("DL-1")
	receive(t, b, k, 50, "2026-03-01")
	receive(t, b, k, 50, "2026-01-01")

	l, ok := b.Lot(k)
	if !ok {
		t.Fatal("lot not found")
	}
	if got, want := FormatDate(l.FirstReceived), "2026-01-01"; got != want {
		t.Errorf("first received = %s, want %s (the earliest receipt)", got, want)
	}
	if got, want := l.BalanceG, g(t, 100); got != want {
		t.Errorf("balance = %s, want %s", got, want)
	}
}

// TestGroups checks the count+shade roll-up and the open-lot count, which is
// the banding-risk signal.
func TestGroups(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 100, "2026-01-05")
	receive(t, b, navy30("DL-2"), 50, "2026-01-10")
	receive(t, b, lotKey("30s", "White", "RFD-1", "Sakthi Spinners"), 200, "2026-01-06")
	receive(t, b, lotKey("2/60s", "Navy", "DL-7", "Other Mills"), 25, "2026-01-07")
	if _, err := b.Issue(navy30("DL-1"), g(t, 100), onDate(t, "2026-02-01"), "WO-1"); err != nil {
		t.Fatalf("Issue: %v", err)
	}

	groups := b.Groups()
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	// Sorted by count then shade: "2/60s" < "30s" as strings.
	if groups[0].Count != "2/60s" || groups[1].Shade != "Navy" || groups[2].Shade != "White" {
		t.Errorf("groups out of order: %+v", groups)
	}

	navy := groups[1]
	if got, want := navy.BalanceG, g(t, 50); got != want {
		t.Errorf("30s Navy balance = %s, want %s", got, want)
	}
	if navy.Lots != 2 {
		t.Errorf("30s Navy lots = %d, want 2", navy.Lots)
	}
	if navy.OpenLots != 1 {
		t.Errorf("30s Navy open lots = %d, want 1 (DL-1 is exhausted)", navy.OpenLots)
	}
}

// TestRegister checks the issue register comes back newest first with a stable
// order within a day.
func TestRegister(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 500, "2026-01-01")

	for _, iss := range []struct {
		kg   float64
		date string
		ref  string
	}{
		{10, "2026-02-01", "WO-1"},
		{20, "2026-03-01", "WO-2"},
		{30, "2026-02-01", "WO-3"},
	} {
		if _, err := b.Issue(navy30("DL-1"), g(t, iss.kg), onDate(t, iss.date), iss.ref); err != nil {
			t.Fatalf("Issue %s: %v", iss.ref, err)
		}
	}

	reg := b.Register()
	if len(reg) != 3 {
		t.Fatalf("register has %d entries, want 3", len(reg))
	}
	wantRefs := []string{"WO-2", "WO-3", "WO-1"}
	for i, want := range wantRefs {
		if reg[i].Ref != want {
			t.Errorf("register[%d] ref = %q, want %q", i, reg[i].Ref, want)
		}
	}
	for _, e := range reg {
		if e.Type != TxnIssue {
			t.Errorf("register contains a %s, want only issues", e.Type)
		}
	}
}

// TestIssuedBetween checks the date window used for a consumption variance is
// inclusive at both ends and scoped to one count+shade.
func TestIssuedBetween(t *testing.T) {
	b := NewBook()
	receive(t, b, navy30("DL-1"), 500, "2026-01-01")
	receive(t, b, lotKey("30s", "White", "RFD-1", "S"), 500, "2026-01-01")

	for _, iss := range []struct {
		k    YarnKey
		kg   float64
		date string
	}{
		{navy30("DL-1"), 10, "2026-01-31"},
		{navy30("DL-1"), 20, "2026-02-01"},
		{navy30("DL-1"), 30, "2026-02-15"},
		{navy30("DL-1"), 40, "2026-02-28"},
		{navy30("DL-1"), 50, "2026-03-01"},
		{lotKey("30s", "White", "RFD-1", "S"), 99, "2026-02-15"},
	} {
		if _, err := b.Issue(iss.k, g(t, iss.kg), onDate(t, iss.date), "WO"); err != nil {
			t.Fatalf("Issue: %v", err)
		}
	}

	tests := []struct {
		name     string
		from, to string
		want     float64
	}{
		{"February only", "2026-02-01", "2026-02-28", 90},
		{"inclusive of the first day", "2026-02-15", "2026-02-28", 70},
		{"inclusive of the last day", "2026-02-01", "2026-02-15", 50},
		{"a single day", "2026-02-15", "2026-02-15", 30},
		{"a day with no issues", "2026-02-02", "2026-02-02", 0},
		{"the whole quarter", "2026-01-01", "2026-03-31", 150},
		{"reversed window finds nothing", "2026-02-28", "2026-02-01", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := b.IssuedBetween("30s", "Navy", onDate(t, tc.from), onDate(t, tc.to))
			if want := g(t, tc.want); got != want {
				t.Errorf("IssuedBetween(%s..%s) = %s, want %s", tc.from, tc.to, got, want)
			}
		})
	}
}

// TestNormalize checks a book loaded from disk is repaired: keys tidied, dates
// flattened, missing sequence numbers filled, and the ledger put back in
// posting order.
func TestNormalize(t *testing.T) {
	b := &Book{
		Txns: []Txn{
			{Seq: 3, Type: TxnIssue, QtyG: 1000, Date: time.Date(2026, 2, 1, 14, 30, 0, 0, time.UTC), Key: lotKey(" 30S ", "NAVY ", " dl-1 ", "Sakthi  Spinners")},
			{Seq: 1, Type: TxnReceipt, QtyG: 5000, Date: time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), Key: navy30("DL-1")},
			{Type: TxnAdjust, QtyG: -100, Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Key: navy30("DL-1")},
		},
		Rules: []ReorderRule{{Count: " 30s ", Shade: " Navy ", LevelG: 1000}},
	}
	b.Normalize()

	if b.AgedAfterDays != DefaultAgedLotDays {
		t.Errorf("AgedAfterDays = %d, want the default %d", b.AgedAfterDays, DefaultAgedLotDays)
	}
	if got := []int64{b.Txns[0].Seq, b.Txns[1].Seq, b.Txns[2].Seq}; got[0] >= got[1] || got[1] >= got[2] {
		t.Errorf("sequences not ascending after Normalize: %v", got)
	}
	for i, tx := range b.Txns {
		if h, m, s := tx.Date.Clock(); h != 0 || m != 0 || s != 0 {
			t.Errorf("txn %d date keeps a clock time %02d:%02d:%02d", i, h, m, s)
		}
		if tx.Seq == 0 {
			t.Errorf("txn %d still has no sequence number", i)
		}
	}
	if b.Rules[0].Count != "30s" || b.Rules[0].Shade != "Navy" {
		t.Errorf("rule not normalised: %+v", b.Rules[0])
	}
	// The three movements all belong to one lot despite the mangled key.
	if got, want := b.Balance(navy30("DL-1")), Grams(5000-1000-100); got != want {
		t.Errorf("balance after Normalize = %s, want %s", got, want)
	}
	if len(b.Lots()) != 1 {
		t.Errorf("got %d lots, want 1 (the mangled key should fold in)", len(b.Lots()))
	}
	// A further posting must not reuse a sequence number.
	tx, err := b.Receive(navy30("DL-1"), 1000, onDate(t, "2026-04-01"), "GRN")
	if err != nil {
		t.Fatalf("Receive after Normalize: %v", err)
	}
	for _, prior := range b.Txns[:len(b.Txns)-1] {
		if prior.Seq == tx.Seq {
			t.Errorf("new txn reused sequence %d", tx.Seq)
		}
	}
}

// TestZeroBookIsUsable checks a zero-value Book (the shape a fresh or empty
// data file unmarshals into) can be posted to without a panic.
func TestZeroBookIsUsable(t *testing.T) {
	var b Book
	if got := b.Balance(navy30("DL-1")); got != 0 {
		t.Errorf("balance = %s, want 0", got)
	}
	if _, err := b.Receive(navy30("DL-1"), 1000, onDate(t, "2026-01-01"), "GRN"); err != nil {
		t.Fatalf("Receive on a zero Book: %v", err)
	}
	if got, want := b.Balance(navy30("DL-1")), Grams(1000); got != want {
		t.Errorf("balance = %s, want %s", got, want)
	}
	if b.agedDays() != DefaultAgedLotDays {
		t.Errorf("agedDays = %d, want the default %d", b.agedDays(), DefaultAgedLotDays)
	}
}
