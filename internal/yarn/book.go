package yarn

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// TxnType is the kind of stock movement.
type TxnType string

// The three movements a yarn store records.
const (
	TxnReceipt TxnType = "receipt"
	TxnIssue   TxnType = "issue"
	TxnAdjust  TxnType = "adjust"
)

// Valid reports whether the movement type is one this tool records.
func (t TxnType) Valid() bool {
	return t == TxnReceipt || t == TxnIssue || t == TxnAdjust
}

// Txn is one stock movement against one dye-lot.
//
// QtyG is always recorded as the operator entered it: a receipt and an issue
// are both positive magnitudes, and only an adjustment carries a sign. The
// conversion to a signed effect on the balance lives in Delta, so the sign rule
// is written once.
type Txn struct {
	Seq  int64     `json:"seq"`
	Type TxnType   `json:"type"`
	QtyG Grams     `json:"qtyG"`
	Date time.Time `json:"date"`
	Ref  string    `json:"ref"`
	Key  YarnKey   `json:"key"`
}

// Delta is the signed effect of this movement on the lot balance.
func (t Txn) Delta() Grams {
	if t.Type == TxnIssue {
		return -t.QtyG
	}
	return t.QtyG
}

// Errors returned by movement posting.
var (
	// ErrNonPositiveQty is returned when a receipt or issue is zero or less.
	ErrNonPositiveQty = errors.New("quantity must be more than zero")
	// ErrZeroAdjust is returned for an adjustment of zero, which records nothing.
	ErrZeroAdjust = errors.New("an adjustment must be a non-zero quantity")
	// ErrBadTxnType is returned for an unknown movement type.
	ErrBadTxnType = errors.New("unknown movement type")
)

// ShortfallError reports a movement refused because it would drive a lot (or a
// whole count+shade group) below zero. Physical yarn cannot go negative: a
// negative balance in the book means either the issue slip or an earlier
// receipt is wrong, and both need a human, so the movement is refused and the
// exact shortfall named.
type ShortfallError struct {
	Key       YarnKey
	Requested Grams
	Available Grams
	// Group is set when the shortfall is across all lots of a count+shade
	// rather than against one named dye-lot.
	Group bool
}

// Shortfall is how much more yarn would be needed for the movement to post.
func (e *ShortfallError) Shortfall() Grams { return e.Requested - e.Available }

func (e *ShortfallError) Error() string {
	where := e.Key.Label()
	if e.Group {
		where = fmt.Sprintf("count %s, shade %s (all dye-lots)", collapse(e.Key.Count), collapse(e.Key.Shade))
	}
	return fmt.Sprintf("insufficient stock for %s: asked for %s, only %s on hand, short by %s",
		where, e.Requested, e.Available, e.Shortfall())
}

// Book is the stock ledger: every movement, plus the reorder settings the
// alerts are judged against. It is the single persisted value.
type Book struct {
	Txns  []Txn         `json:"txns"`
	Rules []ReorderRule `json:"rules"`
	// AgedAfterDays is the lot age, in days, past which a lot still holding
	// stock is flagged for shade-drift risk. Zero means use DefaultAgedLotDays.
	AgedAfterDays int `json:"agedAfterDays"`

	nextSeq int64
}

// NewBook returns an empty book with the default aged-lot threshold.
func NewBook() *Book {
	return &Book{AgedAfterDays: DefaultAgedLotDays, nextSeq: 1}
}

// Normalize repairs a book loaded from disk: it normalises keys and dates,
// restores the posting sequence, and sorts the ledger into posting order. It is
// safe to call on a zero Book.
func (b *Book) Normalize() {
	var max int64
	for i := range b.Txns {
		b.Txns[i].Key = b.Txns[i].Key.Norm()
		b.Txns[i].Date = Day(b.Txns[i].Date)
		if b.Txns[i].Seq > max {
			max = b.Txns[i].Seq
		}
	}
	for i := range b.Txns {
		if b.Txns[i].Seq == 0 {
			max++
			b.Txns[i].Seq = max
		}
	}
	b.nextSeq = max + 1
	for i := range b.Rules {
		b.Rules[i].Count = collapse(b.Rules[i].Count)
		b.Rules[i].Shade = collapse(b.Rules[i].Shade)
	}
	if b.AgedAfterDays <= 0 {
		b.AgedAfterDays = DefaultAgedLotDays
	}
	sort.SliceStable(b.Txns, func(i, j int) bool { return b.Txns[i].Seq < b.Txns[j].Seq })
}

// agedDays is the effective aged-lot threshold.
func (b *Book) agedDays() int {
	if b.AgedAfterDays > 0 {
		return b.AgedAfterDays
	}
	return DefaultAgedLotDays
}

// Balance is the stock on hand for one dye-lot: receipts, less issues, plus or
// minus adjustments.
func (b *Book) Balance(key YarnKey) Grams {
	id := key.ID()
	var total Grams
	for _, t := range b.Txns {
		if t.Key.ID() == id {
			total += t.Delta()
		}
	}
	return total
}

// GroupBalance is the stock on hand across every dye-lot of one count+shade.
// It is what the floor thinks it has; Balance is what it can actually use
// without banding the fabric.
func (b *Book) GroupBalance(count, shade string) Grams {
	g := shadeGroup(count, shade)
	var total Grams
	for _, t := range b.Txns {
		if t.Key.Group() == g {
			total += t.Delta()
		}
	}
	return total
}

// Lot is the rolled-up position of one dye-lot.
type Lot struct {
	Key           YarnKey   `json:"key"`
	BalanceG      Grams     `json:"balanceG"`
	ReceivedG     Grams     `json:"receivedG"`
	IssuedG       Grams     `json:"issuedG"`
	AdjustedG     Grams     `json:"adjustedG"`
	FirstReceived time.Time `json:"firstReceived"`
	LastMovement  time.Time `json:"lastMovement"`

	firstSeq int64
}

// Lots rolls the ledger up per dye-lot, oldest receipt first. Lots that have
// been fully consumed are included with a zero balance so the register still
// accounts for them.
func (b *Book) Lots() []Lot {
	idx := map[string]*Lot{}
	var order []string
	for _, t := range b.Txns {
		id := t.Key.ID()
		l, ok := idx[id]
		if !ok {
			l = &Lot{Key: t.Key.Norm(), FirstReceived: time.Time{}, firstSeq: t.Seq}
			idx[id] = l
			order = append(order, id)
		}
		l.BalanceG += t.Delta()
		switch t.Type {
		case TxnReceipt:
			l.ReceivedG += t.QtyG
			if l.FirstReceived.IsZero() || t.Date.Before(l.FirstReceived) {
				l.FirstReceived = Day(t.Date)
				l.firstSeq = t.Seq
			}
		case TxnIssue:
			l.IssuedG += t.QtyG
		case TxnAdjust:
			l.AdjustedG += t.QtyG
		}
		if d := Day(t.Date); d.After(l.LastMovement) {
			l.LastMovement = d
		}
	}
	out := make([]Lot, 0, len(order))
	for _, id := range order {
		out = append(out, *idx[id])
	}
	sortLotsFIFO(out)
	return out
}

// sortLotsFIFO orders lots oldest receipt first. Lots with no receipt at all
// (opened by an adjustment) sort last. Ties break on posting sequence, so the
// order is total and the same book always allocates the same way.
func sortLotsFIFO(ls []Lot) {
	sort.SliceStable(ls, func(i, j int) bool {
		a, c := ls[i], ls[j]
		if a.FirstReceived.IsZero() != c.FirstReceived.IsZero() {
			return !a.FirstReceived.IsZero()
		}
		if !a.FirstReceived.Equal(c.FirstReceived) {
			return a.FirstReceived.Before(c.FirstReceived)
		}
		return a.firstSeq < c.firstSeq
	})
}

// Lot returns the rolled-up position of one dye-lot.
func (b *Book) Lot(key YarnKey) (Lot, bool) {
	id := key.ID()
	for _, l := range b.Lots() {
		if l.Key.ID() == id {
			return l, true
		}
	}
	return Lot{}, false
}

// Groups lists the count+shade pairs in the book with their pooled balance,
// in count then shade order.
type Group struct {
	Count    string `json:"count"`
	Shade    string `json:"shade"`
	BalanceG Grams  `json:"balanceG"`
	Lots     int    `json:"lots"`
	// OpenLots counts the dye-lots still holding stock. More than one open lot
	// in a group is the shade-banding risk this tool exists to surface.
	OpenLots int `json:"openLots"`
}

// Groups rolls the ledger up per count+shade.
func (b *Book) Groups() []Group {
	idx := map[string]*Group{}
	for _, l := range b.Lots() {
		g := l.Key.Group()
		if _, ok := idx[g]; !ok {
			idx[g] = &Group{Count: l.Key.Count, Shade: l.Key.Shade}
		}
		idx[g].BalanceG += l.BalanceG
		idx[g].Lots++
		if l.BalanceG > 0 {
			idx[g].OpenLots++
		}
	}
	out := make([]Group, 0, len(idx))
	for _, g := range idx {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count < out[j].Count
		}
		return out[i].Shade < out[j].Shade
	})
	return out
}

// append records a movement and returns it.
func (b *Book) append(t Txn) Txn {
	if b.nextSeq == 0 {
		b.Normalize()
	}
	t.Seq = b.nextSeq
	b.nextSeq++
	t.Key = t.Key.Norm()
	t.Date = Day(t.Date)
	t.Ref = collapse(t.Ref)
	b.Txns = append(b.Txns, t)
	return t
}

// Receive books yarn in against a dye-lot.
func (b *Book) Receive(key YarnKey, qty Grams, date time.Time, ref string) (Txn, error) {
	if err := key.Validate(); err != nil {
		return Txn{}, err
	}
	if qty <= 0 {
		return Txn{}, fmt.Errorf("receipt: %w", ErrNonPositiveQty)
	}
	return b.append(Txn{Type: TxnReceipt, QtyG: qty, Date: date, Ref: ref, Key: key}), nil
}

// Issue books yarn out of one named dye-lot. It refuses to drive the lot
// negative.
func (b *Book) Issue(key YarnKey, qty Grams, date time.Time, ref string) (Txn, error) {
	if err := key.Validate(); err != nil {
		return Txn{}, err
	}
	if qty <= 0 {
		return Txn{}, fmt.Errorf("issue: %w", ErrNonPositiveQty)
	}
	if have := b.Balance(key); qty > have {
		return Txn{}, &ShortfallError{Key: key.Norm(), Requested: qty, Available: have}
	}
	return b.append(Txn{Type: TxnIssue, QtyG: qty, Date: date, Ref: ref, Key: key}), nil
}

// Adjust books a signed correction against one dye-lot: a positive quantity for
// yarn found, a negative one for a short count or damage written off. A
// negative adjustment is held to the same rule as an issue and may not drive
// the lot below zero.
func (b *Book) Adjust(key YarnKey, qty Grams, date time.Time, ref string) (Txn, error) {
	if err := key.Validate(); err != nil {
		return Txn{}, err
	}
	if qty == 0 {
		return Txn{}, ErrZeroAdjust
	}
	if qty < 0 {
		if have := b.Balance(key); -qty > have {
			return Txn{}, &ShortfallError{Key: key.Norm(), Requested: -qty, Available: have}
		}
	}
	return b.append(Txn{Type: TxnAdjust, QtyG: qty, Date: date, Ref: ref, Key: key}), nil
}

// Register returns the issue register: every issue, newest first, with the
// posting sequence breaking ties within a day.
func (b *Book) Register() []Txn {
	out := []Txn{}
	for _, t := range b.Txns {
		if t.Type == TxnIssue {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.After(out[j].Date)
		}
		return out[i].Seq > out[j].Seq
	})
	return out
}

// IssuedBetween totals the yarn issued for one count+shade over a date range,
// inclusive of both ends. This is the "actual issued" figure a consumption
// variance is measured against.
func (b *Book) IssuedBetween(count, shade string, from, to time.Time) Grams {
	g := shadeGroup(count, shade)
	f, t2 := Day(from), Day(to)
	var total Grams
	for _, t := range b.Txns {
		if t.Type != TxnIssue || t.Key.Group() != g {
			continue
		}
		d := Day(t.Date)
		if d.Before(f) || d.After(t2) {
			continue
		}
		total += t.QtyG
	}
	return total
}
