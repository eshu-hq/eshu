# Collector Claim Pacing And Per-Key Quotas

Status: design for issue #4588, for owner sign-off. No implementation. Nothing
here has been built; the measurements come from read-only probes and a throwaway
test harness that is not part of this branch.

Owners: workflow, coordinator, and storage maintainers.

## 1. Correcting the issue's premise

The issue proposes that "the coordinator paces `ClaimNextEligible` grants."
Two things are wrong with that, and the second one is the useful correction.

**The coordinator does not grant claims, and must not.**
`go/internal/coordinator/AGENTS.md` is explicit: the coordinator "must not claim
work on behalf of collectors. Claim ownership belongs to collectors." Collectors
self-claim with one SQL statement they issue themselves.

**But a pacing layer already exists — on the collector side.**
`collector.FairClaimDispatcher` (`go/internal/collector/fair_claim_dispatcher.go`)
is, in `go/internal/workflow/README.md:165`'s words, "the production
claim-dispatch boundary". It chooses the next target and then calls
`ClaimNextEligible` for it, leaving lease fencing, retry, and completion in the
existing lifecycle. It is wired in production at
`go/internal/collector/claimed_multi_source_host.go:148`.

So this issue is not "add pacing where there is none". It is **"the pacing that
exists stops one level too high."**

## 2. Where the existing fairness stops

`workflow.FamilyFairnessScheduler` (`go/internal/workflow/fairness.go`) does
deterministic smooth weighted round-robin across collector *families*, and plain
rotation across *instances* within the selected family. Its `Next()` returns a
`ClaimTarget{CollectorKind, CollectorInstanceID}` — nothing finer.

Below that, the claim statement is strictly FIFO. From
`go/internal/storage/postgres/workflow_control_sql.go:63`, carrying its own note
about this issue:

```go
// TODO(phase-2-fairness): This selector is intentionally FIFO within one
// collector family. Multi-family fairness must move into an explicit scheduler
// before this ORDER BY changes, otherwise family starvation can regress
// silently under the wrong claim model.
```

```sql
WHERE collector_kind = $1 AND collector_instance_id = $2 AND status = 'pending' ...
ORDER BY COALESCE(visible_at, created_at), created_at, work_item_id
LIMIT 1 FOR UPDATE SKIP LOCKED
```

The claim is partitioned by `collector_kind` **and** `collector_instance_id`, so
one family genuinely cannot starve another at claim time — the scheduler above
already handles that axis, and the query cannot even see another family's rows.

**The gap is inside a single (kind, instance): its work items are drained in
pure arrival order, with no notion of who they belong to.**

### The unit that is missing is already in the schema

`workflow_work_items` has a `fairness_key` column
(`migrations/014_workflow_control_plane.sql:28`). Nineteen scheduler files
populate it, each with a per-target key:

| Scheduler | Key shape |
| --- | --- |
| `aws_scheduled_scheduler.go:285` | `aws:<instance>:<account id>` |
| `oci_registry_scheduler.go:258` | `oci_registry:<instance>:<provider>` |
| `package_registry_scheduler.go:350` | `package_registry:<instance>:<class>:<ecosystem>` |
| `loki_scheduler.go:272` | `loki:<instance>:<scope>` |

`loki_scheduler.go:270` states the intent outright: "FairnessKey partitions
claims by the per-target Loki source."

Not for claiming, it does not — and the precise scope of that matters, because
two production paths *do* consult the key:

- `go/internal/collector/extensionhost/mapping.go:69-73` — `partitionKey()`
  returns `item.FairnessKey` when set, falling back to
  `SourceSystem + ":" + ScopeID`, and the result becomes
  `scope.IngestionScope.PartitionKey`.
- `go/internal/storage/postgres/status_registry.go:97,108` —
  `NULLIF(BTRIM(SPLIT_PART(fairness_key, ':', 4)), '') AS ecosystem`, grouped and
  filtered on, feeding `RegistryMetadataTargetCount` on an operator status
  surface.

**The second one is a live constraint on any change here.** It reads the
*fourth colon-delimited segment* of the key, which only means "ecosystem" for the
`package_registry:<instance>:<class>:<ecosystem>` shape. Restructuring the key,
or making other schedulers emit a different segment count, silently changes what
that status query reports. Nothing declares this coupling; it is positional.

What is genuinely absent is any use of the key **in claim ordering**, and any
appearance at all in `go/internal/telemetry/` (verified: zero hits).

That is this issue in one sentence: the family-level scheduler is real and
working, the per-target key is populated and consumed for partitioning and
status but never for claim ordering, and the starvation lives in that gap.

## 3. The measured problem

### 3.1 Harness

A throwaway Go test drove the real `WorkflowControlStore.ClaimNextEligible`
against a real Postgres 18.4 with the full migration set applied — the shipped
statement, the shipped indexes, no substitutions. Eight concurrent claim loops
drained the backlog, each recording the fairness key and elapsed time, then
completing the claim.

It deliberately targets **one** `(collector_kind, collector_instance_id)`, which
is exactly the layer `FairClaimDispatcher` hands off to and does not itself pace.

Two scenarios, same total work (3,100 items) and same worker count:

- **skew** — 3,000 items on one key, then 25 items each on four other keys
  created one second later.
- **even** — 620 items on each of five keys, all created together.

### 3.2 Result

| Scenario | Aggregate | Burst key, first claim | Other keys, first claim |
| --- | --- | ---: | ---: |
| skew | 3,100 items, 26.076s, 119 claims/s | 18 ms | **25.26 – 25.29 s** |
| even | 3,100 items, 25.509s, 122 claims/s | 35 ms | 20 – 35 ms |

The four small keys wait about **25.3 seconds** for a first claim in the skewed
run and about **30 milliseconds** in the even run — roughly a **thousand-fold**
difference for identical work.

The second column is what makes this worth fixing rather than tolerating.
Aggregate throughput barely moves: 119 claims/s skewed against 122 even, ~2%,
within the noise of ordering and machine load. The backlog drains just as fast
either way. Nothing is slower. Work simply arrives in an order that makes four
of five keys wait for the fifth to finish.

This is head-of-line blocking, and it is purely an ordering defect. It is not a
capacity problem, so adding workers does not help — eight workers all pull from
the head of the same FIFO.

### 3.3 What an operator would feel

The small keys stand for a per-account, per-provider, or per-target slice of one
collector family. Given the shipped key shapes, one AWS account with a large
backlog delays every other account on that collector instance until it drains.
The delay is proportional to the burst, so a 10× larger burst is a 10× longer
wait.

## 4. The claim query is also doing far more work than it needs to

Adjacent finding from the same measurement, and it changes the option analysis.

`EXPLAIN (ANALYZE)` on the shipped candidate select, shipped indexes, 50,100-row
pending backlog:

```
Seq Scan on workflow_work_items (actual rows=50100.00)
  Sort Method: quicksort  Memory: 4276kB
```

Every claim sequentially scans and fully sorts the whole eligible backlog. The
existing `workflow_work_items_claimable_idx` is
`(collector_kind, collector_instance_id, status, visible_at, updated_at DESC)`
while the query orders by `COALESCE(visible_at, created_at), created_at,
work_item_id`. The orders do not match, so the index cannot serve the sort.

Claim cost therefore **grows with backlog size**: 1.582 ms at 3,100 pending rows
against about 10.8 ms at 50,100. Two points on a curve do not establish the
exponent — the growth measured here is in fact sub-linear (16× the rows for ~7×
the cost) — but the plan is a full scan plus a full sort of the eligible set on
every claim, which is the wrong shape for a queue whichever way the constant
falls.

Five samples of each shape at 50,100 pending, sorted, on an otherwise quiet
machine:

| Claim shape | Samples (ms) | Median |
| --- | --- | ---: |
| A. Shipped query, shipped indexes | 10.773 / 10.778 / 10.832 / 11.068 / 11.079 | **10.832** |
| B. Same FIFO order, matching index added | 0.055 / 0.057 / 0.064 / 0.081 / 0.158 | **0.064** |
| C. Fair per-key claim, supporting index | 0.178 / 0.178 / 0.179 / 0.180 / 0.208 | **0.179** |

Adding an index matching the order the query already asks for makes the claim
about **169× cheaper** at this backlog, and flat in backlog size rather than
linear.

## 5. Options

All three extend the existing dispatcher one level down. None moves claim
ownership off the collector, and none introduces a coordinator broker.

### Option A — token bucket at claim time

Each fairness key gets a refill rate and a burst ceiling; a claim consults the
bucket for the candidate row's key and skips to the next key when it is empty.

**Fairness:** rate-proportional. **Starvation:** an empty bucket is skipped, not
blocked, so a key recovers on refill.

The problem is where the bucket lives. `FairClaimDispatcher` holds its scheduler
state in process behind a mutex, which is fine for round-robin because it is
self-correcting and cheap to lose. A token bucket is neither: in-process buckets
are per host, two hosts do not agree, and a restart silently resets the pacing.
Shared buckets mean bucket state in Postgres updated on every claim — a hot write
on the claim path, creating exactly the contention this issue wants to remove.

It also needs a configured rate per key, and the key space is dynamic: accounts
and providers appear without an operator setting anything.

**Assessment: reject.** Right mechanism for a known, static, rate-limited
resource; wrong one for a dynamic key space, and it buys fairness with a new
write conflict.

### Option B — extend round-robin to the fairness key, in the selector

Change the ORDER BY so the claim takes the oldest item *per fairness key* rather
than the oldest overall. Round-robin across keys falls out of choosing the oldest
among the per-key heads.

This is the same algorithm the scheduler already applies one level up, which is
its main appeal: one fairness model, applied at two levels, rather than two
different mechanisms to reason about.

Measured at 50,100 pending using a loose index scan over distinct keys plus a
lateral head per key — row C above, median **0.179 ms**:

| Claim shape | Median at 50,100 pending | vs shipped |
| --- | ---: | ---: |
| Shipped query, shipped indexes | 10.832 ms | — |
| Shipped FIFO order, matching index | 0.064 ms | 169× faster |
| **Fair per-key claim, supporting index** | **0.179 ms** | **61× faster** |

Fair claiming costs about 2.8× the cheapest possible unfair claim and is still
roughly **sixty times cheaper than what ships today**. The usual objection —
that fairness costs throughput — is not true here, by a margin wide enough that
sampling noise does not threaten it.

**Fairness:** equal share per key, degrading to FIFO when only one key has work.
**Starvation:** structurally impossible — every key with eligible work has its
head in the candidate set on every claim, so no key can be passed over
indefinitely. That is a property of the query shape, not of a tuning parameter,
which is what makes it worth preferring.

Cost scales with the number of *distinct keys*, not the backlog.

### Option C — weighted quota with borrowing

Per-key quotas over a window, with unused quota borrowable by keys that exceed
theirs when nothing else wants it.

This is Option B plus a weight and an accounting window — and unlike the token
bucket, the repo already has the vocabulary: `FairnessCandidate.Weight` and
`fairnessFamily.weight` exist today, so per-key weights would extend a concept
rather than invent one.

What is missing is a reason. No current key shape expresses a priority, and
nobody has asked to weight one account above another. Borrowing in particular is
machinery whose behaviour is hard to reason about and which nothing yet needs.

**Assessment: defer, but it is the natural successor to B.** Weights belong in
the ORDER BY once someone can say what they should be. Building the borrowing
machinery first means guessing at both the weights and the window.

## 6. The test that proves none of these is a serialization workaround

Serialization would satisfy a naive reading of this issue: cap each key's
in-flight claims, or drop the worker count, and the tail shortens. The repo
forbids that, and the check has to be a number in the PR, not an assurance in
its description.

**The implementation PR must show aggregate throughput at or above today's,
measured under the skewed backlog, at the same worker count.**

| Metric | Baseline (measured) | Requirement |
| --- | ---: | --- |
| Aggregate drain, 3,100 items, 8 workers, skewed | 119 claims/s | **≥ 119 claims/s** |
| Small-key time to first claim | 25.26 s | materially lower; the even-distribution 30 ms is the floor |
| Small-key time to first claim, even distribution | 20–35 ms | unchanged |
| Claim statement at 50,100 pending | 10.832 ms (median of 5) | **≤ 10.832 ms** |

The first row is the anti-serialization gate. A design that fixes the tail by
throttling the burst lowers it and fails; one that fixes the ordering holds it,
and section 5 says fair claiming should raise it substantially.

The last row matters because it is possible to improve the tail and make the
claim path worse at once; both must be reported. The even-distribution row is the
regression guard — a fairness change must not degrade the unskewed case, which is
the common one.

**Two honest limitations of this gate.**

First, throughput plus tail latency does not prove *work-conserving* behaviour: a
design could hold both numbers and still idle a worker while eligible work
exists, if it skipped a key without falling through to the next.
`FairClaimDispatcher.ClaimNext` already sets the precedent of skipping empty
lanes without sleeping. **The PR should therefore also show that no claim attempt
returns "nothing available" while any eligible row exists** — a direct counter,
not an inference from the timing rows.

Second, and more awkward: **none of these outcome measures distinguish the
recommended design from a well-tuned version of the one this document rejects.**
Option A is described above as skipping an empty bucket rather than blocking on
it, which makes it work-conserving too. A per-key rate limit with its ceiling set
near the measured aggregate could satisfy every row of the table. So the table
rules out a hard concurrency cap; it does not rule out rate limiting.

The check that does discriminate is structural, not measured — but it has to be
stated as the invariant itself, not as a proxy for it:

> **Eligible** means: the key has at least one row satisfying the claim query's
> own `WHERE` clause — `status = 'pending'`, `visible_at` null or past, the
> non-empty identity predicates — evaluated with no fairness, rotation, or
> pacing logic in the loop. It is a property of the rows, not of the scheduler.
>
> **1. No eligible key's head row may be excluded from the candidate set because
> of that key's own claim history or rate**, by any mechanism — a `WHERE` clause,
> a deferred `visible_at`, or a cooldown applied one layer up.
>
> **2. No key may enter or leave the eligible set because of its own claim
> history or rate.**
>
> **3. A key's position in the round-robin order may depend only on a declared
> static weight, or on the current set of eligible keys — never on that key's own
> claim history or rate.**
>
> **4. The round-robin unit is the literal `fairness_key` value the scheduler
> populated.** No two distinct values may be merged into one rotation unit, and
> no single value may be split into several, on the basis of a key's own claim
> history or rate.
>
> **5. The eligible-key set and any declared weight must be refreshed on a
> cadence independent of any key's own claim history or rate.** A key's own
> volume may not change how quickly its eligibility is discovered, nor how
> quickly its own weight is updated.

That is exactly the property section 5 claims makes starvation structurally
impossible under Option B, and it is false of any rate cap.

An earlier draft of this document used a weaker proxy — "must not read or write
per-key state outside the claim SQL and its supporting index" — which does not
hold. A per-key rate limit can be written as a window function over
`last_claimed_at` partitioned by `fairness_key`, needing no bucket table, no new
column, and no write outside the claim statement: all the state it reads already
lives in `workflow_work_items`. It would satisfy the proxy while still bumping a
steadily-claiming key behind newer arrivals whose rows are younger than its own
head. The invariant above rejects that; the proxy did not.

The definition is not padding, and neither is clause 2.

An earlier version stated only clauses 1 and 3 and left "eligible" undefined —
which mattered because both clauses lean on it. Under the narrow reading above it
is airtight; under a reading where "eligible" means "currently in rotation", a
design could gate entry to that set on a key's own cooldown and then claim
clause 3's "current set of eligible keys" carve-out as cover, having done its
throttling one level before either clause's forbidden terms apply. The same
mechanism read as compliant or forbidden depending on which meaning a reader
brought. Pinning "eligible" to the claim query's own predicate settles it, and
clause 2 closes the set-membership route explicitly rather than relying on the
definition to imply it.

Clause 3 is not padding either. An earlier version stopped after "only its
position in the round-robin order may defer it", which constrains what may
*exclude* a head row but says nothing about what position may be *computed from*.
A design that keeps every eligible key in the candidate set — satisfying the
letter — while recomputing a key's rotation position from its own recent claim
count reproduces the throttle exactly, and calls the mechanism "position", which
the sentence had explicitly blessed. That is deficit round robin: a real,
respectable algorithm an implementer would plausibly reach for, not a contrived
edge case. Constraining the position computation is what closes it.

Clause 4 exists because the first three all take "a key" as given and reason
about that unit's eligibility, membership and position — while saying nothing
about what the unit *is*. Leave every `fairness_key` value's head row eligible,
remove none from the set, compute position with no history term, and still
penalise a busy key by folding it into a shared "overflow" bucket with several
low-volume keys: the bucket takes one turn like any other unit, and the busy
key's effective share drops by roughly `1/M`. Every clause is satisfied, because
the history-dependence lives in how the grouping is *formed* — upstream of every
term the other clauses constrain. It is the same structural evasion as the
eligible-set shrink, one layer further out.

This is the loophole most likely to be walked into by accident rather than
design. Section 7 already worries about `fairness_key` cardinality for the metric
label, and section 8's question 2 asks how many distinct keys a large family
reaches, because the loose index scan's cost scales with it. An implementer
solving that cardinality problem would reach for bucketing for entirely
legitimate reasons — and bucketing by volume is indistinguishable in the diff
from bucketing as a throttle. Clause 4 makes the distinction explicit rather than
leaving it to intent. It also closes the mirror abuse, where a key inflates its
own share by triggering split logic.

Clause 5 covers *when*, where the first four cover *what*. Each of them
constrains the shape of an input at the moment it is consumed — is this row
eligible, is this key in the set, what does position depend on, what is the unit
— and none constrains the process that produces or refreshes that input.

Two designs slip through on that. Option B's cost scales with the number of
distinct keys rather than the backlog, which is a direct incentive to cache the
distinct-key discovery rather than redo it per claim; if that cache's refresh
interval is ever tuned by observed volume — an ordinary cache-efficiency move — a
busy key's newly-arrived rows stay undiscovered longer than a quiet key's. Every
earlier clause holds at the instant the scheduler looks: the rows are genuinely
eligible, they simply have not been noticed yet. Separately, "declared static
weight" in clause 3 is only required to be static *within* a claim; an
out-of-band job that rewrites those weights nightly from observed claim rates is
a slow rate limiter wearing "declared" as cover, and section 8's question 4
treats weights as a live concept, so this is not far-fetched.

**This is the sixth formulation of this check, and I am stopping here.** Each of
the previous five looked correct when written and each was evaded one layer
further out: state location, then exclusion, then position computation, then the
unit, now the refresh cadence. The honest claim is that **no evasion has been
found after six adversarial rounds** — not that none exists. An implementer who
finds a seventh should treat that as expected rather than surprising, and should
fix the invariant rather than assume their design is compliant because the
wording does not name it.

**Two seams are known open and deliberately not closed by a clause.**

The first was found immediately after the sixth round, which is the closing
statement above earning its keep rather than contradicting it: **the invariant
does not constrain the delay between selecting a key's turn and executing its
claim.** A design can compute a fair position, keep the set and weights fresh,
merge and exclude nothing — and still sleep before issuing the claim SQL for the
selected key, for a duration sized by that key's own recent rate. Every clause
holds at the moment the claim executes; the turn simply took longer to honour.
It is the same root cause as clause 5 — shape constrained, process not — applied
to turn execution instead of set refresh.

The second: **how many rows a turn may claim.** The statement is `LIMIT 1` today, so batch size is not a variable and a per-key
batch throttle is not expressible. If step 2's rewritten candidate CTE
ever claims N rows per turn, that changes — small batches for busy keys and large
ones for idle keys would satisfy all three clauses while behaving as a throttle.
Not a gap today; a thing to re-check if the claim shape moves off single-row.

Reviewers should apply this by reading the diff, not by reading a number.

## 7. Operator telemetry

An operator paged at 3 AM for "the AWS collector is behind" cannot tell whether
the family is saturated or one key is hogging it, for a specific and checkable
reason: **`fairness_key` appears nowhere in `go/internal/telemetry/`.** Not as a
metric label, not as a span attribute, not in the contract file.

What exists:

- `eshu_dp_workflow_family_queue_depth` — labeled `collector_kind`,
  `source_system`, `status` (`instruments.go:5163`). Too coarse: a burst key and
  the keys it starves usually share a `source_system`.
- `eshu_dp_workflow_claim_wait_seconds` — item age when a claim starts, buckets
  to 3600s (`instruments.go:3379`). This *would* show the 25 s tail, but with no
  key label an operator sees that something waited and cannot see what.

The implementation needs, at minimum:

1. **A `fairness_key` dimension on claim wait.** The starvation signal is already
   collected and merely unattributable. Highest value for the least work.
2. **Per-key queue depth**, as a gauge or bounded top-N — this is what separates
   "one account is enormous" from "everything is behind".
3. **A skip counter**, reason-labeled, if the design skips candidates, so pacing
   that is working looks different from pacing that is stuck. This is also the
   signal that backs the work-conserving check in section 6.

On cardinality, the real constraint: `fairness_key` embeds account and provider
identifiers, so it is unbounded in principle and belongs in spans and logs rather
than metric labels, per the repo's own rule. The workable shape is a bounded
top-N gauge plus the full key on the span. **Do not put a raw `fairness_key` on a
metric label.** The exact bound is worth deciding before code is written.

Whatever lands must be the full four-artifact telemetry contract — X1 doc, X2
verifier, X3 CI gate, X4 dashboard — not a metric alone.

## 8. Recommendation

**Option B, extending the existing fairness model down to `fairness_key`, in two
separately provable steps.**

**Step 1 — index the claim path (no behavior change).** Add an index matching the
current ORDER BY. Claims stop scanning the backlog: 10.832 ms → 0.064 ms median
at 50,100 pending, claim order byte-identical to today. Provable by exact
row-order equivalence plus the plan change. Worth landing alone: it is a real
defect, independent of fairness, carrying none of its risk.

**Step 2 — make `fairness_key` load-bearing in the ORDER BY.** Head-of-key
selection, supporting index, the section 7 telemetry, and the section 6 table
including the work-conserving counter.

Step 2 must also either preserve the key's colon-delimited segment layout or
update `status_registry.go:97,108` in the same change. That query extracts
`SPLIT_PART(fairness_key, ':', 4)` as an ecosystem label, so the key's shape is
already an undeclared contract with an operator-facing surface. A regression test
pinning that status output belongs in the same PR.

Splitting them keeps the diffs honest: step 1 changes performance and not
behaviour, step 2 changes behaviour. Bundled, neither is cleanly provable.

### Cost

| Item | Estimate |
| --- | --- |
| Step 1 | One migration, one index, an order-equivalence test, a plan assertion. Small. |
| Step 2 | Rewritten candidate CTE, second index, dispatch tests, live contention proof, telemetry with its four artifacts. Moderate. |
| Migration risk | Two `CREATE INDEX CONCURRENTLY` on a hot table. Build time on a large `workflow_work_items` is the operational risk and needs measuring on a realistic table first. |
| Rollback | Step 1 is a dropped index. Step 2 needs a flag or a revert, since claim order is observable in drain behaviour. |

### Open questions for the owner

1. **Is `fairness_key` the right unit?** The design takes it as the schedulers
   already build it — some per-account, some per-provider, some per-scope. If the
   intended unit differs, that decision precedes any code; it is the design.
2. **Cardinality bound for telemetry** (section 7). How many distinct keys does a
   large single-instance family reach in production? That sets both the top-N
   bound and the cost of the loose index scan.
3. **Does step 1 ship on its own?** I would recommend yes.
4. **Weights** (Option C). Deferred unless a weighting rule exists — but the
   `Weight` field already in `FairnessCandidate` suggests someone once intended
   one. Worth knowing whether that intent is still live.

### Measurement caveats

The section 3 drain probe ran at load average ~15.9 on 12 logical CPUs with other
work running, which inflates its absolute milliseconds. That does not touch its
conclusions, which rest on ratios measured within a single run under the same
conditions — the 25.26 s against 30 ms tail, and 119 against 122 claims/s.

The section 4 and 5 plan timings were re-measured at load average ~4.4 and are
reported as five samples each, because the first pass varied between 15.6 ms and
32.2 ms on consecutive executions of the same statement. The medians are the
numbers to quote; the plan shapes — Seq Scan plus sort against Index Scan — are
the durable finding and did not vary at all.

Environment: Apple M4 Pro, 12 logical CPUs, 64 GiB, PostgreSQL 18.4 (aarch64) in
Docker with no CPU or memory limit. Contributor-class hardware, not the reference
profile; no absolute target is claimed from it.

## 9. Sign-off request

This is a design for review, not a plan I intend to execute. Before any code is
written I need a decision on:

- **Option B over A and C**, and the two-step split.
- **Question 1**, the fairness unit — the load-bearing one.
- **Question 3**, whether the index fix ships independently.

I have written no production code for this issue and will not until the above is
settled. The probe harness is throwaway and is not in this branch.
