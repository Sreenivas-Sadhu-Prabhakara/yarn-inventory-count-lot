package yarn

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Grams is a yarn quantity held as whole grams.
//
// Mill-floor scales read to the gram, and every balance in this tool is a
// running sum of many receipts and issues. Holding quantities as float64
// kilograms lets rounding error accumulate, which breaks the two rules that
// matter most here: an exact-fit issue must empty a lot to precisely zero, and
// an issue must never be able to drive a lot negative by a fraction of a gram.
// Integer grams make both exact.
type Grams int64

// gramsPerKg is the only place the kg/g factor is written.
const gramsPerKg = 1000

// ErrBadQuantity is returned when a quantity cannot be read as a number of
// kilograms.
var ErrBadQuantity = errors.New("quantity must be a number of kilograms")

// KgToGrams converts kilograms to whole grams, rounding half away from zero.
func KgToGrams(kg float64) (Grams, error) {
	if math.IsNaN(kg) || math.IsInf(kg, 0) {
		return 0, ErrBadQuantity
	}
	g := math.Round(kg * gramsPerKg)
	if math.Abs(g) > float64(math.MaxInt64/2) {
		return 0, fmt.Errorf("%w: %v kg is out of range", ErrBadQuantity, kg)
	}
	return Grams(g), nil
}

// ParseKg reads a kilogram figure typed by a user ("12.5", " 0.250 ", "1,250").
func ParseKg(s string) (Grams, error) {
	t := strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	if t == "" {
		return 0, fmt.Errorf("%w: empty", ErrBadQuantity)
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrBadQuantity, s)
	}
	return KgToGrams(f)
}

// Kg returns the quantity in kilograms.
func (g Grams) Kg() float64 { return float64(g) / gramsPerKg }

// String formats the quantity as kilograms to three decimals, which is how a
// mill-floor weighbridge slip reads.
func (g Grams) String() string {
	neg := ""
	v := g
	if v < 0 {
		neg = "-"
		v = -v
	}
	return fmt.Sprintf("%s%d.%03d kg", neg, int64(v)/gramsPerKg, int64(v)%gramsPerKg)
}

// KgString formats the quantity as a bare kilogram number, for CSV cells.
func (g Grams) KgString() string {
	return strings.TrimSuffix(g.String(), " kg")
}
