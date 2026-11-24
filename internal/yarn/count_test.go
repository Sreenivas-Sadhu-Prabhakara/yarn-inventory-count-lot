package yarn

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

// eps is the tolerance for float comparisons. Counts are quoted to at most
// three decimals on a mill floor, so 1e-6 is far tighter than anything that
// matters and still catches a wrong formula.
const eps = 1e-6

func near(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("%s = %.10f, want %.10f (diff %.3g)", label, got, want, math.Abs(got-want))
	}
}

// TestConstants pins the three conversion factors. If one of these changes,
// every conversion in the tool moves, so the change should be deliberate.
func TestConstants(t *testing.T) {
	if NmTexFactor != 1000.0 {
		t.Errorf("NmTexFactor = %v, want 1000", NmTexFactor)
	}
	if NeTexFactor != 590.5 {
		t.Errorf("NeTexFactor = %v, want 590.5", NeTexFactor)
	}
	if DenierPerTex != 9.0 {
		t.Errorf("DenierPerTex = %v, want 9", DenierPerTex)
	}
}

// TestPrimitiveConversions checks each of the six formulas in isolation.
func TestPrimitiveConversions(t *testing.T) {
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		// tex = 1000 / Nm
		{"tex from Nm 50", TexFromNm(50), 20},
		{"tex from Nm 1", TexFromNm(1), 1000},
		{"tex from Nm 1000", TexFromNm(1000), 1},
		{"tex from Nm 60", TexFromNm(60), 16.666666666666668},
		// Nm = 1000 / tex
		{"Nm from tex 20", NmFromTex(20), 50},
		{"Nm from tex 1000", NmFromTex(1000), 1},
		{"Nm from tex 16.666666666666668", NmFromTex(16.666666666666668), 60},
		// Ne = 590.5 / tex
		{"Ne from tex 20", NeFromTex(20), 29.525},
		{"Ne from tex 590.5", NeFromTex(590.5), 1},
		{"Ne from tex 19.683333333333334", NeFromTex(19.683333333333334), 30},
		// tex = 590.5 / Ne
		{"tex from Ne 30", TexFromNe(30), 19.683333333333334},
		{"tex from Ne 29.525", TexFromNe(29.525), 20},
		{"tex from Ne 1", TexFromNe(1), 590.5},
		{"tex from Ne 60", TexFromNe(60), 9.841666666666667},
		// denier = 9 x tex
		{"denier from tex 20", DenierFromTex(20), 180},
		{"denier from tex 16.666666666666668", DenierFromTex(16.666666666666668), 150},
		{"denier from tex 1", DenierFromTex(1), 9},
		// tex = denier / 9
		{"tex from denier 180", TexFromDenier(180), 20},
		{"tex from denier 150", TexFromDenier(150), 16.666666666666668},
		{"tex from denier 9", TexFromDenier(9), 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { near(t, tc.name, tc.got, tc.want) })
	}
}

// TestConvertAcrossSystems takes one physical yarn and asserts that entering
// it in any of the four systems produces the same other three. Each row is one
// real yarn: the figures must agree whichever box the operator typed into.
func TestConvertAcrossSystems(t *testing.T) {
	yarns := []struct {
		name                string
		ne, nm, tex, denier float64
	}{
		// 20 tex cotton: every figure here is exact.
		{"20 tex cotton", 29.525, 50, 20, 180},
		// 30s cotton, the commonest shirting count in an Indian weaving shed.
		{"Ne 30 cotton", 30, 50.804403048264185, 19.683333333333334, 177.15},
		// 150 denier polyester filament, the commonest PH knitting input.
		{"150 denier polyester", 35.43, 60, 16.666666666666668, 150},
		// A very coarse yarn: 590.5 tex is exactly Ne 1.
		{"Ne 1 coarse", 1, 1.6934801016088062, 590.5, 5314.5},
		// A very fine yarn: 100s cotton.
		{"Ne 100 fine", 100, 169.34801016088062, 5.905, 53.145},
	}
	for _, y := range yarns {
		inputs := []struct {
			sys System
			val float64
		}{
			{SystemNe, y.ne}, {SystemNm, y.nm}, {SystemTex, y.tex}, {SystemDenier, y.denier},
		}
		for _, in := range inputs {
			t.Run(y.name+" via "+string(in.sys), func(t *testing.T) {
				c, err := Convert(in.val, in.sys)
				if err != nil {
					t.Fatalf("Convert(%v, %s): unexpected error %v", in.val, in.sys, err)
				}
				near(t, "Ne", c.Ne, y.ne)
				near(t, "Nm", c.Nm, y.nm)
				near(t, "tex", c.Tex, y.tex)
				near(t, "denier", c.Denier, y.denier)
				if c.System != in.sys || c.Input != in.val {
					t.Errorf("echo = (%v, %s), want (%v, %s)", c.Input, c.System, in.val, in.sys)
				}
			})
		}
	}
}

// TestConvertRejects covers the inputs a count panel must refuse rather than
// return infinity for.
func TestConvertRejects(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		sys   System
		want  error
	}{
		{"zero Ne", 0, SystemNe, ErrBadCountValue},
		{"zero tex", 0, SystemTex, ErrBadCountValue},
		{"negative Nm", -40, SystemNm, ErrBadCountValue},
		{"NaN", math.NaN(), SystemTex, ErrBadCountValue},
		{"positive infinity", math.Inf(1), SystemTex, ErrBadCountValue},
		{"negative infinity", math.Inf(-1), SystemNe, ErrBadCountValue},
		{"unknown system", 30, System("lea"), ErrUnknownSystem},
		{"empty system", 30, System(""), ErrUnknownSystem},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Convert(tc.value, tc.sys)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Convert(%v, %q) error = %v, want %v", tc.value, tc.sys, err, tc.want)
			}
			if got != (Conversion{}) {
				t.Errorf("Convert returned %+v on error, want zero value", got)
			}
		})
	}
}

// TestParseCountValid covers the folded and singles notations, including the
// three different strings that all resolve to Ne 30.
func TestParseCountValid(t *testing.T) {
	tests := []struct {
		in          string
		plies       int
		singles     float64
		wantNe      float64
		wantTex     float64
		wantFolded  bool
		wantsString string
	}{
		{"30s", 1, 30, 30, 19.683333333333334, false, "30s"},
		{"30", 1, 30, 30, 19.683333333333334, false, "30s"},
		{"30S", 1, 30, 30, 19.683333333333334, false, "30s"},
		{"2/60s", 2, 60, 30, 19.683333333333334, true, "2/60s"},
		{"2/60", 2, 60, 30, 19.683333333333334, true, "2/60s"},
		{"3/90s", 3, 90, 30, 19.683333333333334, true, "3/90s"},
		{"2 / 40s", 2, 40, 20, 29.525, true, "2/40s"},
		{"  2/40S  ", 2, 40, 20, 29.525, true, "2/40s"},
		{"1/30s", 1, 30, 30, 19.683333333333334, false, "30s"},
		{"20.5s", 1, 20.5, 20.5, 28.80487804878049, false, "20.5s"},
		{"4/20s", 4, 20, 5, 118.1, true, "4/20s"},
		{"24/48s", 24, 48, 2, 295.25, true, "24/48s"},
		{"200s", 1, 200, 200, 2.9525, false, "200s"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			c, err := ParseCount(tc.in)
			if err != nil {
				t.Fatalf("ParseCount(%q): unexpected error %v", tc.in, err)
			}
			if c.Plies != tc.plies {
				t.Errorf("plies = %d, want %d", c.Plies, tc.plies)
			}
			near(t, "singles", c.Singles, tc.singles)
			near(t, "Ne", c.Ne(), tc.wantNe)
			near(t, "tex", c.Tex(), tc.wantTex)
			if c.Folded() != tc.wantFolded {
				t.Errorf("Folded = %v, want %v", c.Folded(), tc.wantFolded)
			}
			if c.String() != tc.wantsString {
				t.Errorf("String = %q, want %q", c.String(), tc.wantsString)
			}
			// Ne and tex are derived by different routes (divide the plies vs
			// multiply them). They must describe the same yarn.
			near(t, "Ne via tex", NeFromTex(c.Tex()), c.Ne())
		})
	}
}

// TestParseCountRejects covers the malformed strings a count field must refuse.
// A silently mis-parsed count is the worst failure this tool could have: it
// would mis-state every conversion and every consumption figure downstream.
func TestParseCountRejects(t *testing.T) {
	tests := []struct{ name, in string }{
		{"empty", ""},
		{"blank", "   "},
		{"letters only", "abc"},
		{"suffix only", "s"},
		{"zero singles", "0s"},
		{"zero singles bare", "0"},
		{"negative", "-30s"},
		{"zero plies", "0/60s"},
		{"zero singles folded", "2/0s"},
		{"three parts", "2/60/40"},
		{"trailing slash", "2/"},
		{"leading slash", "/60s"},
		{"junk suffix", "30x"},
		{"double suffix", "30ss"},
		{"exponent", "1e3s"},
		{"reversed notation", "60/2"},
		{"plies over limit", "25/40s"},
		{"decimal plies", "2.5/60s"},
		{"comma", "2,60s"},
		{"range", "30-40s"},
		{"count with unit", "30 tex"},
		{"too many decimals", "30.1234s"},
		{"five digits", "30000s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCount(tc.in)
			if !errors.Is(err, ErrBadCountString) {
				t.Fatalf("ParseCount(%q) = %+v, error %v; want ErrBadCountString", tc.in, got, err)
			}
			if got != (Count{}) {
				t.Errorf("ParseCount(%q) returned %+v on error, want zero value", tc.in, got)
			}
		})
	}
}

// TestParseCountReversedNotationMessage checks the reversed-notation refusal
// actually tells the operator how to write it, since that is the whole point of
// refusing instead of guessing.
func TestParseCountReversedNotationMessage(t *testing.T) {
	_, err := ParseCount("60/2")
	if err == nil {
		t.Fatal("ParseCount(\"60/2\") succeeded, want an error")
	}
	for _, want := range []string{"2/60s", "plies first"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}
}

// TestConvertCount checks a ply string converts through to all four systems.
func TestConvertCount(t *testing.T) {
	cc, err := ConvertCount("2/60s")
	if err != nil {
		t.Fatalf("ConvertCount: unexpected error %v", err)
	}
	if cc.Count.Plies != 2 {
		t.Errorf("plies = %d, want 2", cc.Count.Plies)
	}
	near(t, "resultant Ne", cc.Resultant.Ne, 30)
	near(t, "resultant tex", cc.Resultant.Tex, 19.683333333333334)
	near(t, "resultant denier", cc.Resultant.Denier, 177.15)
	near(t, "resultant Nm", cc.Resultant.Nm, 50.804403048264185)
	// The singles are 60s, so each component thread is half the resultant tex.
	near(t, "singles tex", cc.SinglesTex, 9.841666666666667)
	near(t, "singles x plies", cc.SinglesTex*2, cc.Resultant.Tex)

	if _, err := ConvertCount("nonsense"); !errors.Is(err, ErrBadCountString) {
		t.Errorf("ConvertCount(\"nonsense\") error = %v, want ErrBadCountString", err)
	}
}

// TestRoundTo covers display rounding, including the half-way cases and the
// negative side.
func TestRoundTo(t *testing.T) {
	tests := []struct {
		in     float64
		places int
		want   float64
	}{
		{19.683333333333334, 3, 19.683},
		{19.6835, 3, 19.684},
		{29.525, 2, 29.53},
		{29.524, 2, 29.52},
		{0.5, 0, 1},
		{1.5, 0, 2},
		{2.5, 0, 3},
		{-0.5, 0, -1},
		{-19.6835, 3, -19.684},
		{177.15, 1, 177.2},
		{0, 3, 0},
		{5314.5, 0, 5315},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%v to %ddp", tc.in, tc.places), func(t *testing.T) {
			if got := RoundTo(tc.in, tc.places); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("RoundTo(%v, %d) = %v, want %v", tc.in, tc.places, got, tc.want)
			}
		})
	}
	if got := RoundTo(math.NaN(), 2); !math.IsNaN(got) {
		t.Errorf("RoundTo(NaN) = %v, want NaN", got)
	}
	if got := RoundTo(math.Inf(1), 2); !math.IsInf(got, 1) {
		t.Errorf("RoundTo(+Inf) = %v, want +Inf", got)
	}
}

func TestParseSystem(t *testing.T) {
	tests := []struct {
		in      string
		want    System
		wantErr bool
	}{
		{"Ne", SystemNe, false},
		{"ne", SystemNe, false},
		{" NE ", SystemNe, false},
		{"Nm", SystemNm, false},
		{"nm", SystemNm, false},
		{"tex", SystemTex, false},
		{"TEX", SystemTex, false},
		{"denier", SystemDenier, false},
		{"den", SystemDenier, false},
		{"lea", "", true},
		{"", "", true},
		{"worsted", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseSystem(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrUnknownSystem) {
					t.Fatalf("ParseSystem(%q) error = %v, want ErrUnknownSystem", tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSystem(%q): unexpected error %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseSystem(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	if len(Systems()) != 4 {
		t.Errorf("Systems() has %d entries, want 4", len(Systems()))
	}
}
