package yarn

import (
	"errors"
	"fmt"
	"strings"
)

// YarnKey is the composite stock key.
//
// Yarn is not a fungible commodity. The same nominal shade from a different
// dye-lot is very rarely an exact match: put two lots through one warp or one
// knitting run and the join shows as a visible colour band in the finished
// fabric. Stock therefore has to be held per dye-lot, not pooled by shade, and
// the supplier is part of the identity because two suppliers reuse lot numbers.
type YarnKey struct {
	Count    string `json:"count"`
	Shade    string `json:"shade"`
	DyeLot   string `json:"dyeLot"`
	Supplier string `json:"supplier"`
}

// ErrIncompleteKey is returned when a stock key is missing a required field.
var ErrIncompleteKey = errors.New("incomplete yarn key")

// collapse trims a field and collapses internal runs of whitespace, so
// "  Navy  Blue " and "Navy Blue" are the same shade.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Norm returns the key with every field trimmed and its whitespace collapsed.
// Display values keep the operator's own capitalisation.
func (k YarnKey) Norm() YarnKey {
	return YarnKey{
		Count:    collapse(k.Count),
		Shade:    collapse(k.Shade),
		DyeLot:   collapse(k.DyeLot),
		Supplier: collapse(k.Supplier),
	}
}

// ID is the canonical identity of a stock key: normalised and case-folded, so
// "navy"/"Navy" and "dl-114"/"DL-114" land on one balance instead of two.
func (k YarnKey) ID() string {
	n := k.Norm()
	return strings.ToLower(strings.Join([]string{n.Count, n.Shade, n.DyeLot, n.Supplier}, "\x1f"))
}

// Validate reports whether the key can identify a lot. Count, shade and
// dye-lot are required; supplier may be blank for yarn already on the floor
// when the book is opened.
func (k YarnKey) Validate() error {
	n := k.Norm()
	var missing []string
	if n.Count == "" {
		missing = append(missing, "count")
	}
	if n.Shade == "" {
		missing = append(missing, "shade")
	}
	if n.DyeLot == "" {
		missing = append(missing, "dye-lot")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s required", ErrIncompleteKey, strings.Join(missing, ", "))
	}
	return nil
}

// Label is a one-line human description of the lot, used in error messages.
func (k YarnKey) Label() string {
	n := k.Norm()
	s := fmt.Sprintf("count %s, shade %s, dye-lot %s", n.Count, n.Shade, n.DyeLot)
	if n.Supplier != "" {
		s += ", supplier " + n.Supplier
	}
	return s
}

// shadeGroup is the identity of a count+shade pair, ignoring lot and supplier.
// It is the level a production order is raised at: the floor asks for "30s
// navy", and this tool decides which lots that comes out of.
func shadeGroup(count, shade string) string {
	return strings.ToLower(collapse(count) + "\x1f" + collapse(shade))
}

// Group returns the count+shade group this key belongs to.
func (k YarnKey) Group() string { return shadeGroup(k.Count, k.Shade) }
