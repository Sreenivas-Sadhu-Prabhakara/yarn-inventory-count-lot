package yarn

import (
	"errors"
	"math"
	"testing"
)

// TestKgToGrams covers the kg-to-gram conversion, including the half-gram
// boundary. Rounding is half away from zero, so a 0.5 g reading never silently
// disappears from a receipt.
func TestKgToGrams(t *testing.T) {
	tests := []struct {
		name string
		kg   float64
		want Grams
	}{
		{"whole kg", 1, 1000},
		{"zero", 0, 0},
		{"half kg", 0.5, 500},
		{"one gram", 0.001, 1},
		{"a real receipt", 248.5, 248500},
		{"below half a gram rounds down", 0.0004, 0},
		{"above half a gram rounds up", 0.0006, 1},
		{"exactly half a gram rounds up", 0.0005, 1},
		{"one and a half grams", 0.0015, 2},
		{"two and a half grams", 0.0025, 3},
		{"sub-gram precision truncated", 12.3456, 12346},
		{"negative for a write-off", -1.5, -1500},
		{"negative half gram rounds away from zero", -0.0005, -1},
		{"large receipt", 25000, 25000000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := KgToGrams(tc.kg)
			if err != nil {
				t.Fatalf("KgToGrams(%v): unexpected error %v", tc.kg, err)
			}
			if got != tc.want {
				t.Errorf("KgToGrams(%v) = %d g, want %d g", tc.kg, got, tc.want)
			}
		})
	}
}

func TestKgToGramsRejects(t *testing.T) {
	for _, kg := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e18} {
		if _, err := KgToGrams(kg); !errors.Is(err, ErrBadQuantity) {
			t.Errorf("KgToGrams(%v) error = %v, want ErrBadQuantity", kg, err)
		}
	}
}

// TestParseKg covers the strings a weighbridge slip or a spreadsheet cell
// actually contains.
func TestParseKg(t *testing.T) {
	tests := []struct {
		in      string
		want    Grams
		wantErr bool
	}{
		{"12.5", 12500, false},
		{"  0.250  ", 250, false},
		{"1,250", 1250000, false},
		{"1,250.75", 1250750, false},
		{"0", 0, false},
		{"-5", -5000, false},
		{"248.500", 248500, false},
		{".5", 500, false},
		{"", 0, true},
		{"   ", 0, true},
		{"abc", 0, true},
		{"1.2.3", 0, true},
		{"12 kg", 0, true},
		{"--5", 0, true},
		{"NaN", 0, true},
		{"Inf", 0, true},
	}
	for _, tc := range tests {
		t.Run("in="+tc.in, func(t *testing.T) {
			got, err := ParseKg(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrBadQuantity) {
					t.Fatalf("ParseKg(%q) = %d, error %v; want ErrBadQuantity", tc.in, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseKg(%q): unexpected error %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseKg(%q) = %d g, want %d g", tc.in, got, tc.want)
			}
		})
	}
}

// TestGramsFormatting checks the kg rendering, including the zero-padding that
// keeps a column of figures aligned and readable.
func TestGramsFormatting(t *testing.T) {
	tests := []struct {
		g      Grams
		want   string
		wantKg float64
	}{
		{0, "0.000 kg", 0},
		{1, "0.001 kg", 0.001},
		{10, "0.010 kg", 0.010},
		{100, "0.100 kg", 0.100},
		{1000, "1.000 kg", 1},
		{1234, "1.234 kg", 1.234},
		{248500, "248.500 kg", 248.5},
		{-1500, "-1.500 kg", -1.5},
		{-1, "-0.001 kg", -0.001},
		{25000000, "25000.000 kg", 25000},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.g.String(); got != tc.want {
				t.Errorf("Grams(%d).String() = %q, want %q", tc.g, got, tc.want)
			}
			wantBare := tc.want[:len(tc.want)-3]
			if got := tc.g.KgString(); got != wantBare {
				t.Errorf("Grams(%d).KgString() = %q, want %q", tc.g, got, wantBare)
			}
			if got := tc.g.Kg(); math.Abs(got-tc.wantKg) > 1e-9 {
				t.Errorf("Grams(%d).Kg() = %v, want %v", tc.g, got, tc.wantKg)
			}
		})
	}
}

// TestGramsRoundTrip is the property that justifies holding stock in grams: a
// long run of receipts and issues must land on exactly zero, which the same
// sequence in float64 kilograms does not reliably do.
func TestGramsRoundTrip(t *testing.T) {
	var total Grams
	for i := 0; i < 1000; i++ {
		g, err := KgToGrams(0.001)
		if err != nil {
			t.Fatalf("KgToGrams: %v", err)
		}
		total += g
	}
	if total != 1000 {
		t.Fatalf("1000 x 0.001 kg = %d g, want 1000 g", total)
	}
	if total.String() != "1.000 kg" {
		t.Errorf("total = %q, want \"1.000 kg\"", total.String())
	}
	for i := 0; i < 1000; i++ {
		total -= 1
	}
	if total != 0 {
		t.Errorf("after issuing it all back, balance = %d g, want exactly 0", total)
	}
}
