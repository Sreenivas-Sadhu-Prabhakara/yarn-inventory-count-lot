package yarn

import (
	"fmt"
	"time"
)

// Allocation is one line of a FIFO issue: how much comes off which dye-lot.
type Allocation struct {
	Key           YarnKey   `json:"key"`
	QtyG          Grams     `json:"qtyG"`
	LotBalanceG   Grams     `json:"lotBalanceG"`
	RemainingG    Grams     `json:"remainingG"`
	FirstReceived time.Time `json:"firstReceived"`
	// Exhausts reports that this allocation empties the lot. A run that
	// exhausts a lot mid-way is the point at which the weaver has to stop and
	// re-sample the shade against the next lot.
	Exhausts bool `json:"exhausts"`
}

// IssueFIFO plans an issue of one count+shade across its dye-lots, oldest
// receipt first, and returns the per-lot allocation.
//
// Oldest-first is not an accounting convention here, it is the physical rule:
// yarn dyed earliest has had the longest to drift in storage, so it is the lot
// most urgent to consume, and consuming one lot to exhaustion before opening
// the next keeps the number of shade joins in the fabric to a minimum.
//
// This plans only; it does not record anything. PostFIFOIssue commits. If the
// group cannot cover the quantity, nothing is allocated and a *ShortfallError
// names the gap, because a half-allocated issue would leave the floor with a
// slip it cannot fill.
func (b *Book) IssueFIFO(count, shade string, qty Grams) ([]Allocation, error) {
	if collapse(count) == "" || collapse(shade) == "" {
		return nil, fmt.Errorf("%w: count and shade required to issue", ErrIncompleteKey)
	}
	if qty <= 0 {
		return nil, fmt.Errorf("issue: %w", ErrNonPositiveQty)
	}

	group := shadeGroup(count, shade)
	var open []Lot
	var available Grams
	for _, l := range b.Lots() {
		if l.Key.Group() == group && l.BalanceG > 0 {
			open = append(open, l)
			available += l.BalanceG
		}
	}
	if qty > available {
		return nil, &ShortfallError{
			Key:       YarnKey{Count: collapse(count), Shade: collapse(shade)},
			Requested: qty,
			Available: available,
			Group:     true,
		}
	}

	out := []Allocation{}
	left := qty
	for _, l := range open {
		if left == 0 {
			break
		}
		take := l.BalanceG
		if take > left {
			take = left
		}
		out = append(out, Allocation{
			Key:           l.Key,
			QtyG:          take,
			LotBalanceG:   l.BalanceG,
			RemainingG:    l.BalanceG - take,
			FirstReceived: l.FirstReceived,
			Exhausts:      l.BalanceG-take == 0,
		})
		left -= take
	}
	return out, nil
}

// PostFIFOIssue plans a FIFO issue and records it. Either every allocation
// line is written or none is: the plan is checked against the full group
// balance before the first line is posted, so a partly-written issue cannot
// leave the ledger inconsistent.
func (b *Book) PostFIFOIssue(count, shade string, qty Grams, date time.Time, ref string) ([]Allocation, error) {
	plan, err := b.IssueFIFO(count, shade, qty)
	if err != nil {
		return nil, err
	}
	for _, a := range plan {
		if _, err := b.Issue(a.Key, a.QtyG, date, ref); err != nil {
			return nil, fmt.Errorf("posting allocation for %s: %w", a.Key.Label(), err)
		}
	}
	return plan, nil
}

// AllocatedTotal sums an allocation plan.
func AllocatedTotal(as []Allocation) Grams {
	var total Grams
	for _, a := range as {
		total += a.QtyG
	}
	return total
}
