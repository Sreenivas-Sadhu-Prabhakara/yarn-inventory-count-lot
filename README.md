# Yarn inventory by count, lot and shade

A free, single-binary yarn store ledger for weaving and knitting units. Stock is
held **per dye-lot**, issues are allocated across lots oldest first, and yarn
counts convert between Ne, Nm, tex, denier and folded notations. It runs on your
own machine, serves its own web interface, and sends nothing anywhere.

## What it does

Yarn is not a fungible commodity, and most stock software treats it as though it
were. Two lots of one nominal shade come out of separate dye baths, and the
difference shows as a visible colour band across the finished fabric where the
warp or the feed changes over. A single pooled "30s Navy: 225 kg" balance hides
exactly the information a weaver needs before cutting a warp.

This tool keys stock on **count, shade, dye-lot and supplier** together — the
supplier included because two spinners happily reuse the same lot numbers — and
does the arithmetic a yarn store actually needs:

- **Balance per lot**: receipts, less issues, plus or minus adjustments. An
  issue or write-off that would drive a lot negative is refused outright, with
  the shortfall named in kilograms.
- **FIFO allocation on issue**: ask for 120 kg of 30s Navy and the tool spreads
  it across dye-lots oldest receipt first, shows the per-lot allocation and how
  many shade joins it implies, and writes nothing until you confirm. Either
  every allocation line posts or none does.
- **Count conversion**: Ne, Nm, tex and denier in both directions, plus folded
  counts — `2/60s` is read as a two-fold of 60s singles and resolves to a
  resultant Ne 30.
- **Consumption against standard**: what a run should have drawn against what
  was actually issued, as a variance and as a wastage percentage.
- **Reorder and aged-lot alerts**: what has fallen to or below its reorder level
  with a suggested order quantity, and which lots have sat long enough to be a
  shade-drift risk.
- **CSV in and out**: import receipts from your existing sheet (columns matched
  by header name, in any order), export balances by lot and the issue register.

## Who it's for

Small and medium weaving and knitting units, commission dyers, and yarn traders
in India and the Philippines — the shed with four to forty looms, or a circular
knitting floor, where the yarn store runs on a notebook or a spreadsheet and the
count conversions are done on a phone calculator. If a buyer has ever rejected a
piece for a shade band, this is built for that problem.

## The rule it implements

**No statute applies to this tool, and it does not pretend one does.** Yarn
stock control is not a regulated return; it is a physical and arithmetic
discipline. What it implements is:

**Dye-lot integrity.** Stock is never pooled above the dye-lot. Issues consume
one lot to exhaustion before opening the next, both because the oldest lot has
had the longest to drift in storage and because it minimises the shade joins in
a run.

**The count conversions.** Ne and Nm are indirect counts (length per unit mass:
a bigger number is finer). Tex and denier are direct (mass per unit length: a
bigger number is coarser). Converting between the families is a reciprocal, not
a multiplication. Tex is the hub, so there is one path between any two systems:

```
tex    = 1000 / Nm         Nm     = 1000 / tex
Ne     = 590.5 / tex       tex    = 590.5 / Ne
denier = 9 x tex           tex    = denier / 9
```

A folded count divides in Ne and multiplies in tex — the same statement twice —
so `2/60s`, `3/90s` and `30s` all resolve to the same resultant count.

### Constants used, and where they come from

Each is a single named constant in `internal/yarn`, and the two that are
judgements rather than definitions are user-overridable from the interface.

| Constant | Value | Source |
|---|---|---|
| `NmTexFactor` | 1000 | Exact by definition: Nm is m/g, tex is g/1000 m. |
| `DenierPerTex` | 9 | Exact by definition: denier is g/9000 m. |
| `NeTexFactor` | 590.5 | A rounding. Ne is 840-yard hanks per pound, so the factor is 453.59237 g ÷ (840 × 0.9144 m) × 1000 = **590.5412474…**. Mill practice rounds it to 590.5, which keeps every conversion within **0.007%** of exact. |
| `MaxPlies` | 24 | A sanity bound whose real job is catching the reversed notation: `60/2` means the same yarn as `2/60s` in some trades, and reading it plies-first would take a count from 30 to 0.033, so it is refused with an explanation. |
| `DefaultAgedLotDays` | 90 | **A working figure, not a standard.** Shade drift depends on fibre, dye class and godown conditions. Change it in the Alerts tab. |
| `DefaultOrderUpToMultiple` | 2 | The suggested order brings stock to twice the reorder level when no order-up-to level is set, since a trigger is not a target. Set an explicit figure per supplier lead time. |

If your buyer's contract specifies a different Ne factor, convert with theirs
before quoting. Confirm any figure that affects a commercial commitment against
the counterparty's own contract or test house, not against this tool.

Quantities are held internally as **whole grams**, not fractional kilograms.
That is what lets an exact-fit issue empty a lot to precisely zero, and stops a
long run of receipts and issues drifting by a fraction of a gram into a false
negative balance.

## Install & run

Go 1.25 or later is needed once, to build it. After that it is a single
executable you can copy to any machine of the same kind — no runtime, no
database, no installer.

```sh
git clone https://github.com/sreenivas-sadhu-prabhakara/yarn-inventory-count-lot
cd yarn-inventory-count-lot

# build the binary
go build -o yarn-inventory-count-lot .
./yarn-inventory-count-lot

# or just run it from source
go run .
```

Then open `http://127.0.0.1:8080` in a browser on the same machine.

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `127.0.0.1:8080` | Address to listen on. Loopback only by default. |
| `-data` | `./yarn-inventory-count-lot-data.json` | Where the ledger is kept. |

## Where data is stored

One JSON file on your own machine — `./yarn-inventory-count-lot-data.json` by
default, or wherever `-data` points. It holds your movements, your reorder
levels and your aged-lot threshold, and nothing else.

The file is written atomically: a temporary file alongside it is written,
flushed, and renamed over the target, so a power cut during a save leaves the
previous file intact rather than a truncated one. It is written with owner-only
permissions. Because it is plain JSON you can read it, diff it, keep it in
version control, or back it up by copying it.

The tool binds to `127.0.0.1`, so nothing outside your machine can reach it
unless you deliberately change `-addr`. It makes **no outbound network calls of
any kind**: no telemetry, no analytics, no update check, no fonts or scripts
from a CDN. The web interface is compiled into the executable, so the whole
thing runs on an air-gapped machine with nothing missing.

## Scope and accuracy

This is a stock and conversion tool, not an accounting package, an ERP, or a
costing system. It does not value your stock, post to a general ledger, handle
GST or VAT, manage purchase orders, or track work in progress. It has no
multi-user support and no access control: anyone who can use your machine can
use it.

The conversion constants are definitional and stated above. The 90-day aged-lot
default and the order-up-to multiple are working judgements, not standards, and
you should set both from your own fibre, storage and lead-time experience. The
variance figures are only as good as the grams-per-unit standard you type in.
Nothing here replaces sampling a shade against the lot you are about to run.

## Project layout

```
main.go              HTTP server, JSON API, atomic file persistence
internal/yarn/       the rules, as pure functions with table-driven tests
  grams.go           whole-gram quantities, parsing and formatting
  key.go             the composite stock key and its normalisation
  count.go           the six count conversions and ply-count parsing
  dates.go           calendar-day arithmetic
  book.go            the ledger: balances, lots, groups, the register
  fifo.go            oldest-first allocation across dye-lots
  consumption.go     standard requirement, variance and wastage
  alerts.go          reorder levels and aged-lot flags
  csv.go             receipts import, balances and register exports
web/                 the interface, embedded into the binary
docs/                the explainer site
```

Run the checks with:

```sh
gofmt -l .
go vet ./...
go test ./... -count=1
go build -o /tmp/yarn-inventory-count-lot .
```

MIT licensed.
