# Supply-Chain Impact File Evidence Fold (#9781)

## Defect

The reducer was killed for memory every 25 to 50 minutes after the
`56f6d19678` roll. A heap profile put 85% of in-use heap (6.65 GB of 7.8 GB)
under `FactStore.loadSupplyChainImpactPayloadPages`, decoding the full
`payload` of every parser `file` fact an npm batch pass resolves into
`map[string]any` and keeping all of them until the index builders ran.

Root-Cause Evidence: the `inuse_space` profile taken in the QA environment on
the rolled image names `loadSupplyChainImpactPayloadPages` →
`supplyChainImpactPayloadPage` → `encoding/json` `objectInterface` at 85.4% of
in-use heap, called from `ListActiveSupplyChainImpactFactsWithStats` under
`loadActiveSupplyChainImpactFactsUntilStable`. Five concurrent
`osv/npm/_batch` passes held about 123k decoded file envelopes at the burst,
which is about 140 KB of heap per envelope (inferred from timing; the profile
is not a peak capture). The pass reads those envelopes through exactly three
builders: `buildJSTSPackageReachabilityIndex`, `extractPythonReachabilityEvidence`
and `buildJVMReachabilityIndex`. The JVM builder rejects every JS/TS file (it
requires `lang` in java, kotlin, scala or a JVM extension), so only the first
two read what the loader returns.

The payload budget did not bound this. `maxSupplyChainImpactEvidenceEnvelopesPerIntent`
counts envelopes, and its note assumed 1.9 KB per envelope (measured on
scanner and OS-package facts). A JS/TS file envelope is about 70 times that.

## Phase 0: which fix shape

Decision: stream-fold the file evidence at the loader boundary. SQL key
projection alone does not bound memory.

### QA payload sizes (read-only, JS/TS and all-language samples)

| Measurement | Result |
|---|---|
| Active JS/TS file facts in the fleet | 38,749; 1.90 GiB raw JSON text; 823 MiB TOAST-compressed |
| All active file facts | 150,368; 4.26 GiB raw JSON text |
| JS/TS sample (2,000 rows), raw : projected | 1.93 : 1 (90.5 MB : 46.8 MB) |
| All-language sample (2,000 rows), raw : projected | 3.01 : 1 (65.7 MB : 21.8 MB) |
| Largest repository (2,800 JS/TS files) | 2.50 : 1 |
| `function_calls` share of the projection | about 94% |
| `function_calls_scip` bytes | 0 in both samples (weight unmeasured) |

One npm batch pass resolves 20k to 54k file rows, which is most of the 38.7k
JS/TS fleet. So a pass touches about 1.9 GiB of raw JSON. Projecting away the
keys the readers never use halves that at best, and `function_calls` (which the
readers do use) is what remains.

### Benchmark shim (throwaway, not committed)

A synthetic JS/TS file payload shaped like the parser's rows (imports,
function_calls, function_calls_scip, functions, variables, classes, 70 empty
buckets). The "calls-heavy" shape is calibrated so the decoded map costs 141.2
KB per row, the figure the QA profile implies; raw JSON is 30.3 KB per row.
Variants, all producing the same JS/TS index:

- **A**: today. Decode each row into `map[string]any`, keep every envelope,
  then `buildJSTSPackageReachabilityIndex`.
- **B**: SQL key projection. Decode the projected payload, keep every envelope.
- **C**: stream fold. Decode each 500-row page, fold it, drop the envelopes.
- **D**: C with a typed struct decode of only the keys read (lower bound for a
  later decode optimisation; not the shipped design).

Live heap (`runtime.GC` twice, `ReadMemStats.HeapAlloc` minus baseline), 20,000
rows. Per-row cost was 141,296 B at 5,000 rows and 141,205 B at 20,000, so it
is linear and 5x and 10x are computed, not run:

| Variant | Live per row | 1x (20k) | 5x (100k) | 10x (200k) |
|---|---:|---:|---:|---:|
| A today | 141.2 KB | 2.82 GB | 14.1 GB | 28.2 GB |
| B projection kept | 103.0 KB | 2.06 GB | 10.3 GB | 20.6 GB |
| C fold | index only | 0.5 MB + 82 MB page | same | same |

Allocation cost (`go test -bench -benchmem -benchtime 1x`, one pass of N rows;
ns/op is not reported because the host ran at load average 57 to 71):

| Variant | 1x B/op | 1x allocs | 5x B/op | 5x allocs | 10x B/op | 10x allocs |
|---|---:|---:|---:|---:|---:|---:|
| A | 3.44 GB | 107.5M | 17.20 GB | 537.5M | not run | not run |
| B | 2.32 GB | 82.9M | 11.62 GB | 414.6M | not run | not run |
| C | 3.46 GB | 107.6M | 17.29 GB | 537.9M | 34.58 GB | 1,075.8M |
| D | 0.40 GB | 7.7M | 2.00 GB | 38.4M | 4.01 GB | 76.8M |

Reading:

1. **Projection does not fix the OOM.** With `function_calls` at 94% of the
   projection, B keeps 73% of A's live bytes. Eight concurrent passes at 2.06
   GB each are still 16.5 GB before any other allocation, and a single pass at
   10x is 20.6 GB.
2. **Fold bounds live memory by the index.** C retains 0.5 MB plus one page of
   transient decode, the same at 1x, 5x and 10x.
3. **Fold does not cut allocation churn.** C pays the same full decode as A.
   D shows the decode itself is 8.6x fewer bytes and 14x fewer allocations when
   typed; that is the follow-up if GC time on these passes ever matters. It is
   not shipped here because it would re-implement the readers' key semantics
   in a second place, which is the parity risk this change avoids.

### Parity risk by shape

| Shape | Parity risk |
|---|---|
| SQL projection | Must keep every key three readers use, including Python's `decorators` under `functions[]` and `classes[]`. A missed key silently drops reachability evidence. |
| Fold at the loader (shipped) | The fold runs the readers' own per-file bodies, so no key semantics are copied. Remaining risks are order (first usage of the best priority wins; Python lists keep fact order) and dedupe (`appendUniqueSupplyChainImpactFacts` must still drop a file a later stage re-loads). |

The Python reader scans every `file` envelope in the pass, including the JS/TS
ones the impact loader returns. JS/TS import and call rows carry
`lang=javascript|typescript`, which the Python collectors skip, so only
decorators can carry JS/TS-sourced Python evidence. The fold keeps that path by
running the Python per-file body as well.

## Phase 1: the shipped change

The loader folds parser `file` facts into the reachability index as it scans
them. `FactStore.ListActiveSupplyChainImpactFactsWithStats` splits the resolved
IDs: when the filter carries a `FileReachabilityFold`, file-kind IDs go to
`PayloadPageOrderedSQL` (the primary-key page plus
`ORDER BY convert_to(fact.fact_id, 'UTF8')`), are scanned one row at a time in
the byte order Go sorted them, and each decoded payload is dropped after
`fold.Add`. The handler seeds the fold from the facts the pass already holds,
passes it on every active-evidence round, and appends one in-memory digest
envelope (`reducer_file_reachability_digest`) after the loop's files and before
the Python-loaded files. The JS/TS and Python builders merge the digest at that
position, so first-wins tie-breaks and Python list order match the old layout.
`appendUniqueSupplyChainImpactFacts` treats the digest's fact IDs as seen, so a
later stage cannot re-add a folded file.

A loader with no fold (a fake, or a caller that sets none) returns file
envelopes exactly as before. The standalone Python reachability stage
(`loadPythonReachabilityEvidenceFacts`, which calls `LoadFactsForKindAndPayloadValue`)
and the JVM loader are unchanged. The Python stage still loads all-language
`file` facts whole and charges neither limit, so only the Python evidence that
JS/TS-language rows in the active-evidence file branches carry is folded. The
Python stage is the next memory risk (#9860).

Python and JVM readers: the fold runs the Python per-file body as well, because
the Python reader scans every `file` envelope in the pass and JS/TS rows can
carry decorator evidence. JVM rejects JS/TS files by language, so folded files
could never reach it.

### Intended deltas

1. Folded files no longer count toward the 100,000-envelope budget. A pass that
   went partial only because of file count now completes and retracts. This is
   the point of the change: the count was never a memory bound for files.
2. A retained-payload byte budget (256 MiB raw, about 0.8 to 1.3 GiB decoded)
   backs the count. It is new for every kind the loader returns (core, identity
   and suppression pages), not only files. Exceeding it sets
   `truncation.budget`, the existing `evidence_budget` cause, so a pass the
   count budget used to finish can now go partial. Folded bytes are not
   charged. See "Disclosed behavior change".
3. Findings and reachability verdicts are identical for the same facts.
   `TestFileReachabilityFoldMatchesEnvelopeBuilders` compares the full JS/TS
   index and Python evidence over 60 seeded corpora (layout: base files, folded
   files, later Python-loaded files) with `reflect.DeepEqual`.
   `TestSupplyChainImpactHandlerFoldsActiveEvidenceFilesWithoutChangingFindings`
   compares written findings through the handler.

### Before and after, on the production loader path

`BenchmarkSupplyChainImpactFileEvidenceLoad` drives
`ListActiveSupplyChainImpactFactsWithStats` through the routing fake with
parser-shaped JS/TS payloads (calls-heavy shape, about 30 KB raw). "before" is
a load with no fold; "after" has one. `live_heap_bytes` is heap still reachable
after the load returns with the loaded envelopes and the fold held, which is
what a pass carries until its index builders run. Same host, same payload, one
pass per cell, `-benchtime=1x`. The host ran at load average about 60, so
`ns/op` is not evidence and is not reported.

Performance Evidence: live heap held by a pass's file evidence fell from 2.58 GB
to 5.0 MB at 20,000 files (the fleet's QA JS/TS file count is 38,749). The
before figure is linear at 128.8 KB per file; the after figure is the fold's
fact-ID set plus the index, about 250 B per file.

| Files in the pass | Scale | before live heap | after live heap | before B/op | after B/op | before allocs/op | after allocs/op |
|---:|---|---:|---:|---:|---:|---:|---:|
| 2,000 | 1x | 257.6 MB | 0.56 MB | 311 MB | 345 MB | 9.83 M | 11.17 M |
| 10,000 | 5x | 1,288 MB | 2.43 MB | 1,470 MB | 1,638 MB | 48.56 M | 55.25 M |
| 20,000 | 10x | 2,576 MB | 5.03 MB | 2,919 MB | 3,258 MB | 96.97 M | 110.35 M |

Reading:

- Live heap is constant per file in both modes. A QA pass of 54,000 files is
  about 7.0 GB before and about 14 MB after, and eight concurrent passes were
  about 55 GB of file payload before.
- Allocation churn rises about 11 to 12% (B/op) and 14% (allocs). The fold
  decodes each row exactly as before and then runs the two per-file bodies, and
  the ordered page scans one row at a time. The garbage is short-lived, so the
  cost is GC work, not heap. Phase 0 variant D (a typed decode of only the keys
  the readers use) cut decode bytes 8.6x and is the follow-up if GC time on
  these passes ever matters. It is not shipped here because it would copy the
  readers' key semantics into a second place.

### Eight concurrent passes, measured

`BenchmarkSupplyChainImpactFileEvidenceConcurrent` runs eight loads at once (the
reducer's worker count, each with its own routing fake) and samples
`runtime.GC` plus `HeapAlloc` every 250 ms while they run. `peak_live_heap_bytes`
is the highest value sampled, minus the baseline before the passes started.

| Workers x files per pass | before peak live heap | after peak live heap |
|---|---:|---:|
| 8 x 2,000 (1x) | 1.74 GB | 53 MB |
| 8 x 20,000 (10x) | 20.8 GB | 195 MB |

The 8 x 20,000 row is not part of the default benchmark run. Its before arm holds
eight decoded 20,000-file passes at once, about 20.8 GB, which is more than a
16 GB CI runner has: the benchmark job was killed with exit 143 while it started.
`BenchmarkSupplyChainImpactFileEvidenceConcurrent` now skips both 20,000-row arms
unless `ESHU_BENCH_HEAVY=1` is set; run it with that variable on a host with 32 GB
or more free (`go test -run '^$' -bench SupplyChainImpactFileEvidenceConcurrent
-benchtime=1x ./internal/storage/postgres/`). CI runs the 8 x 2,000 row.

The after figure is eight index digests, eight fact-ID sets (about 250 B per
file), one row being decoded per worker, and the fake's page buffers. It grows
with file count only through the fact-ID set, which is why 10x files cost
195 MB across all eight workers. The before figure is the value the QA
profile shows: the QA diagnosis put the whole pass, including non-file evidence
and Python files, at 4 to 7.6 GB at 1x and 40 to 76 GB at 10x; the file
evidence alone, 20 to 54 thousand rows, is 2.6 to 7.0 GB per pass here at
128.8 KB per file.

### What `cloneAuthorizedImpactEnvelopes` adds

The authorized loader deep-copies every returned envelope
(`facts.Envelope.Clone`) before the writer sees it. `BenchmarkSupplyChainImpactFileEvidenceClone`
holds the loaded envelopes and the clone together:

| Files in the pass | before, load only | before, load + clone | after, load only | after, load + clone |
|---:|---:|---:|---:|---:|
| 2,000 | 257.6 MB | 420.3 MB | 0.56 MB | 0.57 MB |
| 10,000 | 1,288 MB | 2,101 MB | 2.43 MB | 2.43 MB |

The clone adds 63% on top of the loaded files (it copies the maps and slices and
shares the strings), so a pass that held 7.0 GB of file payloads briefly held
about 11.4 GB. After the fold the result holds one digest envelope, and
`Envelope.Clone` copies only maps and slices, so the digest's pointer is shared
and the clone adds nothing that scales with files.

### The ordered payload-page statement

Root-Cause Evidence for the SQL choice: with a byte-ordered page the loader
folds each row as scanned and holds none, where an unordered page forces the
loader to hold the page (500 rows, about 70 MB at 140 KB each) to sort it in Go.
`EXPLAIN (ANALYZE, BUFFERS)` on a private native PostgreSQL 18.6 cluster, a
synthetic `fact_records` table with the production primary key and the 16
loaded columns (20,000 rows, 203 MB), generic plan forced
(`plan_cache_mode = force_generic_plan`), 500 random IDs:

| Statement | Plan | Shared buffers hit | Execution |
|---|---|---:|---:|
| `PayloadPageSQL` | Bitmap Heap Scan over `fact_records_pkey` | 852 | 0.83 ms |
| `PayloadPageOrderedSQL` | the same scan under one Sort (quicksort, 126 kB) | 852 | 1.06 ms |

The ordering adds one 126 kB in-memory sort over the 500 matched rows and no
extra buffer reads. `convert_to(fact_id, 'UTF8')` orders by bytes regardless of
database collation, so it matches Go's `bytes.Compare`; 17 ids with mixed case,
`_`, `-`, `:`, spaces, accented and four-byte characters sorted identically in
PostgreSQL and in Go. The loader still fails the call if a page comes back out
of order or short, so a collation or snapshot surprise cannot change evidence
silently.

Not measured: the production `fact_records` table has more indexes and the QA
data distribution; the plan depends on the primary key only. No Postgres
schema, index, or lock change ships.

### Consumers of per-file fact IDs (#9855)

The fold replaces the parser file envelopes with one digest envelope. Any code
that reads the pass's envelope list for file fact IDs sees the digest's synthetic
ID instead. The first version missed one: on the protection-engaged path the
loader built `StaticNonAdvisoryEvidenceFactIDs` from the envelope FactIDs, so the
protected writer rejected every fold-sourced JS/TS or Python reachability
finding as having unclassified evidence, and the pass retried forever. A digest
now contributes its folded file IDs through `core.AppendEvidenceFactIDs`, used in
both loops of `supply_chain_impact_authorized_loader.go` (disengaged and engaged).
Each loop has its own pin. The engaged loop is the one the protected writer
reads, and `TestAuthorizedLoaderEngagedFoldPublishesFoldedEvidenceLive` (a
live-postgres-readiness test) drives it for real: role policy installed, the
repeatable-read snapshot, the snapshot store folding JS/TS files, and the real
protected writer. It publishes with the expanded list and still refuses the
pre-fix list, an unknown ID, the digest's synthetic ID, and a tombstoned file
ID. Reverting only the engaged loop fails that test and nothing else.
`TestAuthorizedLoaderListsFoldedFilesAsStaticEvidence` drives the disengaged loop
through the real loader method (the handler discards that list, so it pins the
helper wiring only). `TestProtectedStaticClassificationAcceptsFoldedReachabilityEvidence`
runs the handler and the writer's classifier over a folded pass with a mirror
loader. A full `Handle` on the engaged branch with receipt-backed public
advisories is NOT_CHECKED.

| Consumer | Reads | Verdict |
|---|---|---|
| Authorized loader, disengaged and engaged loops | envelope FactIDs into `StaticNonAdvisoryEvidenceFactIDs` | Fixed: expands the digest |
| `writer_protected.go`, `writer_protected_static.go` | that ID list against each finding's `EvidenceFactIDs` | Fine once the list holds the file IDs (tests above) |
| JS/TS and Python reachability builders | digest fold, winning file ID per package | Fine: same IDs as before, parity property test |
| `appendUniqueSupplyChainImpactFacts` | envelope FactIDs for dedupe | Fine: digest-aware, adds the fold's seen IDs |
| `BuildVulnerabilitySuppressions`, anchor probe, derived-fact scope check | filter by kind or scope | Fine: the digest matches no kind and has no scope |
| `findingHasAnyScanFact`, `findingAdvisoryDependencies` | scan package and advisory envelopes | Fine: never files |
| JVM reachability | files from the separate JVM loader | Fine: JS/TS files fail its language gate, and the fold only folds JS/TS files |
| Persisted findings, query, explain, remediation | stored `evidence_fact_ids` | Fine: the IDs are the same file IDs as before the fold |
| `evidence_envelopes` sub-signal and truncation WARN field | `len(envelopes)` | Fixed: counts each folded file as one envelope (`loadedEnvelopeCount`) |

### Concurrency and the byte budget

No lock, queue, lease, or claim code changed. The fold is created per
`loadActiveSupplyChainImpactFactsUntilStable` call, is touched by that call's
goroutine only, and holds no shared state. The payload reads still run inside the
existing read-only repeatable-read snapshot. Concurrency was not reduced: the
change lowers per-pass memory so the existing concurrent passes fit.

### Observability

Observability Evidence: `eshu_dp_supply_chain_impact_active_evidence_payload_bytes`
(histogram, unit `By`, label `branch` in `payload_page`, `identity`,
`file_fold`) records the raw payload bytes each payload-reading statement read.
The branch-seconds and branch-rows instruments gain the `file_fold` label. The
`reducer execution succeeded` line for `supply_chain_impact` adds
`active_evidence_files_folded`, `active_evidence_payload_bytes_folded`,
`active_evidence_payload_bytes_retained`, and
`sub_signal_evidence_payload_bytes_ratio`. At 3 AM an operator seeing a pass go
partial reads the ratio and the retained-bytes total on the success line; a
climbing `payload_page` tail with a flat `file_fold` tail names retained
non-file payloads, not files. See
[Supply-Chain Active Evidence Signals](../../public/reference/telemetry/supply-chain-active-evidence.md).

### Tests that pin the change

- Core: fold parity property (60 seeds), dedupe against the pass and across
  rounds, non-file and tombstone filtering, handler findings parity, fallback
  for a loader that cannot fold, byte-budget cases.
- Store: files folded and not returned, byte order and bound IDs, paging past
  500 files, out-of-order and short pages rejected, no-fold path unchanged,
  payload-bytes histogram emission.
- Mutations run: making `bytesExhausted` return false fails the byte-budget
  test; dropping the fact-ID comparison in the ordered page check fails the
  out-of-order test. (An earlier mutation of only `charge` passed because
  `exhausted()` also reads the byte test, so the mutation target is
  `bytesExhausted`.) Later rounds added: reverting only the engaged authorized
  loop, listing the digest ID, an unsorted expansion, the digest prepended in
  `withDigest`, a spent budget read as zero (unlimited), the loader's spent check
  at `>=`, and the handler's at `>=` each fail a test. The two boundary mutants
  fail `TestListActiveSupplyChainImpactFactsReadsEveryPageAtTheExactByteLimit`
  and the exact-limit case of
  `TestSupplyChainImpactEvidenceBudgetChargesRetainedPayloadBytes`.

### The byte budget is charged per page, during the load

The handler hands each active-evidence call the bytes left in the pass's
payload budget (`SupplyChainImpactFactFilter.PayloadBytesLimit`). The store
checks it before every payload page and identity page: the page that crosses
the limit is kept, no later page is read, and the pass is partial under
`evidence_budget`. So the first round, which carries the file branches and the
heaviest statements, is bounded as it loads, not after it returns. The
QA diagnosis showed the old count budget was charged after the load, so it never
bounded round one.
`TestListActiveSupplyChainImpactFactsStopsPayloadPagesAtTheByteBudget` pins the
stop (one page read where the unlimited run reads three or more, and no identity
or suppression page after it); `TestSupplyChainImpactHandlerBoundsEachLoadByTheRemainingByteBudget`
pins the handler hand-off.

Folded file rows are not charged because the fold keeps none of them. That
leaves one page-sized transient: the fold scans a 500-ID page one row at a time,
so the decoded payload alive at once is a single row (about 130 KB at the
calls-heavy shape), not 500 rows (about 70 MB). The raw bytes of one page sit in
the driver's read buffer, which is bounded by the page size and does not grow
with the number of pages.

### Disclosed behavior change

Two changes move the partial boundary, in opposite directions.

Fewer partial passes for file-heavy scopes. Before, folded `file` envelopes
counted against the 100,000-envelope budget and could mark a pass partial.
After, they do not count, so a scope that was partial only because file rows
pushed it over the cap is now complete and retracts.
`TestSupplyChainImpactFoldedFilesStopCountingAgainstTheEnvelopeBudget` pins it:
the same facts with an envelope budget of 1 are partial with file envelopes and
complete with the fold, and the verdict fields (status, confidence, runtime
reachability, priority score, evidence fact IDs, repository) match. The only
difference in the finding is the partial-pass note `active supply-chain evidence
expansion limit reached` in its missing evidence.

More partial passes for retained-payload-heavy scopes. The byte budget is new for
every kind the loader keeps: core payload pages, identity pages and suppression
pages. The loader now stops mid-call, at a byte-order prefix of the core pages,
and skips the identity and suppression pages once the budget is spent. The old
count budget only stopped at round boundaries. So a scope that finished under the
count budget can now go partial if the payloads it keeps exceed 256 MiB raw.
That is a stricter bound, not a looser one, and it is why this note no longer
claims "never fewer verdicts".

A partial pass is fail-visible. `truncation.budget` is set, suppressions are
dropped (`suppressionEvidenceTruncated`), findings carry the partial-pass note,
and nothing is retracted. On the protection-engaged authorized path a partial
pass returns `authorizedImpactCoverageIncompleteError`, which is retryable and
writes nothing, so the queue retries it rather than publishing a truncated set.

Margin against 256 MiB for the non-file payloads. No direct measurement exists:
the retained-bytes signal ships with this change. The estimate has two sources.
The 100,000-envelope count budget still stops non-file loading at round
boundaries, and the scanner and OS-package envelopes grew the heap by about
1.9 KB each on average (101 MB over 55,002 envelopes of one kind mix; see
[7154](7154-capped-scope-convergence.md)). The new budget counts raw bytes, and
decoded heap runs several times raw (256 MiB raw is 0.8 to 1.3 GiB decoded), so
reading that average as raw bytes per envelope overstates it. A pass at the count
cap then keeps under 190 MB raw, under 71% of 256 MiB. An average is not a
bound for one kind: a `vulnerability.cve` envelope can be larger than 1.9 KB, so
a pass heavy in such kinds could use more of the budget. Observed passes are far
smaller: the QA step-5 readings show at most 30,897 evidence envelopes in one
pass, most of them file rows, which is at most 59 MB (22% of 256 MiB) at the
average even if every one were non-file. The count cap's crossing round is kept,
so the cap-bound figure can run over by one round. INFERRED. After the roll,
`sub_signal_active_evidence_payload_bytes_retained` replaces the estimate with a
measurement; see "After the roll". The retained figure counts every row a load
returns, including rows the pass later drops as duplicates, so it can read above
what the pass keeps. That errs toward an earlier, visible partial pass.

Accepted and unreachable today, found in review:

- A folded file's scope never reaches the derived-scope guards, because the
  digest has an empty scope ID. A parser `file` fact in an `image://derived/`
  scope used to fail the pass and would now be folded. No producer writes parser
  files into derived scopes (only scanner kinds get them), and the protected
  snapshot SQL excludes derived scopes.
- The engaged loader skips trusted public advisories by envelope fact ID, but
  expanded file IDs are not checked against that set. A file ID equal to a
  public advisory evidence ID would now fail with conflicting classes where it
  was skipped before. That needs a fact ID collision across kinds.

### Relation to #9778 and the diagnosis

#9778 (page `supply_chain_impact` evidence by repository) names this heap. Two
of its premises do not survive the QA profile: that the defect was not visible at
1x (the profile shows 85% of in-use heap in the payload page at the current
load), and that the ceiling is about 190 MB per pass (that figure is the
scanner and OS-package envelope; a file envelope is about 130 to 140 KB). This
change fixes the file-evidence share and leaves #9778's repository paging open
for the rest. Its SQL projection list lacks the Python `decorators` under
`functions[]` and `classes[]`; the fold needs no projection list because it runs
the readers' own per-file bodies, so there is no key set to keep in sync.

### A 54,000-file single pass under the byte budget (INFERRED)

Inferred, not run: the figures are the measured 251.6 B kept per folded file at
20,000 files extended linearly, and the fleet average of about 52 KB raw per
JS/TS file (38,749 active files, 2.04 GB raw JSON text in the QA environment).

- With the fold, the files are folded and not charged, so the pass reads about
  2.8 GB of raw payload through the loader, keeps about 14 MB for the fold, and
  is not partial for bytes. Only retained non-file payloads count against the
  256 MiB budget.
- Without the fold, the same pass would keep about 2.8 GB raw (about 7.0 GB
  decoded at 128.8 KB per file). The byte budget would stop it after roughly
  5,000 files (256 MiB over 52 KB) and mark it partial under `evidence_budget`.
  So the byte budget is a backstop for retained payloads; the fold is what makes
  a whole-fleet JS/TS pass fit.

### Out of scope, flagged

- The Python loader (`LoadFactsForKindAndPayloadValue`) still retains full
  all-language payloads per PyPI-evidenced repository. QA's largest such
  repository has 23,329 file facts (977 MB raw JSON). It is the next memory
  risk after this change: #9860.
- A typed decode of only the keys the readers use (Phase 0 variant D) would cut
  decode allocation 8.6x. It would copy the readers' key semantics into a second
  place, so it is not shipped: #9861.
- Later rounds re-read and re-decode files already folded in an earlier round
  and drop them in the fold's `seen` check. Skipping IDs the fold has seen
  would cut that decode work; it is not needed for the memory bound.
- QA validation of the rolled image is NOT_CHECKED; it needs the reviewed
  commit rolled.

### NOT_CHECKED

- Live heap above 20,000 files per pass (54,000-file passes are extrapolated
  from the linear per-file figure; 1x, 5x and 10x of a 2,000-file base were run
  one pass at a time, and 1x and 10x with eight concurrent passes).
- A live-Postgres run of the full loader on the production schema (the EXPLAIN
  used a synthetic table).
- QA end-to-end memory after the roll.

### After the roll: what QA should show

With the fold, one pass's transient while loading files is one decoded row
(about 130 KB) plus the page's raw bytes in the driver buffer (a 500-row page of
30 KB rows is about 15 MB), and what a pass keeps is the index plus about 250 B
per folded file. Eight workers should sit well under 1 GB of file-evidence heap
where the profile showed 6.65 GB attributed to the payload page. Greps for a
reviewer after the deploy:

- `eshu_dp_supply_chain_impact_active_evidence_payload_bytes{branch="file_fold"}`
  climbing while the container's working set stays flat.
- The `reducer execution succeeded` line for `supply_chain_impact`:
  `sub_signal_active_evidence_files_folded` large,
  `sub_signal_active_evidence_payload_bytes_retained` small (megabytes, not
  gigabytes), `sub_signal_evidence_payload_bytes_ratio` far under 1.
- `eshu_dp_supply_chain_impact_partial_scopes{cause="evidence_budget"}` should
  fall for scopes that were partial only because of file count.
- A heap profile (`inuse_space`) with `supplyChainImpactPayloadPage`
  and `objectInterface` no longer above a few percent.
