# MCP router eval

## Purpose

`eval` answers one question for the MCP router surface from epic #8752: does
`find_tool` offer the right tool for a real question? It holds a corpus of
questions with the tool an agent should be offered, a harness that scores any
`router.Ranker` against that corpus, and a report-only CI gate. A ranking
mistake is invisible to an agent, because a tool that is never offered is a tool
the agent never learns exists. This package makes that mistake visible before the
router becomes a default.

## Ownership boundary

This package owns the corpus (`testdata/corpus.json`), its schema and floors, the
scoring harness, and the report format. It does not own the ranker (`rank`), the
curated intent phrases (`internal/ask/catalog`, #8754), or the `Ranker` contract
(`router`). It must not import `internal/mcp`;
`TestEvalDoesNotImportMCPRoot` enforces that for the harness. The registry-backed
test lives in the external `eval_test` package, which may import the mcp root.

## The corpus

`testdata/corpus.json` holds at least 150 questions, one JSON object each:

| Field | Meaning |
| --- | --- |
| `id` | Stable label such as `code-lookup-001`. |
| `family` | One of the 13 labels in `corpus.go`. The six epic families are required. |
| `question` | The text sent to the ranker as the intent. |
| `expected` | The registered tool that best answers it. |
| `alternates` | Other registered tools that also answer it acceptably. |
| `anchors` | Optional `repo_id`, `service_id`, `workload_id`, `environment`. |
| `source` | Where the wording came from: `description`, `sweep`, `starter_prompt`, or `hand_written`. |

The wording comes from tool descriptions (most questions), the public starter
prompts, the catalog sweep arguments, and hand-written questions for gaps. It is
written without reading the curated intent phrases in `internal/ask/catalog`. A
corpus built from those phrases would score the intents against themselves, so
top-1 would measure nothing.

Most questions paraphrase a tool description, and the lexical ranker indexes those
same descriptions. Part of its top-1 is therefore lexical self-match, and the
small families (`dead_code`, `security_posture`) are noisy. Read family numbers
as a trend, not a score. Per-source tallies in the report are a later step. Keep it that way: when you add a question, do not open the
`annotations_intents_*.go` files first.

Names in the corpus are generic (`orders-api`, `checkout-service`). No employer,
customer, or private repository name belongs here; the `no-private-identifiers`
gate checks the committed file.

## Exported surface

- `Default` and `Load` read a corpus. They reject unknown fields, trailing data,
  and an unsupported version.
- `Validate(corpus, registered, requirements)` checks the shape and that every
  expected tool and alternate is registered. It reports every problem at once.
- `DefaultRequirements` returns the floors: 150 questions, 40 distinct tools, and
  the six epic families.
- `Run(ctx, ranker, corpus, env)` asks the ranker each question with limit `TopK`
  (5) and returns a `Report`. A question is top-1 when the first candidate is the
  expected tool or an alternate, and top-5 when any of the first five is.
- `Format(w, report)` writes overall and per-family numbers and every top-1 miss.

## The gate

`mcp-router-eval` in `specs/ci-gates.d/gates-04.yaml` runs this package's tests.
Two things fail the build: a corpus that does not load or validate, and an
expected tool that is not registered. Accuracy never fails it. The report goes to
the job summary through `ESHU_ROUTER_EVAL_REPORT`, which `TestRoutingAccuracyReport`
writes when set.

Run it locally from `go/`:

```bash
go test ./internal/mcp/router/eval/ -count=1 -v
```

Blocking thresholds come later. After at least five recorded runs on `main`,
propose them in a comment on epic #8752 with the numbers.

## Telemetry

None. The package is offline measurement tooling; its report is the signal.
