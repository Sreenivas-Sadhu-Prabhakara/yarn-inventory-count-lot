package yarn

import (
	"errors"
	"strings"
	"testing"
)

// TestParseReceiptsCSV covers a clean import, including the day-first dates and
// thousands separators a mill spreadsheet contains.
func TestParseReceiptsCSV(t *testing.T) {
	in := `date,count,shade,dye_lot,supplier,kg,ref
2026-01-05,30s,Navy,DL-1142,Sakthi Spinners,248.500,INV-8891
10/01/2026,2/60s,Ecru,DL-77,Other Mills,"1,020.250",INV-8892
2026-01-20,40s,White,RFD-3,,75,
`
	got, bad, err := ParseReceiptsCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseReceiptsCSV: %v", err)
	}
	if len(bad) != 0 {
		t.Fatalf("got %d bad rows, want 0: %+v", len(bad), bad)
	}
	if len(got) != 3 {
		t.Fatalf("got %d receipts, want 3", len(got))
	}

	want := []struct {
		date, count, shade, lot, supplier, ref string
		kg                                     float64
		line                                   int
	}{
		{"2026-01-05", "30s", "Navy", "DL-1142", "Sakthi Spinners", "INV-8891", 248.5, 2},
		{"2026-01-10", "2/60s", "Ecru", "DL-77", "Other Mills", "INV-8892", 1020.25, 3},
		{"2026-01-20", "40s", "White", "RFD-3", "", "", 75, 4},
	}
	for i, w := range want {
		r := got[i]
		if FormatDate(r.Date) != w.date {
			t.Errorf("row %d date = %s, want %s", i, FormatDate(r.Date), w.date)
		}
		if r.Key.Count != w.count || r.Key.Shade != w.shade || r.Key.DyeLot != w.lot || r.Key.Supplier != w.supplier {
			t.Errorf("row %d key = %+v, want %s/%s/%s/%s", i, r.Key, w.count, w.shade, w.lot, w.supplier)
		}
		if qty := g(t, w.kg); r.QtyG != qty {
			t.Errorf("row %d qty = %s, want %s", i, r.QtyG, qty)
		}
		if r.Ref != w.ref {
			t.Errorf("row %d ref = %q, want %q", i, r.Ref, w.ref)
		}
		if r.Line != w.line {
			t.Errorf("row %d line = %d, want %d", i, r.Line, w.line)
		}
	}
}

// TestParseReceiptsCSVHeaderAliases checks the columns are matched by name in
// whatever wording and order the mill's own sheet uses, and that an Excel byte
// order mark on the first cell does not break the match.
func TestParseReceiptsCSVHeaderAliases(t *testing.T) {
	bom := string(rune(byteOrderMark))
	in := bom + `Invoice No,Vendor,Colour,Yarn Count,Lot No,Weight,Receipt Date
INV-1,Sakthi Spinners,Navy,30s,DL-1142,248.5,05/01/2026
`
	got, bad, err := ParseReceiptsCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseReceiptsCSV: %v", err)
	}
	if len(bad) != 0 {
		t.Fatalf("got %d bad rows, want 0: %+v", len(bad), bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d receipts, want 1", len(got))
	}
	r := got[0]
	if r.Key.Count != "30s" || r.Key.Shade != "Navy" || r.Key.DyeLot != "DL-1142" || r.Key.Supplier != "Sakthi Spinners" {
		t.Errorf("key = %+v", r.Key)
	}
	if r.Ref != "INV-1" {
		t.Errorf("ref = %q, want INV-1", r.Ref)
	}
	if got, want := r.QtyG, g(t, 248.5); got != want {
		t.Errorf("qty = %s, want %s", got, want)
	}
	if FormatDate(r.Date) != "2026-01-05" {
		t.Errorf("date = %s, want 2026-01-05", FormatDate(r.Date))
	}
}

// TestParseReceiptsCSVBadRows is the resilience rule: a sheet with a few typos
// must still import the rows that are good, and name the lines that are not.
func TestParseReceiptsCSVBadRows(t *testing.T) {
	in := `date,count,shade,dye_lot,supplier,kg,ref
2026-01-05,30s,Navy,DL-1,S,100,INV-1
not-a-date,30s,Navy,DL-2,S,100,INV-2
2026-01-06,30s,,DL-3,S,100,INV-3
2026-01-07,30s,Navy,,S,100,INV-4
2026-01-08,30s,Navy,DL-5,S,abc,INV-5
2026-01-09,30s,Navy,DL-6,S,0,INV-6
2026-01-10,30s,Navy,DL-7,S,-50,INV-7
2026-01-11,60/2,Navy,DL-8,S,100,INV-8
2026-01-12,30s,Navy,DL-9,S,200,INV-9

2026-01-13,2/60s,Navy,DL-10,S,300,INV-10
`
	got, bad, err := ParseReceiptsCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseReceiptsCSV: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d good rows, want 3: %+v", len(got), got)
	}
	for i, want := range []string{"DL-1", "DL-9", "DL-10"} {
		if got[i].Key.DyeLot != want {
			t.Errorf("good row %d is %s, want %s", i, got[i].Key.DyeLot, want)
		}
	}

	// Lines 3 to 9 are each unusable for a different reason.
	wantBadLines := []int{3, 4, 5, 6, 7, 8, 9}
	if len(bad) != len(wantBadLines) {
		t.Fatalf("got %d bad rows, want %d: %+v", len(bad), len(wantBadLines), bad)
	}
	for i, want := range wantBadLines {
		if bad[i].Line != want {
			t.Errorf("bad row %d is line %d, want %d", i, bad[i].Line, want)
		}
		if bad[i].Message == "" {
			t.Errorf("bad row at line %d has no message", bad[i].Line)
		}
	}
	// The reversed-notation count must be caught at import, not stored.
	if !strings.Contains(bad[6].Message, "2/60s") {
		t.Errorf("line 9 message = %q, want it to explain the ply notation", bad[6].Message)
	}
}

// TestParseReceiptsCSVHeaderErrors covers the files that cannot be used at all.
func TestParseReceiptsCSVHeaderErrors(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantMention string
	}{
		{"empty file", "", ""},
		{"no kg column", "date,count,shade,dye_lot,supplier\n", "kg"},
		{"no dye-lot column", "date,count,shade,supplier,kg\n", "dyelot"},
		{"no date column", "count,shade,dye_lot,kg\n", "date"},
		{"nothing recognisable", "a,b,c\n1,2,3\n", "date"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseReceiptsCSV(strings.NewReader(tc.in))
			if !errors.Is(err, ErrNoHeader) {
				t.Fatalf("error = %v, want ErrNoHeader", err)
			}
			if tc.wantMention != "" && !strings.Contains(err.Error(), tc.wantMention) {
				t.Errorf("error %q does not name the missing column %q", err.Error(), tc.wantMention)
			}
		})
	}
}

// TestParseReceiptsCSVHeaderOnly checks a template with no data rows is not an
// error, just an empty import.
func TestParseReceiptsCSVHeaderOnly(t *testing.T) {
	got, bad, err := ParseReceiptsCSV(strings.NewReader("date,count,shade,dye_lot,supplier,kg,ref\n"))
	if err != nil {
		t.Fatalf("ParseReceiptsCSV: %v", err)
	}
	if len(got) != 0 || len(bad) != 0 {
		t.Errorf("got %d receipts and %d bad rows, want 0 and 0", len(got), len(bad))
	}
}

// TestReceiptsCSVTemplateRoundTrips checks the template offered for download is
// itself a valid import, which is the only way to know the two agree.
func TestReceiptsCSVTemplateRoundTrips(t *testing.T) {
	got, bad, err := ParseReceiptsCSV(strings.NewReader(ReceiptsCSVTemplate))
	if err != nil {
		t.Fatalf("the download template does not parse: %v", err)
	}
	if len(bad) != 0 {
		t.Fatalf("the download template has bad rows: %+v", bad)
	}
	if len(got) != 1 {
		t.Fatalf("got %d receipts from the template, want 1", len(got))
	}
	if got[0].Key.Count != "2/60s" {
		t.Errorf("template count = %q, want 2/60s", got[0].Key.Count)
	}
	if qty, want := got[0].QtyG, g(t, 248.5); qty != want {
		t.Errorf("template qty = %s, want %s", qty, want)
	}
}

// TestImportReceipts checks parsed rows post to the ledger and that a row the
// ledger refuses is reported with its original line number.
func TestImportReceipts(t *testing.T) {
	b := NewBook()
	receipts, _, err := ParseReceiptsCSV(strings.NewReader(
		"date,count,shade,dye_lot,supplier,kg,ref\n" +
			"2026-01-05,30s,Navy,DL-1,S,100,INV-1\n" +
			"2026-01-06,30s,Navy,DL-2,S,50,INV-2\n"))
	if err != nil {
		t.Fatalf("ParseReceiptsCSV: %v", err)
	}

	rep := b.ImportReceipts(receipts)
	if rep.Rows != 2 || rep.Imported != 2 || len(rep.Skipped) != 0 {
		t.Fatalf("report = %+v, want 2 rows, 2 imported, 0 skipped", rep)
	}
	if got, want := b.GroupBalance("30s", "Navy"), g(t, 150); got != want {
		t.Errorf("group balance = %s, want %s", got, want)
	}

	// A receipt the ledger itself refuses is carried back with its line.
	rep = b.ImportReceipts([]Receipt{
		{Key: navy30("DL-9"), QtyG: g(t, 10), Date: onDate(t, "2026-01-07"), Line: 42},
		{Key: YarnKey{Count: "30s", Shade: "Navy"}, QtyG: g(t, 10), Date: onDate(t, "2026-01-07"), Line: 43},
	})
	if rep.Imported != 1 {
		t.Errorf("imported = %d, want 1", rep.Imported)
	}
	if len(rep.Skipped) != 1 || rep.Skipped[0].Line != 43 {
		t.Errorf("skipped = %+v, want one entry for line 43", rep.Skipped)
	}
}

// TestWriteBalancesCSV checks the balance export, including the lot that has
// been run down to zero and so must still appear for the file to reconcile.
func TestWriteBalancesCSV(t *testing.T) {
	b := threeLots(t)
	if _, err := b.PostFIFOIssue("30s", "Navy", g(t, 120), onDate(t, "2026-02-01"), "WO-5"); err != nil {
		t.Fatalf("PostFIFOIssue: %v", err)
	}
	if _, err := b.Adjust(navy30("DL-3"), g(t, -5), onDate(t, "2026-02-02"), "damage"); err != nil {
		t.Fatalf("Adjust: %v", err)
	}

	var sb strings.Builder
	if err := WriteBalancesCSV(&sb, b.Lots(), onDate(t, "2026-04-05")); err != nil {
		t.Fatalf("WriteBalancesCSV: %v", err)
	}
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4 (header plus three lots):\n%s", len(lines), sb.String())
	}
	wantHeader := "count,shade,dye_lot,supplier,first_received,age_days,received_kg,issued_kg,adjusted_kg,balance_kg"
	if lines[0] != wantHeader {
		t.Errorf("header = %q, want %q", lines[0], wantHeader)
	}
	want := []string{
		// DL-1 received 2026-01-05, 90 days to 2026-04-05, fully issued.
		"30s,Navy,DL-1,Sakthi Spinners,2026-01-05,90,100.000,100.000,0.000,0.000",
		// DL-2 received 2026-01-10, 85 days, 20 kg of its 50 kg issued.
		"30s,Navy,DL-2,Sakthi Spinners,2026-01-10,85,50.000,20.000,0.000,30.000",
		// DL-3 received 2026-01-20, 75 days, untouched but 5 kg written off.
		"30s,Navy,DL-3,Sakthi Spinners,2026-01-20,75,75.000,0.000,-5.000,70.000",
	}
	for i, w := range want {
		if lines[i+1] != w {
			t.Errorf("line %d:\n got %q\nwant %q", i+1, lines[i+1], w)
		}
	}
}

// TestWriteBalancesCSVEmpty checks an empty book still writes a usable header.
func TestWriteBalancesCSVEmpty(t *testing.T) {
	b := NewBook()
	var sb strings.Builder
	if err := WriteBalancesCSV(&sb, b.Lots(), onDate(t, "2026-04-05")); err != nil {
		t.Fatalf("WriteBalancesCSV: %v", err)
	}
	if got := strings.Count(sb.String(), "\n"); got != 1 {
		t.Errorf("got %d lines, want just the header:\n%s", got, sb.String())
	}
}

// TestWriteBalancesCSVNoReceipt covers a lot opened by an adjustment alone,
// which has no receipt date and so no age.
func TestWriteBalancesCSVNoReceipt(t *testing.T) {
	b := NewBook()
	if _, err := b.Adjust(navy30("DL-X"), g(t, 10), onDate(t, "2026-01-01"), "opening stock"); err != nil {
		t.Fatalf("Adjust: %v", err)
	}
	var sb strings.Builder
	if err := WriteBalancesCSV(&sb, b.Lots(), onDate(t, "2026-04-05")); err != nil {
		t.Fatalf("WriteBalancesCSV: %v", err)
	}
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	want := "30s,Navy,DL-X,Sakthi Spinners,,,0.000,0.000,10.000,10.000"
	if lines[1] != want {
		t.Errorf("line = %q, want %q", lines[1], want)
	}
}

// TestWriteIssueRegisterCSV checks the register export, which is the record a
// shade complaint from a buyer is traced back through.
func TestWriteIssueRegisterCSV(t *testing.T) {
	b := threeLots(t)
	if _, err := b.PostFIFOIssue("30s", "Navy", g(t, 120), onDate(t, "2026-02-01"), "WO-5"); err != nil {
		t.Fatalf("PostFIFOIssue: %v", err)
	}
	if _, err := b.PostFIFOIssue("30s", "Navy", g(t, 10), onDate(t, "2026-03-01"), "WO-6"); err != nil {
		t.Fatalf("PostFIFOIssue: %v", err)
	}

	var sb strings.Builder
	if err := WriteIssueRegisterCSV(&sb, b.Register()); err != nil {
		t.Fatalf("WriteIssueRegisterCSV: %v", err)
	}
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4:\n%s", len(lines), sb.String())
	}
	if lines[0] != "date,ref,count,shade,dye_lot,supplier,qty_kg" {
		t.Errorf("header = %q", lines[0])
	}
	want := []string{
		"2026-03-01,WO-6,30s,Navy,DL-2,Sakthi Spinners,10.000",
		"2026-02-01,WO-5,30s,Navy,DL-2,Sakthi Spinners,20.000",
		"2026-02-01,WO-5,30s,Navy,DL-1,Sakthi Spinners,100.000",
	}
	for i, w := range want {
		if lines[i+1] != w {
			t.Errorf("line %d:\n got %q\nwant %q", i+1, lines[i+1], w)
		}
	}
}

// TestWriteIssueRegisterCSVEmpty checks a book with no issues still writes a
// header.
func TestWriteIssueRegisterCSVEmpty(t *testing.T) {
	b := NewBook()
	var sb strings.Builder
	if err := WriteIssueRegisterCSV(&sb, b.Register()); err != nil {
		t.Fatalf("WriteIssueRegisterCSV: %v", err)
	}
	if got := strings.TrimRight(sb.String(), "\n"); got != "date,ref,count,shade,dye_lot,supplier,qty_kg" {
		t.Errorf("got %q, want just the header", got)
	}
}

// TestExportQuotesAwkwardValues checks a shade or supplier containing a comma
// or a quote is escaped, so the file does not silently gain a column.
func TestExportQuotesAwkwardValues(t *testing.T) {
	b := NewBook()
	k := lotKey("30s", `Navy, dark`, `DL "A"`, "Sakthi Spinners")
	receive(t, b, k, 10, "2026-01-05")

	var sb strings.Builder
	if err := WriteBalancesCSV(&sb, b.Lots(), onDate(t, "2026-01-05")); err != nil {
		t.Fatalf("WriteBalancesCSV: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, `"Navy, dark"`) {
		t.Errorf("comma in the shade is not quoted:\n%s", out)
	}
	if !strings.Contains(out, `"DL ""A"""`) {
		t.Errorf("quotes in the dye-lot are not escaped:\n%s", out)
	}
	// Re-reading it must give back the same single row.
	rows, bad, err := ParseReceiptsCSV(strings.NewReader(
		"count,shade,dye_lot,supplier,kg,date\n" +
			`30s,"Navy, dark","DL ""A""",Sakthi Spinners,10,2026-01-05` + "\n"))
	if err != nil || len(bad) != 0 || len(rows) != 1 {
		t.Fatalf("re-reading escaped values: err=%v bad=%+v rows=%d", err, bad, len(rows))
	}
	if rows[0].Key.Shade != "Navy, dark" || rows[0].Key.DyeLot != `DL "A"` {
		t.Errorf("round trip lost the awkward values: %+v", rows[0].Key)
	}
}
