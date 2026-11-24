package yarn

import (
	"errors"
	"strings"
	"testing"
)

func TestYarnKeyNorm(t *testing.T) {
	tests := []struct {
		name string
		in   YarnKey
		want YarnKey
	}{
		{
			"trims and collapses",
			YarnKey{Count: " 2/60s ", Shade: "  Navy   Blue ", DyeLot: " DL-1142\t", Supplier: "Sakthi  Spinners"},
			YarnKey{Count: "2/60s", Shade: "Navy Blue", DyeLot: "DL-1142", Supplier: "Sakthi Spinners"},
		},
		{
			"already clean",
			YarnKey{Count: "30s", Shade: "White", DyeLot: "RFD", Supplier: "Local"},
			YarnKey{Count: "30s", Shade: "White", DyeLot: "RFD", Supplier: "Local"},
		},
		{
			"empty supplier stays empty",
			YarnKey{Count: "30s", Shade: "White", DyeLot: "RFD", Supplier: "   "},
			YarnKey{Count: "30s", Shade: "White", DyeLot: "RFD", Supplier: ""},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Norm(); got != tc.want {
				t.Errorf("Norm() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestYarnKeyID is the test that keeps one physical lot on one balance. If
// capitalisation or stray spacing split a lot in two, the floor would be shown
// two half-balances for one set of cones.
func TestYarnKeyID(t *testing.T) {
	base := YarnKey{Count: "2/60s", Shade: "Navy", DyeLot: "DL-1142", Supplier: "Sakthi Spinners"}
	same := []struct {
		name string
		key  YarnKey
	}{
		{"identical", base},
		{"different case", YarnKey{Count: "2/60S", Shade: "NAVY", DyeLot: "dl-1142", Supplier: "SAKTHI SPINNERS"}},
		{"extra whitespace", YarnKey{Count: " 2/60s", Shade: "Navy ", DyeLot: "  DL-1142", Supplier: "Sakthi  Spinners"}},
	}
	for _, tc := range same {
		t.Run("same/"+tc.name, func(t *testing.T) {
			if tc.key.ID() != base.ID() {
				t.Errorf("ID() = %q, want it to equal %q", tc.key.ID(), base.ID())
			}
		})
	}

	different := []struct {
		name string
		key  YarnKey
	}{
		{"different dye-lot", YarnKey{Count: "2/60s", Shade: "Navy", DyeLot: "DL-1143", Supplier: "Sakthi Spinners"}},
		{"different count", YarnKey{Count: "30s", Shade: "Navy", DyeLot: "DL-1142", Supplier: "Sakthi Spinners"}},
		{"different shade", YarnKey{Count: "2/60s", Shade: "Royal", DyeLot: "DL-1142", Supplier: "Sakthi Spinners"}},
		{"different supplier", YarnKey{Count: "2/60s", Shade: "Navy", DyeLot: "DL-1142", Supplier: "Other Mills"}},
	}
	for _, tc := range different {
		t.Run("different/"+tc.name, func(t *testing.T) {
			if tc.key.ID() == base.ID() {
				t.Errorf("ID() = %q, want it to differ from %q", tc.key.ID(), base.ID())
			}
		})
	}
}

// TestYarnKeyIDNoFieldBleed checks the field separator does its job: moving a
// character across a field boundary must change the identity.
func TestYarnKeyIDNoFieldBleed(t *testing.T) {
	a := YarnKey{Count: "30s", Shade: "Navy", DyeLot: "A", Supplier: "B"}
	b := YarnKey{Count: "30s", Shade: "Navy", DyeLot: "AB", Supplier: ""}
	if a.ID() == b.ID() {
		t.Errorf("keys %+v and %+v share the ID %q", a, b, a.ID())
	}
}

func TestYarnKeyValidate(t *testing.T) {
	tests := []struct {
		name        string
		key         YarnKey
		wantErr     bool
		wantMention []string
	}{
		{"complete", YarnKey{Count: "30s", Shade: "Navy", DyeLot: "DL-1", Supplier: "S"}, false, nil},
		{"supplier optional", YarnKey{Count: "30s", Shade: "Navy", DyeLot: "DL-1"}, false, nil},
		{"no count", YarnKey{Shade: "Navy", DyeLot: "DL-1"}, true, []string{"count"}},
		{"no shade", YarnKey{Count: "30s", DyeLot: "DL-1"}, true, []string{"shade"}},
		{"no dye-lot", YarnKey{Count: "30s", Shade: "Navy"}, true, []string{"dye-lot"}},
		{"blank dye-lot", YarnKey{Count: "30s", Shade: "Navy", DyeLot: "   "}, true, []string{"dye-lot"}},
		{"nothing at all", YarnKey{}, true, []string{"count", "shade", "dye-lot"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.key.Validate()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("Validate(): unexpected error %v", err)
				}
				return
			}
			if !errors.Is(err, ErrIncompleteKey) {
				t.Fatalf("Validate() error = %v, want ErrIncompleteKey", err)
			}
			for _, m := range tc.wantMention {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("error %q does not name the missing field %q", err.Error(), m)
				}
			}
		})
	}
}

func TestYarnKeyLabel(t *testing.T) {
	withSupplier := YarnKey{Count: "2/60s", Shade: "Navy", DyeLot: "DL-1142", Supplier: "Sakthi Spinners"}
	got := withSupplier.Label()
	for _, want := range []string{"2/60s", "Navy", "DL-1142", "Sakthi Spinners"} {
		if !strings.Contains(got, want) {
			t.Errorf("Label() = %q, missing %q", got, want)
		}
	}
	noSupplier := YarnKey{Count: "30s", Shade: "White", DyeLot: "RFD"}
	if strings.Contains(noSupplier.Label(), "supplier") {
		t.Errorf("Label() = %q, should omit the supplier when blank", noSupplier.Label())
	}
}

// TestYarnKeyGroup checks lots pool into a count+shade group regardless of lot
// and supplier, which is the level a production order is raised at.
func TestYarnKeyGroup(t *testing.T) {
	a := YarnKey{Count: "30s", Shade: "Navy", DyeLot: "DL-1", Supplier: "One"}
	b := YarnKey{Count: "30s", Shade: "navy", DyeLot: "DL-2", Supplier: "Two"}
	c := YarnKey{Count: "40s", Shade: "Navy", DyeLot: "DL-1", Supplier: "One"}
	d := YarnKey{Count: "30s", Shade: "Royal", DyeLot: "DL-1", Supplier: "One"}

	if a.Group() != b.Group() {
		t.Errorf("same count+shade in different lots should share a group: %q vs %q", a.Group(), b.Group())
	}
	if a.Group() == c.Group() {
		t.Error("different counts should not share a group")
	}
	if a.Group() == d.Group() {
		t.Error("different shades should not share a group")
	}
	if a.Group() != shadeGroup("  30S ", "NAVY") {
		t.Errorf("shadeGroup is not normalising: %q vs %q", a.Group(), shadeGroup("  30S ", "NAVY"))
	}
}
