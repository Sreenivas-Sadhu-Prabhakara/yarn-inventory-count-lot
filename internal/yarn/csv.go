package yarn

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrNoHeader is returned when a receipts CSV has no usable header row.
var ErrNoHeader = errors.New("receipts CSV needs a header row")

// ReceiptsCSVTemplate is the header and one example row, offered for download
// so a store clerk can fill it in a spreadsheet.
const ReceiptsCSVTemplate = "date,count,shade,dye_lot,supplier,kg,ref\n" +
	"2026-11-24,2/60s,Navy,DL-1142,Sakthi Spinners,248.500,INV-8891\n"

// columnAliases maps the header names a mill spreadsheet actually uses onto the
// fields this tool needs.
var columnAliases = map[string]string{
	"date": "date", "receipt_date": "date", "receipt date": "date", "grn_date": "date",

	"count": "count", "yarn_count": "count", "yarn count": "count", "ne": "count",

	"shade": "shade", "colour": "shade", "color": "shade", "shade_name": "shade",

	"dye_lot": "dyelot", "dyelot": "dyelot", "dye lot": "dyelot", "lot": "dyelot",
	"lot_no": "dyelot", "lot no": "dyelot", "lot_number": "dyelot", "batch": "dyelot",

	"supplier": "supplier", "vendor": "supplier", "mill": "supplier", "party": "supplier",

	"kg": "kg", "qty": "kg", "qty_kg": "kg", "quantity": "kg", "quantity_kg": "kg",
	"weight": "kg", "weight_kg": "kg", "net_kg": "kg",

	"ref": "ref", "reference": "ref", "invoice": "ref", "invoice_no": "ref",
	"bill": "ref", "bill_no": "ref", "grn": "ref",
}

// byteOrderMark is the UTF-8 BOM. A spreadsheet exported from Excel puts one in
// front of the first header cell, which would otherwise stop "date" matching.
const byteOrderMark = rune(0xFEFF)

// stripBOM removes a leading byte-order mark from a cell.
func stripBOM(s string) string { return strings.TrimPrefix(s, string(byteOrderMark)) }

// normalizeHeader maps a header cell to a field name, or "" if unrecognised.
func normalizeHeader(cell string) string {
	k := strings.ToLower(collapse(stripBOM(cell)))
	if f, ok := columnAliases[k]; ok {
		return f
	}
	// Try again with spaces as underscores, so "Dye Lot" matches "dye_lot".
	if f, ok := columnAliases[strings.ReplaceAll(k, " ", "_")]; ok {
		return f
	}
	return ""
}

// Receipt is one row of an import, already parsed and checked.
type Receipt struct {
	Key  YarnKey   `json:"key"`
	QtyG Grams     `json:"qtyG"`
	Date time.Time `json:"date"`
	Ref  string    `json:"ref"`
	Line int       `json:"line"`
}

// RowError is one unusable row of an import, named by its line number in the
// file so the clerk can find it in the spreadsheet.
type RowError struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// ImportReport is the outcome of a receipts import.
type ImportReport struct {
	Rows     int        `json:"rows"`
	Imported int        `json:"imported"`
	Skipped  []RowError `json:"skipped"`
}

// ParseReceiptsCSV reads a receipts CSV.
//
// Columns are matched by header name, not position, because no two mills export
// their columns in the same order. date, count, shade, dye-lot and kg are
// required; supplier and ref are optional. Bad rows are collected and reported
// with their line numbers rather than aborting the file: a 400-row GRN sheet
// with two typos should still import 398 rows.
func ParseReceiptsCSV(r io.Reader) ([]Receipt, []RowError, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true

	head, err := cr.Read()
	if err == io.EOF {
		return nil, nil, fmt.Errorf("%w: file is empty", ErrNoHeader)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading header: %w", err)
	}

	col := map[string]int{}
	for i, cell := range head {
		if f := normalizeHeader(cell); f != "" {
			if _, dup := col[f]; !dup {
				col[f] = i
			}
		}
	}
	var missing []string
	for _, need := range []string{"date", "count", "shade", "dyelot", "kg"} {
		if _, ok := col[need]; !ok {
			missing = append(missing, need)
		}
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("%w: no column found for %s", ErrNoHeader, strings.Join(missing, ", "))
	}

	get := func(rec []string, field string) string {
		i, ok := col[field]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	var out []Receipt
	var bad []RowError
	line := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			bad = append(bad, RowError{Line: line, Message: err.Error()})
			continue
		}
		if blankRow(rec) {
			continue
		}

		date, err := ParseDate(get(rec, "date"))
		if err != nil {
			bad = append(bad, RowError{Line: line, Message: err.Error()})
			continue
		}
		key := YarnKey{
			Count:    get(rec, "count"),
			Shade:    get(rec, "shade"),
			DyeLot:   get(rec, "dyelot"),
			Supplier: get(rec, "supplier"),
		}
		if err := key.Validate(); err != nil {
			bad = append(bad, RowError{Line: line, Message: err.Error()})
			continue
		}
		if _, err := ParseCount(key.Count); err != nil {
			bad = append(bad, RowError{Line: line, Message: err.Error()})
			continue
		}
		qty, err := ParseKg(get(rec, "kg"))
		if err != nil {
			bad = append(bad, RowError{Line: line, Message: err.Error()})
			continue
		}
		if qty <= 0 {
			bad = append(bad, RowError{Line: line, Message: fmt.Sprintf("receipt of %s: %s", qty, ErrNonPositiveQty)})
			continue
		}
		out = append(out, Receipt{Key: key.Norm(), QtyG: qty, Date: date, Ref: get(rec, "ref"), Line: line})
	}
	return out, bad, nil
}

// blankRow reports whether every cell in a record is empty.
func blankRow(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

// ImportReceipts posts parsed receipts to the book.
func (b *Book) ImportReceipts(rs []Receipt) ImportReport {
	rep := ImportReport{Rows: len(rs), Skipped: []RowError{}}
	for _, r := range rs {
		if _, err := b.Receive(r.Key, r.QtyG, r.Date, r.Ref); err != nil {
			rep.Skipped = append(rep.Skipped, RowError{Line: r.Line, Message: err.Error()})
			continue
		}
		rep.Imported++
	}
	return rep
}

// balanceHeader is the column order of a balances export.
var balanceHeader = []string{
	"count", "shade", "dye_lot", "supplier", "first_received", "age_days",
	"received_kg", "issued_kg", "adjusted_kg", "balance_kg",
}

// WriteBalancesCSV writes the per-lot balance sheet. Every lot the book has
// ever held is written, including lots now at zero, so the file reconciles
// against the receipts it came from.
func WriteBalancesCSV(w io.Writer, lots []Lot, asOf time.Time) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(balanceHeader); err != nil {
		return err
	}
	for _, l := range lots {
		first, age := "", ""
		if !l.FirstReceived.IsZero() {
			first = FormatDate(l.FirstReceived)
			age = fmt.Sprintf("%d", DaysBetween(l.FirstReceived, asOf))
		}
		row := []string{
			l.Key.Count, l.Key.Shade, l.Key.DyeLot, l.Key.Supplier, first, age,
			l.ReceivedG.KgString(), l.IssuedG.KgString(), l.AdjustedG.KgString(), l.BalanceG.KgString(),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// issueHeader is the column order of an issue-register export.
var issueHeader = []string{"date", "ref", "count", "shade", "dye_lot", "supplier", "qty_kg"}

// WriteIssueRegisterCSV writes the issue register: which dye-lot each
// production issue actually came off, which is the record a shade complaint is
// traced back through.
func WriteIssueRegisterCSV(w io.Writer, txns []Txn) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(issueHeader); err != nil {
		return err
	}
	for _, t := range txns {
		row := []string{
			FormatDate(t.Date), t.Ref, t.Key.Count, t.Key.Shade, t.Key.DyeLot,
			t.Key.Supplier, t.QtyG.KgString(),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
