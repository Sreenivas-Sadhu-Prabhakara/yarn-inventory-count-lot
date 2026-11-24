package yarn

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Yarn count conversion constants. Every conversion in this tool is derived
// from these three numbers and nothing else, so there is one place to correct.
//
// A count is either indirect (length per unit mass — bigger number means finer
// yarn: Ne, Nm) or direct (mass per unit length — bigger number means coarser
// yarn: tex, denier). Converting between the two families is a reciprocal, not
// a multiplication, which is the mistake this panel exists to prevent.
const (
	// NmTexFactor converts between metric count and tex:
	// tex = NmTexFactor / Nm. Nm is metres per gram, tex is grams per 1000 m,
	// so the factor is exactly 1000 by definition.
	NmTexFactor = 1000.0

	// NeTexFactor converts between English cotton count and tex:
	// Ne = NeTexFactor / tex. Ne is the number of 840-yard hanks per pound, so
	// the factor is 453.59237 g / (840 x 0.9144 m) x 1000 = 590.5412474...
	// Mill practice universally rounds it to 590.5, which is what this tool
	// uses: conversions stay within 0.007% of exact.
	NeTexFactor = 590.5

	// DenierPerTex converts between denier and tex: denier = 9 x tex. Denier is
	// grams per 9000 m, tex is grams per 1000 m, so the factor is exactly 9.
	DenierPerTex = 9.0
)

// System names a yarn count system.
type System string

// The four count systems this tool converts between.
const (
	SystemNe     System = "Ne"
	SystemNm     System = "Nm"
	SystemTex    System = "tex"
	SystemDenier System = "denier"
)

// Systems lists the supported count systems in panel order.
func Systems() []System { return []System{SystemNe, SystemNm, SystemTex, SystemDenier} }

var (
	// ErrBadCountValue is returned for a count value that cannot be converted.
	ErrBadCountValue = errors.New("count value must be a positive number")
	// ErrUnknownSystem is returned for a count system this tool does not know.
	ErrUnknownSystem = errors.New("unknown count system")
	// ErrBadCountString is returned for a count string that cannot be parsed.
	ErrBadCountString = errors.New("unreadable yarn count")
)

// --- the six primitive conversions -----------------------------------------

// TexFromNm converts metric count to tex: tex = 1000 / Nm.
func TexFromNm(nm float64) float64 { return NmTexFactor / nm }

// NmFromTex converts tex to metric count: Nm = 1000 / tex.
func NmFromTex(tex float64) float64 { return NmTexFactor / tex }

// NeFromTex converts tex to English cotton count: Ne = 590.5 / tex.
func NeFromTex(tex float64) float64 { return NeTexFactor / tex }

// TexFromNe converts English cotton count to tex: tex = 590.5 / Ne.
func TexFromNe(ne float64) float64 { return NeTexFactor / ne }

// DenierFromTex converts tex to denier: denier = 9 x tex.
func DenierFromTex(tex float64) float64 { return DenierPerTex * tex }

// TexFromDenier converts denier to tex: tex = denier / 9.
func TexFromDenier(den float64) float64 { return den / DenierPerTex }

// --- the panel --------------------------------------------------------------

// Conversion is one count expressed in all four systems.
type Conversion struct {
	Input  float64 `json:"input"`
	System System  `json:"system"`
	Ne     float64 `json:"ne"`
	Nm     float64 `json:"nm"`
	Tex    float64 `json:"tex"`
	Denier float64 `json:"denier"`
}

// Convert expresses one count value in all four systems. Tex is the hub: every
// input is reduced to tex first, then fanned back out, so there is exactly one
// conversion path between any two systems.
func Convert(value float64, sys System) (Conversion, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return Conversion{}, fmt.Errorf("%w, got %v", ErrBadCountValue, value)
	}
	var tex float64
	switch sys {
	case SystemNe:
		tex = TexFromNe(value)
	case SystemNm:
		tex = TexFromNm(value)
	case SystemTex:
		tex = value
	case SystemDenier:
		tex = TexFromDenier(value)
	default:
		return Conversion{}, fmt.Errorf("%w: %q", ErrUnknownSystem, sys)
	}
	return Conversion{
		Input:  value,
		System: sys,
		Ne:     NeFromTex(tex),
		Nm:     NmFromTex(tex),
		Tex:    tex,
		Denier: DenierFromTex(tex),
	}, nil
}

// --- folded / ply counts ----------------------------------------------------

// Count is a parsed yarn count: a singles count, optionally folded to a number
// of plies.
type Count struct {
	// Plies is 1 for a singles yarn, 2 for a two-fold, and so on.
	Plies int `json:"plies"`
	// Singles is the count of the component singles yarn, in Ne.
	Singles float64 `json:"singles"`
	// Raw is the string as the operator typed it.
	Raw string `json:"raw"`
}

// countPattern accepts an optional "<plies>/" prefix, then a positive decimal,
// then an optional "s"/"S" suffix: "30", "30s", "2/60s", "2 / 60".
var countPattern = regexp.MustCompile(`^(?:(\d{1,3})\s*/\s*)?(\d{1,4}(?:\.\d{1,3})?)\s*[sS]?$`)

// MaxPlies is the largest number of plies this tool will read. It is a sanity
// bound, not a physical limit: real folded yarns are 2, 3 or 4 ply, and cords
// rarely pass a dozen. Its real job is to catch the reversed notation. "60/2"
// is used in some trades for the same yarn this tool writes "2/60s", and
// reading it plies-first would silently take the count from 30 to 0.033, so a
// ply count this large is refused with an explanation rather than accepted.
const MaxPlies = 24

// ParseCount reads a mill-floor count string.
//
// "30s" is a singles 30s. "2/60s" is a two-fold of 60s singles, written
// plies-first as it is on an Indian and Philippine mill floor, and its
// resultant count is Ne 30. Only the plies-first form is accepted: "60/2" is
// used in some trades for the same yarn, and guessing between the two would
// silently halve or double a count, so it is rejected rather than assumed.
func ParseCount(s string) (Count, error) {
	raw := collapse(s)
	if raw == "" {
		return Count{}, fmt.Errorf("%w: empty", ErrBadCountString)
	}
	m := countPattern.FindStringSubmatch(raw)
	if m == nil {
		return Count{}, fmt.Errorf("%w: %q, expected a count like \"30s\" or \"2/60s\"", ErrBadCountString, s)
	}
	plies := 1
	if m[1] != "" {
		p, err := strconv.Atoi(m[1])
		if err != nil || p < 1 {
			return Count{}, fmt.Errorf("%w: %q has %s plies, must be 1 or more", ErrBadCountString, s, m[1])
		}
		if p > MaxPlies {
			return Count{}, fmt.Errorf("%w: %q reads as %d plies, over the limit of %d; this tool writes plies first, so a 2-fold of 60s is \"2/60s\"", ErrBadCountString, s, p, MaxPlies)
		}
		plies = p
	}
	singles, err := strconv.ParseFloat(m[2], 64)
	if err != nil || singles <= 0 {
		return Count{}, fmt.Errorf("%w: %q has a zero or negative count", ErrBadCountString, s)
	}
	return Count{Plies: plies, Singles: singles, Raw: raw}, nil
}

// Ne returns the resultant English cotton count. Ne is indirect (length per
// mass), so folding n plies together divides the count: 2/60s resolves to 30.
func (c Count) Ne() float64 { return c.Singles / float64(c.Plies) }

// Tex returns the resultant tex. Tex is direct (mass per length), so folding n
// plies together multiplies it. This agrees with Ne by construction:
// NeFromTex(c.Tex()) == c.Ne().
func (c Count) Tex() float64 { return float64(c.Plies) * TexFromNe(c.Singles) }

// Folded reports whether this is a folded (plied) yarn rather than a singles.
func (c Count) Folded() bool { return c.Plies > 1 }

// String renders the count back in mill notation.
func (c Count) String() string {
	n := strconv.FormatFloat(c.Singles, 'f', -1, 64)
	if c.Plies > 1 {
		return fmt.Sprintf("%d/%ss", c.Plies, n)
	}
	return n + "s"
}

// CountConversion is a parsed ply count with its resultant in all four systems.
type CountConversion struct {
	Count      Count      `json:"count"`
	Resultant  Conversion `json:"resultant"`
	SinglesTex float64    `json:"singlesTex"`
}

// ConvertCount parses a count string and converts its resultant count.
func ConvertCount(s string) (CountConversion, error) {
	c, err := ParseCount(s)
	if err != nil {
		return CountConversion{}, err
	}
	res, err := Convert(c.Ne(), SystemNe)
	if err != nil {
		return CountConversion{}, err
	}
	return CountConversion{Count: c, Resultant: res, SinglesTex: TexFromNe(c.Singles)}, nil
}

// RoundTo rounds a float to the given number of decimal places, half away from
// zero. Used for display only: the domain keeps full precision.
func RoundTo(v float64, places int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// ParseSystem reads a count system name, case-insensitively.
func ParseSystem(s string) (System, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ne":
		return SystemNe, nil
	case "nm":
		return SystemNm, nil
	case "tex":
		return SystemTex, nil
	case "denier", "den":
		return SystemDenier, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownSystem, s)
}
