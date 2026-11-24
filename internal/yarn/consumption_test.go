package yarn

import (
	"errors"
	"math"
	"testing"
)

// TestExpectedConsumption covers the standard requirement for a run, including
// the rounding to the whole gram.
func TestExpectedConsumption(t *testing.T) {
	tests := []struct {
		name         string
		units        float64
		gramsPerUnit float64
		want         Grams
	}{
		{"1000 shirts at 250 g", 1000, 250, 250000},
		{"one unit", 1, 250, 250},
		{"zero units", 0, 250, 0},
		{"zero grams per unit", 1000, 0, 0},
		{"both zero", 0, 0, 0},
		{"fractional units", 250.5, 120.4, 30160},
		{"rounds up at the half gram", 1, 0.5, 1},
		{"rounds up above the half gram", 1, 0.6, 1},
		{"rounds down below the half gram", 1, 0.4, 0},
		{"a metre-based run", 4500, 185.5, 834750},
		{"sub-gram per unit", 10000, 0.125, 1250},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExpectedConsumption(tc.units, tc.gramsPerUnit)
			if err != nil {
				t.Fatalf("ExpectedConsumption(%v, %v): %v", tc.units, tc.gramsPerUnit, err)
			}
			if got != tc.want {
				t.Errorf("ExpectedConsumption(%v, %v) = %s (%d g), want %d g",
					tc.units, tc.gramsPerUnit, got, got, tc.want)
			}
		})
	}
}

func TestExpectedConsumptionRejects(t *testing.T) {
	tests := []struct {
		name         string
		units        float64
		gramsPerUnit float64
	}{
		{"negative units", -1, 250},
		{"negative grams per unit", 1000, -250},
		{"NaN units", math.NaN(), 250},
		{"NaN grams per unit", 1000, math.NaN()},
		{"infinite units", math.Inf(1), 250},
		{"infinite grams per unit", 1000, math.Inf(1)},
		{"overflows", 1e18, 1e18},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ExpectedConsumption(tc.units, tc.gramsPerUnit); !errors.Is(err, ErrBadConsumptionInput) {
				t.Errorf("error = %v, want ErrBadConsumptionInput", err)
			}
		})
	}
}

// TestCompareConsumption is the variance table. The zero-production rows are
// the ones that matter most: a run that made nothing still has to produce a
// readable answer rather than a division by zero.
func TestCompareConsumption(t *testing.T) {
	tests := []struct {
		name            string
		units           float64
		gramsPerUnit    float64
		actualKg        float64
		wantExpectedKg  float64
		wantVarianceKg  float64
		wantOverStd     float64
		wantOverStdOK   bool
		wantWastage     float64
		wantWastageOK   bool
		wantOverConsume bool
	}{
		{
			name: "five percent over standard",
			// 1000 shirts x 250 g = 250 kg standard, 262.5 kg actually issued.
			units: 1000, gramsPerUnit: 250, actualKg: 262.5,
			wantExpectedKg: 250, wantVarianceKg: 12.5,
			wantOverStd: 5, wantOverStdOK: true,
			// 12.5 of the 262.5 kg issued did not reach the cloth.
			wantWastage: 4.761904761904762, wantWastageOK: true,
			wantOverConsume: true,
		},
		{
			name:  "exactly on standard",
			units: 1000, gramsPerUnit: 250, actualKg: 250,
			wantExpectedKg: 250, wantVarianceKg: 0,
			wantOverStd: 0, wantOverStdOK: true,
			wantWastage: 0, wantWastageOK: true,
			wantOverConsume: false,
		},
		{
			name:  "under standard, a favourable variance",
			units: 1000, gramsPerUnit: 250, actualKg: 240,
			wantExpectedKg: 250, wantVarianceKg: -10,
			wantOverStd: -4, wantOverStdOK: true,
			wantWastage: -4.166666666666667, wantWastageOK: true,
			wantOverConsume: false,
		},
		{
			name: "zero production with yarn issued",
			// The guard case: no standard to divide by, but the yarn is gone.
			units: 0, gramsPerUnit: 250, actualKg: 5,
			wantExpectedKg: 0, wantVarianceKg: 5,
			wantOverStd: 0, wantOverStdOK: false,
			wantWastage: 100, wantWastageOK: true,
			wantOverConsume: true,
		},
		{
			name:  "zero production and nothing issued",
			units: 0, gramsPerUnit: 250, actualKg: 0,
			wantExpectedKg: 0, wantVarianceKg: 0,
			wantOverStd: 0, wantOverStdOK: false,
			wantWastage: 0, wantWastageOK: false,
			wantOverConsume: false,
		},
		{
			name:  "zero grams per unit with yarn issued",
			units: 1000, gramsPerUnit: 0, actualKg: 5,
			wantExpectedKg: 0, wantVarianceKg: 5,
			wantOverStd: 0, wantOverStdOK: false,
			wantWastage: 100, wantWastageOK: true,
			wantOverConsume: true,
		},
		{
			name:  "production booked but no yarn drawn",
			units: 1000, gramsPerUnit: 250, actualKg: 0,
			wantExpectedKg: 250, wantVarianceKg: -250,
			wantOverStd: -100, wantOverStdOK: true,
			wantWastage: 0, wantWastageOK: false,
			wantOverConsume: false,
		},
		{
			name:  "double the standard",
			units: 100, gramsPerUnit: 100, actualKg: 20,
			wantExpectedKg: 10, wantVarianceKg: 10,
			wantOverStd: 100, wantOverStdOK: true,
			wantWastage: 50, wantWastageOK: true,
			wantOverConsume: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := CompareConsumption(tc.units, tc.gramsPerUnit, g(t, tc.actualKg))
			if err != nil {
				t.Fatalf("CompareConsumption: %v", err)
			}
			if got, want := v.ExpectedG, g(t, tc.wantExpectedKg); got != want {
				t.Errorf("expected = %s, want %s", got, want)
			}
			if got, want := v.ActualG, g(t, tc.actualKg); got != want {
				t.Errorf("actual = %s, want %s", got, want)
			}
			if got, want := v.VarianceG, g(t, tc.wantVarianceKg); got != want {
				t.Errorf("variance = %s, want %s", got, want)
			}
			if v.OverStandardDefined != tc.wantOverStdOK {
				t.Errorf("OverStandardDefined = %v, want %v", v.OverStandardDefined, tc.wantOverStdOK)
			}
			if tc.wantOverStdOK {
				near(t, "OverStandardPct", v.OverStandardPct, tc.wantOverStd)
			} else if v.OverStandardPct != 0 {
				t.Errorf("OverStandardPct = %v, want 0 when undefined", v.OverStandardPct)
			}
			if v.WastageDefined != tc.wantWastageOK {
				t.Errorf("WastageDefined = %v, want %v", v.WastageDefined, tc.wantWastageOK)
			}
			if tc.wantWastageOK {
				near(t, "WastagePct", v.WastagePct, tc.wantWastage)
			} else if v.WastagePct != 0 {
				t.Errorf("WastagePct = %v, want 0 when undefined", v.WastagePct)
			}
			if v.Over() != tc.wantOverConsume {
				t.Errorf("Over() = %v, want %v", v.Over(), tc.wantOverConsume)
			}
			// The inputs are echoed back so a saved report is self-describing.
			if v.ProductionUnits != tc.units || v.GramsPerUnit != tc.gramsPerUnit {
				t.Errorf("echo = (%v, %v), want (%v, %v)", v.ProductionUnits, v.GramsPerUnit, tc.units, tc.gramsPerUnit)
			}
		})
	}
}

// TestCompareConsumptionNoDivideByZero states the guard directly: no input
// combination may produce a NaN or an infinity in either percentage.
func TestCompareConsumptionNoDivideByZero(t *testing.T) {
	for _, units := range []float64{0, 0.5, 1, 1000} {
		for _, gpu := range []float64{0, 0.001, 250} {
			for _, actual := range []Grams{0, 1, 250000} {
				v, err := CompareConsumption(units, gpu, actual)
				if err != nil {
					t.Fatalf("CompareConsumption(%v, %v, %s): %v", units, gpu, actual, err)
				}
				for name, pct := range map[string]float64{
					"OverStandardPct": v.OverStandardPct,
					"WastagePct":      v.WastagePct,
				} {
					if math.IsNaN(pct) || math.IsInf(pct, 0) {
						t.Errorf("CompareConsumption(%v, %v, %s): %s = %v", units, gpu, actual, name, pct)
					}
				}
			}
		}
	}
}

func TestCompareConsumptionRejects(t *testing.T) {
	if _, err := CompareConsumption(1000, 250, -1); !errors.Is(err, ErrBadConsumptionInput) {
		t.Errorf("negative actual: error = %v, want ErrBadConsumptionInput", err)
	}
	if _, err := CompareConsumption(-1, 250, 1000); !errors.Is(err, ErrBadConsumptionInput) {
		t.Errorf("negative units: error = %v, want ErrBadConsumptionInput", err)
	}
}

// TestCompareConsumptionAgainstTheLedger wires the variance to a real issue
// history, which is how the tool actually uses it.
func TestCompareConsumptionAgainstTheLedger(t *testing.T) {
	b := threeLots(t)
	// A February run: 150 kg of 30s Navy drawn in two issues.
	if _, err := b.PostFIFOIssue("30s", "Navy", g(t, 100), onDate(t, "2026-02-05"), "WO-1"); err != nil {
		t.Fatalf("PostFIFOIssue: %v", err)
	}
	if _, err := b.PostFIFOIssue("30s", "Navy", g(t, 50), onDate(t, "2026-02-20"), "WO-2"); err != nil {
		t.Fatalf("PostFIFOIssue: %v", err)
	}
	// One issue outside the window, which must not count.
	if _, err := b.PostFIFOIssue("30s", "Navy", g(t, 10), onDate(t, "2026-03-02"), "WO-3"); err != nil {
		t.Fatalf("PostFIFOIssue: %v", err)
	}

	actual := b.IssuedBetween("30s", "Navy", onDate(t, "2026-02-01"), onDate(t, "2026-02-28"))
	if want := g(t, 150); actual != want {
		t.Fatalf("issued in February = %s, want %s", actual, want)
	}

	// 1000 pieces were booked at a standard of 140 g each.
	v, err := CompareConsumption(1000, 140, actual)
	if err != nil {
		t.Fatalf("CompareConsumption: %v", err)
	}
	if got, want := v.ExpectedG, g(t, 140); got != want {
		t.Errorf("expected = %s, want %s", got, want)
	}
	if got, want := v.VarianceG, g(t, 10); got != want {
		t.Errorf("variance = %s, want %s", got, want)
	}
	near(t, "OverStandardPct", v.OverStandardPct, 7.142857142857143)
	near(t, "WastagePct", v.WastagePct, 6.666666666666667)
	if !v.Over() {
		t.Error("Over() = false, want true")
	}
}
