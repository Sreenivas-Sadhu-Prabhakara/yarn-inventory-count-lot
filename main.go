// Command yarn-inventory-count-lot serves a local yarn store ledger for a
// weaving or knitting unit: stock held per count, shade and dye-lot, issues
// allocated across lots oldest-first, yarn count conversions, consumption
// variance, and reorder and aged-lot alerts.
//
// It is one binary with no external dependencies. It listens on the loopback
// interface, serves its own UI from inside the executable, and keeps everything
// in one JSON file on the operator's own machine. It makes no outbound network
// calls of any kind.
package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sreenivas-sadhu-prabhakara/yarn-inventory-count-lot/internal/yarn"
)

//go:embed web
var webFiles embed.FS

// maxUploadBytes caps a CSV import. A year of mill receipts is a few hundred
// kilobytes, so this is generous and still bounds memory.
const maxUploadBytes = 8 << 20

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "address to listen on (loopback only by default)")
	data := flag.String("data", "./yarn-inventory-count-lot-data.json", "path to the data file")
	flag.Parse()

	st, err := openStore(*data)
	if err != nil {
		log.Fatalf("opening the data file: %v", err)
	}

	ui, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatalf("reading the embedded UI: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", filesOnly(ui))
	st.routes(mux)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           noSniff(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("yarn store ledger listening on http://%s", *addr)
	log.Printf("data file: %s", st.path)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// filesOnly serves the embedded interface without directory listings. The
// standard file server indexes any directory that has no index.html, which
// would publish the asset list for no good reason; only "/" maps to a
// directory here, and that is answered with the page itself.
func filesOnly(ui fs.FS) http.Handler {
	files := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The root is left to the file server, which answers it with
		// index.html. Any other directory is refused.
		if name := strings.Trim(r.URL.Path, "/"); name != "" {
			if info, err := fs.Stat(ui, name); err != nil || info.IsDir() {
				http.NotFound(w, r)
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}

// noSniff stops a browser second-guessing the content types this tool serves.
func noSniff(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h.ServeHTTP(w, r)
	})
}

// --- store ------------------------------------------------------------------

// store holds the ledger and keeps the file on disk in step with it.
type store struct {
	mu   sync.Mutex
	path string
	book *yarn.Book
}

// openStore loads the data file, or starts an empty ledger if there is none.
func openStore(path string) (*store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	s := &store{path: abs, book: yarn.NewBook()}

	raw, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return s, nil
	}
	var b yarn.Book
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%s is not readable as a ledger: %w", abs, err)
	}
	b.Normalize()
	s.book = &b
	return s, nil
}

// read runs fn against the ledger without changing it.
func (s *store) read(fn func(b *yarn.Book)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.book)
}

// write runs fn against a copy of the ledger and keeps the result only if both
// fn and the save to disk succeed. Memory and file therefore never disagree: a
// failed write leaves the ledger exactly as it was.
func (s *store) write(fn func(b *yarn.Book) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	clone, err := cloneBook(s.book)
	if err != nil {
		return err
	}
	if err := fn(clone); err != nil {
		return err
	}
	if err := saveBook(s.path, clone); err != nil {
		return err
	}
	s.book = clone
	return nil
}

// cloneBook deep-copies the ledger through its own serialised form, so a half
// finished change can never be left behind in memory.
func cloneBook(b *yarn.Book) (*yarn.Book, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	var out yarn.Book
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.Normalize()
	return &out, nil
}

// saveBook writes the ledger atomically: a temp file in the same directory,
// flushed, then renamed over the target. A power cut mid-save leaves the
// previous file intact rather than a truncated one.
func saveBook(path string, b *yarn.Book) error {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".yarn-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// --- HTTP plumbing ----------------------------------------------------------

func (s *store) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/receive", s.handleReceive)
	mux.HandleFunc("/api/issue", s.handleIssue)
	mux.HandleFunc("/api/adjust", s.handleAdjust)
	mux.HandleFunc("/api/rules", s.handleRules)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/convert", s.handleConvert)
	mux.HandleFunc("/api/consumption", s.handleConsumption)
	mux.HandleFunc("/api/import/receipts", s.handleImport)
	mux.HandleFunc("/api/export/balances.csv", s.handleExportBalances)
	mux.HandleFunc("/api/export/issues.csv", s.handleExportIssues)
	mux.HandleFunc("/api/template/receipts.csv", s.handleTemplate)
}

// errBody is the shape of every error reply.
type errBody struct {
	Error string `json:"error"`
	// Shortfall is filled in when a movement was refused for want of stock, so
	// the UI can show the gap without re-parsing the message.
	Requested *qty `json:"requested,omitempty"`
	Available *qty `json:"available,omitempty"`
	Shortfall *qty `json:"shortfall,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writing a response: %v", err)
	}
}

// failWith maps a domain error onto a status code and a useful body.
func failWith(w http.ResponseWriter, err error) {
	body := errBody{Error: err.Error()}
	status := http.StatusBadRequest

	var se *yarn.ShortfallError
	if errors.As(err, &se) {
		status = http.StatusConflict
		req, avail, short := q(se.Requested), q(se.Available), q(se.Shortfall())
		body.Requested, body.Available, body.Shortfall = &req, &avail, &short
	}
	writeJSON(w, status, body)
}

func failText(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errBody{Error: msg})
}

// requireMethod rejects anything but the method a handler supports.
func requireMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	failText(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not allowed here")
	return false
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		failText(w, http.StatusBadRequest, "could not read the request: "+err.Error())
		return false
	}
	return true
}

// --- view types -------------------------------------------------------------

// qty carries a quantity both ways: grams for arithmetic and a fixed three
// decimal kilogram string for display, so the browser never has to format a
// weight and cannot drift from the ledger.
type qty struct {
	G  yarn.Grams `json:"g"`
	Kg string     `json:"kg"`
}

func q(g yarn.Grams) qty { return qty{G: g, Kg: g.KgString()} }

type keyView struct {
	Count    string `json:"count"`
	Shade    string `json:"shade"`
	DyeLot   string `json:"dyeLot"`
	Supplier string `json:"supplier"`
}

func kv(k yarn.YarnKey) keyView {
	return keyView{Count: k.Count, Shade: k.Shade, DyeLot: k.DyeLot, Supplier: k.Supplier}
}

type lotView struct {
	Key           keyView `json:"key"`
	Balance       qty     `json:"balance"`
	Received      qty     `json:"received"`
	Issued        qty     `json:"issued"`
	Adjusted      qty     `json:"adjusted"`
	FirstReceived string  `json:"firstReceived"`
	LastMovement  string  `json:"lastMovement"`
	AgeDays       *int    `json:"ageDays"`
	Aged          bool    `json:"aged"`
}

type groupView struct {
	Count    string `json:"count"`
	Shade    string `json:"shade"`
	Balance  qty    `json:"balance"`
	Lots     int    `json:"lots"`
	OpenLots int    `json:"openLots"`
}

type ruleView struct {
	Count     string `json:"count"`
	Shade     string `json:"shade"`
	Level     qty    `json:"level"`
	OrderUpTo qty    `json:"orderUpTo"`
	OnHand    qty    `json:"onHand"`
}

type txnView struct {
	Seq  int64   `json:"seq"`
	Type string  `json:"type"`
	Qty  qty     `json:"qty"`
	Date string  `json:"date"`
	Ref  string  `json:"ref"`
	Key  keyView `json:"key"`
}

type allocView struct {
	Key           keyView `json:"key"`
	Qty           qty     `json:"qty"`
	LotBalance    qty     `json:"lotBalance"`
	Remaining     qty     `json:"remaining"`
	FirstReceived string  `json:"firstReceived"`
	Exhausts      bool    `json:"exhausts"`
}

type reorderView struct {
	Count          string `json:"count"`
	Shade          string `json:"shade"`
	OnHand         qty    `json:"onHand"`
	Level          qty    `json:"level"`
	ShortBy        qty    `json:"shortBy"`
	OrderUpTo      qty    `json:"orderUpTo"`
	SuggestedOrder qty    `json:"suggestedOrder"`
	TargetDerived  bool   `json:"targetDerived"`
	OpenLots       int    `json:"openLots"`
}

type agedView struct {
	Key           keyView `json:"key"`
	Balance       qty     `json:"balance"`
	FirstReceived string  `json:"firstReceived"`
	AgeDays       int     `json:"ageDays"`
	ThresholdDays int     `json:"thresholdDays"`
}

type alertsView struct {
	AsOf          string        `json:"asOf"`
	AgedAfterDays int           `json:"agedAfterDays"`
	Reorder       []reorderView `json:"reorder"`
	AgedLots      []agedView    `json:"agedLots"`
}

type stateView struct {
	AsOf          string      `json:"asOf"`
	AgedAfterDays int         `json:"agedAfterDays"`
	Lots          []lotView   `json:"lots"`
	Groups        []groupView `json:"groups"`
	Rules         []ruleView  `json:"rules"`
	Register      []txnView   `json:"register"`
	Movements     int         `json:"movements"`
	TotalBalance  qty         `json:"totalBalance"`
	Alerts        alertsView  `json:"alerts"`
	Counts        []string    `json:"counts"`
	Shades        []string    `json:"shades"`
	Suppliers     []string    `json:"suppliers"`
	DataFile      string      `json:"dataFile"`
}

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return yarn.FormatDate(t)
}

func lotViews(lots []yarn.Lot, asOf time.Time, agedAfter int) []lotView {
	out := make([]lotView, 0, len(lots))
	for _, l := range lots {
		v := lotView{
			Key:           kv(l.Key),
			Balance:       q(l.BalanceG),
			Received:      q(l.ReceivedG),
			Issued:        q(l.IssuedG),
			Adjusted:      q(l.AdjustedG),
			FirstReceived: fmtDate(l.FirstReceived),
			LastMovement:  fmtDate(l.LastMovement),
		}
		if !l.FirstReceived.IsZero() {
			age := yarn.DaysBetween(l.FirstReceived, asOf)
			v.AgeDays = &age
			v.Aged = l.BalanceG > 0 && age > agedAfter
		}
		out = append(out, v)
	}
	return out
}

func alertViews(a yarn.Alerts) alertsView {
	out := alertsView{
		AsOf:          fmtDate(a.AsOf),
		AgedAfterDays: a.AgedAfterDays,
		Reorder:       []reorderView{},
		AgedLots:      []agedView{},
	}
	for _, r := range a.Reorder {
		out.Reorder = append(out.Reorder, reorderView{
			Count: r.Count, Shade: r.Shade,
			OnHand: q(r.OnHandG), Level: q(r.LevelG), ShortBy: q(r.ShortByG),
			OrderUpTo: q(r.OrderUpToG), SuggestedOrder: q(r.SuggestedOrderG),
			TargetDerived: r.TargetDerived, OpenLots: r.OpenLots,
		})
	}
	for _, l := range a.AgedLots {
		out.AgedLots = append(out.AgedLots, agedView{
			Key: kv(l.Key), Balance: q(l.BalanceG),
			FirstReceived: fmtDate(l.FirstReceived),
			AgeDays:       l.AgeDays, ThresholdDays: l.ThresholdDays,
		})
	}
	return out
}

// --- handlers ---------------------------------------------------------------

// asOfParam reads an "asOf" query parameter, defaulting to today.
func asOfParam(r *http.Request) (time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("asOf"))
	if raw == "" {
		return yarn.Day(time.Now()), nil
	}
	return yarn.ParseDate(raw)
}

func (s *store) handleState(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	asOf, err := asOfParam(r)
	if err != nil {
		failWith(w, err)
		return
	}

	var out stateView
	s.read(func(b *yarn.Book) {
		alerts := b.CurrentAlerts(asOf)
		lots := b.Lots()

		out = stateView{
			AsOf:          fmtDate(asOf),
			AgedAfterDays: alerts.AgedAfterDays,
			Lots:          lotViews(lots, asOf, alerts.AgedAfterDays),
			Groups:        []groupView{},
			Rules:         []ruleView{},
			Register:      []txnView{},
			Movements:     len(b.Txns),
			Alerts:        alertViews(alerts),
			DataFile:      s.path,
		}

		var total yarn.Grams
		counts, shades, suppliers := newSet(), newSet(), newSet()
		for _, l := range lots {
			total += l.BalanceG
			counts.add(l.Key.Count)
			shades.add(l.Key.Shade)
			suppliers.add(l.Key.Supplier)
		}
		out.TotalBalance = q(total)
		out.Counts, out.Shades, out.Suppliers = counts.sorted(), shades.sorted(), suppliers.sorted()

		for _, gr := range b.Groups() {
			out.Groups = append(out.Groups, groupView{
				Count: gr.Count, Shade: gr.Shade, Balance: q(gr.BalanceG),
				Lots: gr.Lots, OpenLots: gr.OpenLots,
			})
		}
		for _, ru := range b.Rules {
			target := ru.OrderUpToG
			if target == 0 {
				target = ru.LevelG * yarn.DefaultOrderUpToMultiple
			}
			out.Rules = append(out.Rules, ruleView{
				Count: ru.Count, Shade: ru.Shade,
				Level: q(ru.LevelG), OrderUpTo: q(target),
				OnHand: q(b.GroupBalance(ru.Count, ru.Shade)),
			})
		}
		for _, t := range b.Register() {
			out.Register = append(out.Register, txnView{
				Seq: t.Seq, Type: string(t.Type), Qty: q(t.QtyG),
				Date: fmtDate(t.Date), Ref: t.Ref, Key: kv(t.Key),
			})
		}
	})
	writeJSON(w, http.StatusOK, out)
}

type receiveReq struct {
	Count    string `json:"count"`
	Shade    string `json:"shade"`
	DyeLot   string `json:"dyeLot"`
	Supplier string `json:"supplier"`
	Kg       string `json:"kg"`
	Date     string `json:"date"`
	Ref      string `json:"ref"`
}

func (s *store) handleReceive(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req receiveReq
	if !decode(w, r, &req) {
		return
	}
	qtyG, err := yarn.ParseKg(req.Kg)
	if err != nil {
		failWith(w, err)
		return
	}
	date, err := yarn.ParseDate(req.Date)
	if err != nil {
		failWith(w, err)
		return
	}
	// The count is checked here as well as in the key, so an unreadable count
	// can never get into the ledger and poison every later conversion.
	if _, err := yarn.ParseCount(req.Count); err != nil {
		failWith(w, err)
		return
	}
	key := yarn.YarnKey{Count: req.Count, Shade: req.Shade, DyeLot: req.DyeLot, Supplier: req.Supplier}

	var posted yarn.Txn
	if err := s.write(func(b *yarn.Book) error {
		t, err := b.Receive(key, qtyG, date, req.Ref)
		posted = t
		return err
	}); err != nil {
		failWith(w, err)
		return
	}
	writeJSON(w, http.StatusOK, txnView{
		Seq: posted.Seq, Type: string(posted.Type), Qty: q(posted.QtyG),
		Date: fmtDate(posted.Date), Ref: posted.Ref, Key: kv(posted.Key),
	})
}

type issueReq struct {
	Count  string `json:"count"`
	Shade  string `json:"shade"`
	Kg     string `json:"kg"`
	Date   string `json:"date"`
	Ref    string `json:"ref"`
	Commit bool   `json:"commit"`
}

type issueResp struct {
	Allocations []allocView `json:"allocations"`
	Total       qty         `json:"total"`
	Lots        int         `json:"lots"`
	Committed   bool        `json:"committed"`
}

// handleIssue plans a FIFO issue and, only when asked to commit, records it.
// The two-step shape is deliberate: the operator sees exactly which dye-lots
// the yarn will come off, and how many shade joins that means, before anything
// is written.
func (s *store) handleIssue(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req issueReq
	if !decode(w, r, &req) {
		return
	}
	qtyG, err := yarn.ParseKg(req.Kg)
	if err != nil {
		failWith(w, err)
		return
	}
	var date time.Time
	if req.Commit {
		if date, err = yarn.ParseDate(req.Date); err != nil {
			failWith(w, err)
			return
		}
	}

	var plan []yarn.Allocation
	if req.Commit {
		err = s.write(func(b *yarn.Book) error {
			p, err := b.PostFIFOIssue(req.Count, req.Shade, qtyG, date, req.Ref)
			plan = p
			return err
		})
	} else {
		s.read(func(b *yarn.Book) {
			plan, err = b.IssueFIFO(req.Count, req.Shade, qtyG)
		})
	}
	if err != nil {
		failWith(w, err)
		return
	}

	out := issueResp{Allocations: []allocView{}, Total: q(yarn.AllocatedTotal(plan)), Lots: len(plan), Committed: req.Commit}
	for _, a := range plan {
		out.Allocations = append(out.Allocations, allocView{
			Key: kv(a.Key), Qty: q(a.QtyG), LotBalance: q(a.LotBalanceG),
			Remaining: q(a.RemainingG), FirstReceived: fmtDate(a.FirstReceived),
			Exhausts: a.Exhausts,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type adjustReq struct {
	Count    string `json:"count"`
	Shade    string `json:"shade"`
	DyeLot   string `json:"dyeLot"`
	Supplier string `json:"supplier"`
	Kg       string `json:"kg"`
	Date     string `json:"date"`
	Ref      string `json:"ref"`
}

func (s *store) handleAdjust(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req adjustReq
	if !decode(w, r, &req) {
		return
	}
	qtyG, err := yarn.ParseKg(req.Kg)
	if err != nil {
		failWith(w, err)
		return
	}
	date, err := yarn.ParseDate(req.Date)
	if err != nil {
		failWith(w, err)
		return
	}
	key := yarn.YarnKey{Count: req.Count, Shade: req.Shade, DyeLot: req.DyeLot, Supplier: req.Supplier}

	var posted yarn.Txn
	if err := s.write(func(b *yarn.Book) error {
		t, err := b.Adjust(key, qtyG, date, req.Ref)
		posted = t
		return err
	}); err != nil {
		failWith(w, err)
		return
	}
	writeJSON(w, http.StatusOK, txnView{
		Seq: posted.Seq, Type: string(posted.Type), Qty: q(posted.QtyG),
		Date: fmtDate(posted.Date), Ref: posted.Ref, Key: kv(posted.Key),
	})
}

type ruleReq struct {
	Count       string `json:"count"`
	Shade       string `json:"shade"`
	LevelKg     string `json:"levelKg"`
	OrderUpToKg string `json:"orderUpToKg"`
	Delete      bool   `json:"delete"`
}

func (s *store) handleRules(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req ruleReq
	if !decode(w, r, &req) {
		return
	}
	if req.Delete {
		var found bool
		if err := s.write(func(b *yarn.Book) error {
			found = b.DeleteRule(req.Count, req.Shade)
			return nil
		}); err != nil {
			failWith(w, err)
			return
		}
		if !found {
			failText(w, http.StatusNotFound, "no reorder level set for "+req.Count+" "+req.Shade)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}

	level, err := yarn.ParseKg(req.LevelKg)
	if err != nil {
		failWith(w, err)
		return
	}
	var target yarn.Grams
	if strings.TrimSpace(req.OrderUpToKg) != "" {
		if target, err = yarn.ParseKg(req.OrderUpToKg); err != nil {
			failWith(w, err)
			return
		}
	}
	rule := yarn.ReorderRule{Count: req.Count, Shade: req.Shade, LevelG: level, OrderUpToG: target}
	if err := s.write(func(b *yarn.Book) error { return b.SetRule(rule) }); err != nil {
		failWith(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

type settingsReq struct {
	AgedAfterDays int `json:"agedAfterDays"`
}

func (s *store) handleSettings(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req settingsReq
	if !decode(w, r, &req) {
		return
	}
	if req.AgedAfterDays < 1 || req.AgedAfterDays > 3650 {
		failText(w, http.StatusBadRequest, "the aged-lot threshold must be between 1 and 3650 days")
		return
	}
	if err := s.write(func(b *yarn.Book) error {
		b.AgedAfterDays = req.AgedAfterDays
		return nil
	}); err != nil {
		failWith(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsReq{AgedAfterDays: req.AgedAfterDays})
}

type convView struct {
	Input      string `json:"input"`
	System     string `json:"system"`
	Ne         string `json:"ne"`
	Nm         string `json:"nm"`
	Tex        string `json:"tex"`
	Denier     string `json:"denier"`
	Plies      int    `json:"plies"`
	Singles    string `json:"singles"`
	SinglesTex string `json:"singlesTex"`
	Folded     bool   `json:"folded"`
	Parsed     string `json:"parsed"`
}

// num formats a count for display at three decimals.
func num(v float64) string { return strconv.FormatFloat(yarn.RoundTo(v, 3), 'f', 3, 64) }

// handleConvert answers the count panel. It takes either a ply string
// ("2/60s") or a bare value plus a system ("29.525", "Ne").
func (s *store) handleConvert(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	qs := r.URL.Query()

	if raw := strings.TrimSpace(qs.Get("count")); raw != "" {
		cc, err := yarn.ConvertCount(raw)
		if err != nil {
			failWith(w, err)
			return
		}
		writeJSON(w, http.StatusOK, convView{
			Input: raw, System: "count", Parsed: cc.Count.String(),
			Ne: num(cc.Resultant.Ne), Nm: num(cc.Resultant.Nm),
			Tex: num(cc.Resultant.Tex), Denier: num(cc.Resultant.Denier),
			Plies: cc.Count.Plies, Singles: num(cc.Count.Singles),
			SinglesTex: num(cc.SinglesTex), Folded: cc.Count.Folded(),
		})
		return
	}

	sys, err := yarn.ParseSystem(qs.Get("system"))
	if err != nil {
		failWith(w, err)
		return
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(qs.Get("value")), 64)
	if err != nil {
		failText(w, http.StatusBadRequest, "the count value must be a number")
		return
	}
	c, err := yarn.Convert(value, sys)
	if err != nil {
		failWith(w, err)
		return
	}
	writeJSON(w, http.StatusOK, convView{
		Input: num(c.Input), System: string(c.System), Plies: 1,
		Ne: num(c.Ne), Nm: num(c.Nm), Tex: num(c.Tex), Denier: num(c.Denier),
	})
}

type consumptionReq struct {
	Count        string `json:"count"`
	Shade        string `json:"shade"`
	From         string `json:"from"`
	To           string `json:"to"`
	Units        string `json:"units"`
	GramsPerUnit string `json:"gramsPerUnit"`
}

type consumptionView struct {
	Count               string `json:"count"`
	Shade               string `json:"shade"`
	From                string `json:"from"`
	To                  string `json:"to"`
	ProductionUnits     string `json:"productionUnits"`
	GramsPerUnit        string `json:"gramsPerUnit"`
	Expected            qty    `json:"expected"`
	Actual              qty    `json:"actual"`
	Variance            qty    `json:"variance"`
	OverStandardPct     string `json:"overStandardPct"`
	OverStandardDefined bool   `json:"overStandardDefined"`
	WastagePct          string `json:"wastagePct"`
	WastageDefined      bool   `json:"wastageDefined"`
	Over                bool   `json:"over"`
}

func (s *store) handleConsumption(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var req consumptionReq
	if !decode(w, r, &req) {
		return
	}
	from, err := yarn.ParseDate(req.From)
	if err != nil {
		failWith(w, err)
		return
	}
	to, err := yarn.ParseDate(req.To)
	if err != nil {
		failWith(w, err)
		return
	}
	units, err := strconv.ParseFloat(strings.TrimSpace(req.Units), 64)
	if err != nil {
		failText(w, http.StatusBadRequest, "production units must be a number")
		return
	}
	gpu, err := strconv.ParseFloat(strings.TrimSpace(req.GramsPerUnit), 64)
	if err != nil {
		failText(w, http.StatusBadRequest, "grams per unit must be a number")
		return
	}

	var actual yarn.Grams
	s.read(func(b *yarn.Book) { actual = b.IssuedBetween(req.Count, req.Shade, from, to) })

	v, err := yarn.CompareConsumption(units, gpu, actual)
	if err != nil {
		failWith(w, err)
		return
	}
	writeJSON(w, http.StatusOK, consumptionView{
		Count: req.Count, Shade: req.Shade, From: fmtDate(from), To: fmtDate(to),
		ProductionUnits: num(v.ProductionUnits), GramsPerUnit: num(v.GramsPerUnit),
		Expected: q(v.ExpectedG), Actual: q(v.ActualG), Variance: q(v.VarianceG),
		OverStandardPct: num(v.OverStandardPct), OverStandardDefined: v.OverStandardDefined,
		WastagePct: num(v.WastagePct), WastageDefined: v.WastageDefined,
		Over: v.Over(),
	})
}

type importView struct {
	Rows       int             `json:"rows"`
	Imported   int             `json:"imported"`
	Unreadable []yarn.RowError `json:"unreadable"`
	Skipped    []yarn.RowError `json:"skipped"`
}

func (s *store) handleImport(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUploadBytes))
	if err != nil {
		failText(w, http.StatusBadRequest, "could not read the upload: "+err.Error())
		return
	}
	receipts, unreadable, err := yarn.ParseReceiptsCSV(strings.NewReader(string(body)))
	if err != nil {
		failWith(w, err)
		return
	}

	var rep yarn.ImportReport
	if err := s.write(func(b *yarn.Book) error {
		rep = b.ImportReceipts(receipts)
		return nil
	}); err != nil {
		failWith(w, err)
		return
	}
	if unreadable == nil {
		unreadable = []yarn.RowError{}
	}
	writeJSON(w, http.StatusOK, importView{
		Rows:       rep.Rows + len(unreadable),
		Imported:   rep.Imported,
		Unreadable: unreadable,
		Skipped:    rep.Skipped,
	})
}

// sendCSV sets the headers that make a browser save the file.
func sendCSV(w http.ResponseWriter, name string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
}

func (s *store) handleExportBalances(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	asOf, err := asOfParam(r)
	if err != nil {
		failWith(w, err)
		return
	}
	var lots []yarn.Lot
	s.read(func(b *yarn.Book) { lots = b.Lots() })

	sendCSV(w, "yarn-balances-"+yarn.FormatDate(asOf)+".csv")
	if err := yarn.WriteBalancesCSV(w, lots, asOf); err != nil {
		log.Printf("writing the balances export: %v", err)
	}
}

func (s *store) handleExportIssues(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	var reg []yarn.Txn
	s.read(func(b *yarn.Book) { reg = b.Register() })

	sendCSV(w, "yarn-issue-register-"+yarn.FormatDate(yarn.Day(time.Now()))+".csv")
	if err := yarn.WriteIssueRegisterCSV(w, reg); err != nil {
		log.Printf("writing the issue register export: %v", err)
	}
}

func (s *store) handleTemplate(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	sendCSV(w, "yarn-receipts-template.csv")
	if _, err := io.WriteString(w, yarn.ReceiptsCSVTemplate); err != nil {
		log.Printf("writing the import template: %v", err)
	}
}

// --- small helpers ----------------------------------------------------------

// set collects the distinct non-empty values used for the UI's pick lists.
type set map[string]bool

func newSet() set { return set{} }

func (s set) add(v string) {
	if v = strings.TrimSpace(v); v != "" {
		s[v] = true
	}
}

func (s set) sorted() []string {
	out := make([]string, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	sortStrings(out)
	return out
}

// sortStrings sorts case-insensitively so a pick list reads the way a person
// would write it.
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && strings.ToLower(ss[j]) < strings.ToLower(ss[j-1]); j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
