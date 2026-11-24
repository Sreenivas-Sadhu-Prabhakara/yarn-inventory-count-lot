package yarn

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Alert settings. Both are defaults the user can change: no statute fixes
// them, they are stock-control judgements that depend on the mill's own lead
// times and storage conditions.
const (
	// DefaultAgedLotDays is the age past which a dye-lot still holding stock is
	// flagged. Dyed yarn in ordinary godown conditions drifts in shade as it
	// ages, so an old part-lot is the one most likely to band against a fresh
	// lot of the same nominal shade. Ninety days is a common mill working
	// figure, not a standard; set your own from your storage and sampling
	// experience.
	DefaultAgedLotDays = 90

	// DefaultOrderUpToMultiple sets the suggested order quantity when no
	// order-up-to level has been given: the suggestion brings stock up to twice
	// the reorder level. A reorder level is only a trigger, so ordering exactly
	// the shortfall would put the item straight back on this list. Set an
	// explicit order-up-to level to match your supplier's lead time and
	// minimum dye-lot size.
	DefaultOrderUpToMultiple = 2
)

// ErrBadRule is returned for an unusable reorder rule.
var ErrBadRule = errors.New("unusable reorder rule")

// ReorderRule is the reorder point for one count+shade. The level is set per
// count+shade rather than per dye-lot: a dye-lot is consumed and gone, so a
// reorder point against one would be meaningless.
type ReorderRule struct {
	Count  string `json:"count"`
	Shade  string `json:"shade"`
	LevelG Grams  `json:"levelG"`
	// OrderUpToG is the stock level a suggested order should restore. Zero
	// means derive it from DefaultOrderUpToMultiple.
	OrderUpToG Grams `json:"orderUpToG"`
}

// Validate checks a rule can be applied. A level of zero is allowed and means
// the rule is dormant: nothing is ever below it.
func (r ReorderRule) Validate() error {
	if collapse(r.Count) == "" || collapse(r.Shade) == "" {
		return fmt.Errorf("%w: count and shade required", ErrBadRule)
	}
	if r.LevelG < 0 {
		return fmt.Errorf("%w: reorder level for %s %s is negative", ErrBadRule, r.Count, r.Shade)
	}
	if r.OrderUpToG < 0 {
		return fmt.Errorf("%w: order-up-to level for %s %s is negative", ErrBadRule, r.Count, r.Shade)
	}
	if r.OrderUpToG > 0 && r.OrderUpToG < r.LevelG {
		return fmt.Errorf("%w: order-up-to level for %s %s is below its reorder level", ErrBadRule, r.Count, r.Shade)
	}
	return nil
}

// group is the count+shade identity the rule applies to.
func (r ReorderRule) group() string { return shadeGroup(r.Count, r.Shade) }

// target is the stock level a suggested order restores, and whether it was
// derived rather than set by the user.
func (r ReorderRule) target() (Grams, bool) {
	if r.OrderUpToG > 0 {
		return r.OrderUpToG, false
	}
	return r.LevelG * DefaultOrderUpToMultiple, true
}

// ReorderAlert is one count+shade at or below its reorder level.
type ReorderAlert struct {
	Count           string `json:"count"`
	Shade           string `json:"shade"`
	OnHandG         Grams  `json:"onHandG"`
	LevelG          Grams  `json:"levelG"`
	ShortByG        Grams  `json:"shortByG"`
	OrderUpToG      Grams  `json:"orderUpToG"`
	SuggestedOrderG Grams  `json:"suggestedOrderG"`
	// TargetDerived is true when the order-up-to level was derived from the
	// default multiple rather than set by the user.
	TargetDerived bool `json:"targetDerived"`
	// OpenLots is how many dye-lots the on-hand figure is spread across.
	OpenLots int `json:"openLots"`
}

// AgedLot is a dye-lot still holding stock past the aged-lot threshold.
type AgedLot struct {
	Key           YarnKey   `json:"key"`
	BalanceG      Grams     `json:"balanceG"`
	FirstReceived time.Time `json:"firstReceived"`
	AgeDays       int       `json:"ageDays"`
	ThresholdDays int       `json:"thresholdDays"`
}

// Alerts is the full alert list for a book at a point in time.
type Alerts struct {
	AsOf          time.Time      `json:"asOf"`
	AgedAfterDays int            `json:"agedAfterDays"`
	Reorder       []ReorderAlert `json:"reorder"`
	AgedLots      []AgedLot      `json:"agedLots"`
}

// Empty reports whether there is nothing to act on.
func (a Alerts) Empty() bool { return len(a.Reorder) == 0 && len(a.AgedLots) == 0 }

// ReorderAlerts lists the count+shade items at or below their reorder level
// with a suggested order quantity, and flags dye-lots aged past the given
// threshold for shade-drift risk.
//
// Reorder alerts are driven from the rules rather than from stock on hand, so
// an item that has run out completely — and so has no lots left in the book at
// all — still raises an alert instead of silently disappearing.
func (b *Book) ReorderAlerts(rules []ReorderRule, asOf time.Time, agedAfterDays int) Alerts {
	if agedAfterDays <= 0 {
		agedAfterDays = DefaultAgedLotDays
	}
	day := Day(asOf)
	out := Alerts{AsOf: day, AgedAfterDays: agedAfterDays, Reorder: []ReorderAlert{}, AgedLots: []AgedLot{}}

	lots := b.Lots()
	onHand := map[string]Grams{}
	openLots := map[string]int{}
	for _, l := range lots {
		g := l.Key.Group()
		onHand[g] += l.BalanceG
		if l.BalanceG > 0 {
			openLots[g]++
		}
	}

	seen := map[string]bool{}
	for _, r := range rules {
		if r.Validate() != nil || r.LevelG <= 0 {
			continue
		}
		g := r.group()
		if seen[g] {
			continue
		}
		seen[g] = true

		have := onHand[g]
		if have > r.LevelG {
			continue
		}
		target, derived := r.target()
		suggest := target - have
		if suggest < 0 {
			suggest = 0
		}
		out.Reorder = append(out.Reorder, ReorderAlert{
			Count:           collapse(r.Count),
			Shade:           collapse(r.Shade),
			OnHandG:         have,
			LevelG:          r.LevelG,
			ShortByG:        r.LevelG - have,
			OrderUpToG:      target,
			SuggestedOrderG: suggest,
			TargetDerived:   derived,
			OpenLots:        openLots[g],
		})
	}
	sort.SliceStable(out.Reorder, func(i, j int) bool {
		if out.Reorder[i].ShortByG != out.Reorder[j].ShortByG {
			return out.Reorder[i].ShortByG > out.Reorder[j].ShortByG
		}
		if out.Reorder[i].Count != out.Reorder[j].Count {
			return out.Reorder[i].Count < out.Reorder[j].Count
		}
		return out.Reorder[i].Shade < out.Reorder[j].Shade
	})

	for _, l := range lots {
		if l.BalanceG <= 0 || l.FirstReceived.IsZero() {
			continue
		}
		age := DaysBetween(l.FirstReceived, day)
		if age <= agedAfterDays {
			continue
		}
		out.AgedLots = append(out.AgedLots, AgedLot{
			Key:           l.Key,
			BalanceG:      l.BalanceG,
			FirstReceived: l.FirstReceived,
			AgeDays:       age,
			ThresholdDays: agedAfterDays,
		})
	}
	sort.SliceStable(out.AgedLots, func(i, j int) bool {
		return out.AgedLots[i].AgeDays > out.AgedLots[j].AgeDays
	})
	return out
}

// CurrentAlerts runs ReorderAlerts against the rules and threshold stored in
// the book.
func (b *Book) CurrentAlerts(asOf time.Time) Alerts {
	return b.ReorderAlerts(b.Rules, asOf, b.agedDays())
}

// SetRule adds or replaces the reorder rule for a count+shade.
func (b *Book) SetRule(r ReorderRule) error {
	if err := r.Validate(); err != nil {
		return err
	}
	r.Count, r.Shade = collapse(r.Count), collapse(r.Shade)
	for i := range b.Rules {
		if b.Rules[i].group() == r.group() {
			b.Rules[i] = r
			return nil
		}
	}
	b.Rules = append(b.Rules, r)
	return nil
}

// DeleteRule removes the reorder rule for a count+shade. It reports whether a
// rule was removed.
func (b *Book) DeleteRule(count, shade string) bool {
	want := shadeGroup(count, shade)
	for i := range b.Rules {
		if b.Rules[i].group() == want {
			b.Rules = append(b.Rules[:i], b.Rules[i+1:]...)
			return true
		}
	}
	return false
}
