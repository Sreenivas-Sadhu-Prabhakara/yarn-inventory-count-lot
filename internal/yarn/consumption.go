package yarn

import (
	"errors"
	"fmt"
	"math"
)

// ErrBadConsumptionInput is returned for a negative or non-finite production
// figure.
var ErrBadConsumptionInput = errors.New("production figures must be zero or more")

// ExpectedConsumption is the standard yarn requirement for a production run:
// the units produced times the standard grams of yarn per unit, rounded to the
// whole gram.
func ExpectedConsumption(productionUnits, gramsPerUnit float64) (Grams, error) {
	for name, v := range map[string]float64{"production units": productionUnits, "grams per unit": gramsPerUnit} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return 0, fmt.Errorf("%w: %s is %v", ErrBadConsumptionInput, name, v)
		}
	}
	g := math.Round(productionUnits * gramsPerUnit)
	if g > float64(math.MaxInt64/2) {
		return 0, fmt.Errorf("%w: %v units x %v g is out of range", ErrBadConsumptionInput, productionUnits, gramsPerUnit)
	}
	return Grams(g), nil
}

// Variance compares the yarn a run should have taken against the yarn actually
// issued to it.
//
// Two percentages are reported because the mill floor uses both and they are
// not the same number:
//
//   - OverStandardPct measures the excess against the standard, which is the
//     figure to judge a costing or a standard-allowance against.
//   - WastagePct measures the excess as a share of what was issued, which is
//     the conventional mill wastage percentage.
//
// Each carries its own "defined" flag, because the denominator can legitimately
// be zero: a run that produced nothing has no standard to be over, and a run
// that drew no yarn has no issue to waste. Neither case is an error and neither
// divides by zero.
type Variance struct {
	ProductionUnits float64 `json:"productionUnits"`
	GramsPerUnit    float64 `json:"gramsPerUnit"`
	ExpectedG       Grams   `json:"expectedG"`
	ActualG         Grams   `json:"actualG"`
	// VarianceG is actual less expected: positive is over-consumption.
	VarianceG Grams `json:"varianceG"`

	OverStandardPct     float64 `json:"overStandardPct"`
	OverStandardDefined bool    `json:"overStandardDefined"`
	WastagePct          float64 `json:"wastagePct"`
	WastageDefined      bool    `json:"wastageDefined"`
}

// Over reports whether the run consumed more than its standard.
func (v Variance) Over() bool { return v.VarianceG > 0 }

// CompareConsumption measures actual issued yarn against the standard
// requirement for a production run.
func CompareConsumption(productionUnits, gramsPerUnit float64, actual Grams) (Variance, error) {
	expected, err := ExpectedConsumption(productionUnits, gramsPerUnit)
	if err != nil {
		return Variance{}, err
	}
	if actual < 0 {
		return Variance{}, fmt.Errorf("%w: actual issued is %s", ErrBadConsumptionInput, actual)
	}
	v := Variance{
		ProductionUnits: productionUnits,
		GramsPerUnit:    gramsPerUnit,
		ExpectedG:       expected,
		ActualG:         actual,
		VarianceG:       actual - expected,
	}
	if expected > 0 {
		v.OverStandardPct = float64(v.VarianceG) / float64(expected) * 100
		v.OverStandardDefined = true
	}
	if actual > 0 {
		v.WastagePct = float64(v.VarianceG) / float64(actual) * 100
		v.WastageDefined = true
	}
	return v, nil
}
