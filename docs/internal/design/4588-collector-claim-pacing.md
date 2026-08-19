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
or changing the package_registry key's segment count, silently changes what
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

Change the ORDER BY so the claim rotates across fairness keys instead of draining
the global-oldest first.

**An earlier revision of this section said "round-robin falls out of choosing the
oldest among the per-key heads." That is wrong, and it matters.** Choosing the
oldest per-key head is age-based selection, not rotation. Under the section 3
skew workload the 3,000 burst rows are created at `t=0` and the four small keys'
rows at `t=1s`, so the burst key's head *is* the global-oldest on every claim; it
drains first and the small keys still wait ~25 s. That reproduces the exact
defect this design exists to fix.

The first replacement I proposed for it was **also wrong, and measurably so.**
That attempt was `ORDER BY row_number() OVER (PARTITION BY fairness_key ORDER BY
COALESCE(visible_at, created_at)), …`, on the reasoning that per-key rank gives
every key a head before any key gets a second item.

It does not, and the reason is that the claim statement re-evaluates the whole
expression on every call over whatever is *currently* `pending`
(`go/internal/storage/postgres/workflow_control_sql.go:64-77`). Let `r*` be the
globally-oldest pending row. Its age is minimal over *all* pending rows, so it is
certainly minimal within its own partition — meaning `r*` is **rank 1 in its own
key on every single evaluation**. Ordering by `(rank, age, id)` therefore always
returns `r*`, which is exactly what plain FIFO returns. The window function
cannot discriminate, because claimed rows vacate their rank slot rather than
leaving a gap.

Measured, rather than argued, on PostgreSQL 16.15 with the section 3 skew shape —
3,000 rows on one key at `t=0`, 25 each on four keys at `t=1s`, claiming 40 times
and marking each claimed:

| Ordering | Claims to burst key | Claims to the four small keys |
| --- | ---: | ---: |
| `row_number()` recomputed per claim | **40** | **0** |
| Fixed per-key sequence, assigned at enqueue | 8 | 8 / 8 / 8 / 8 |

The first row is the defect this design exists to fix, reproduced exactly by its
own proposed cure.

**What does work is a rank that does not move.** Give each row a per-key sequence
number at enqueue and order by it: because the position is fixed rather than
recomputed, a claimed row's slot does *not* collapse, so the next claim at that
rank level goes to a different key. The measured pick order rotates —
`burst → small2 → small3 → small4 → small1 → burst → …`.

**And it has a failure of its own, measured.** My 8/8/8/8/8 result above gave
every key a sequence starting at 1 — equal lifetime volume. Production will not.
Sequence numbers are assigned per key and zeroed independently, so two keys are
only comparable if they have processed similar cumulative volume over their
entire history. An established key that has handled a million items carries
current sequence numbers around 1,000,001; a newly onboarded key starts at 1. If
both stay non-empty, the new key's rows compare smaller **forever**, purely as an
artifact of lifetime volume rather than arrival time.

Measured on the same harness — 50 pending rows each, established key at sequence
1,000,001+ with *older* timestamps, new key at sequence 1 with newer ones:

| Scenario | Established key | Newly-added key |
| --- | ---: | ---: |
| Fixed per-key sequence, unequal lifetime volume | **0 of 40** | **40 of 40** |

That is indefinite starvation again, inverted — the long-running key gets nothing
— and it needs no adversarial timing. It falls out of comparing independently
zeroed counters whose growth rates differ, which any deployment with a mix of
established and new sources has on day one.

A fix would need the comparison to be relative rather than absolute: rebasing
counters per epoch, or comparing turns taken within a window. Note that the
second of those is explicitly a claim-history quantity, which **sharpens rather
than resolves** the clause 3 question below.

**Three things must be settled before this is a recommendation rather than a
candidate.** First, whether a persisted per-key sequence is compatible with
clause 3 in section 6. It is assigned at enqueue from arrival order, before any
claim exists, and it never excludes an eligible key's head — but as a key drains,
its remaining rows carry higher sequence numbers, so the *effect* correlates with
how much that key has already claimed. Whether that counts as depending on "the
key's own claim history" is a real question about the invariant's intent, not a
rhetorical one, and it belongs to whoever builds this. Second, the cost: it adds
a column, an assignment at enqueue, and an index, none of which this document has
priced.


**The numbers below measure formulation #1 — the one this section opens by
calling wrong.** "A loose index scan over distinct keys plus a lateral head per
key" computes each key's current head; turning that into one winner still needs a
final tiebreak across those heads, and that tiebreak was age. They are kept
because they establish the **SQL-cost floor a real mechanism has to beat**, not
because they measure a live candidate. Read them that way:

| Claim shape | Median at 50,100 pending | vs shipped |
| --- | ---: | ---: |
| Shipped query, shipped indexes | 10.832 ms | — |
| Shipped FIFO order, matching index | 0.064 ms | 169× faster |
| **Fair per-key claim, supporting index** | **0.179 ms** | **61× faster** |

That row lands at 2.8× the cheapest *unfair* claim, and in hindsight the
closeness is a tell rather than a triumph: a query that always returns the row
FIFO would have returned, computed through a slightly pricier plan, has no reason
to cost much more. A mechanism doing real cross-key bookkeeping would not land
that near the unfair baseline.

What the row still supports is narrower and worth keeping: **claim cost at this
scale is dominated by the missing index, not by fairness.** Even the pricier plan
is roughly sixty times cheaper than what ships today. So "fairness costs
throughput" is not the objection to worry about here — but that is a statement
about the floor, not evidence that any fair mechanism has been built.

**Fairness:** equal share per key, degrading to FIFO when only one key has work.
**Starvation:** the original claim here was "structurally impossible — a property
of the query shape." **That claim is withdrawn.** Having every key's head in the
candidate set is necessary but not sufficient: the tiebreak still decides, and if
the tiebreak is age then the burst key wins every time, which is exactly what
formulations #1 and #2 were measured doing. Presence in the candidate set was
never the property that prevents starvation.

**What is measured and what is not.** The 0.179 ms above was measured for a loose
index scan over distinct keys with a lateral head per key — **not** for either
ordering discussed above, and it must not be carried over to them. A window
function in the outer `ORDER BY` generally forces the eligible backlog to be
materialised and sorted before it can be ranked, which is the Seq-Scan-plus-sort
shape section 4 measured at 10.8 ms, not 0.179 ms. Whatever mechanism is finally
chosen needs its own `EXPLAIN ANALYZE`. The figure is *not* time-to-first-claim
under skew either, and this design does not measure the tail latency of any
proposed ordering — the 25.26 s
figure in section 3 is the shipped FIFO behaviour. **Implementation must report
small-key time-to-first-claim for the rank shape under the section 3 skew
workload before the ORDER BY change is accepted.** Without it, the claim that
this fixes the measured starvation is an argument, not a result.

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
| Small-key time to first claim, single priority class | 25.26 s | materially lower; the even-distribution 30 ms is the floor |
| Small-key time to first claim, mixed priority classes | not yet measured | **required before acceptance** — priority is ordered before rotation, so a lower-priority key can still starve behind a higher-priority burst |
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
  `source_system`, `status` (`instruments.go:5171`). Too coarse: a burst key and
  the keys it starves usually share a `source_system`.
- `eshu_dp_workflow_claim_wait_seconds` — item age when a claim starts, buckets
  to 3600s (`instruments.go:3387`). This *would* show the 25 s tail, but with no
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

> **Superseded in part by [section 10](#10-owner-decision-2026-08-19).** The
> mechanism below stands. What does not: the two-step split, the step numbering
> (a normalisation step is inserted as the new step 2), the cost table's step
> boundary, the rollback row, and the open either/or about the `SPLIT_PART`
> parse. The owner selected a three-step plan in one PR. Read the split argument
> here as the case that was put, not the plan of record — and do not reconstruct
> an implementation order from this section alone.

**Option B, extending the existing fairness model down to `fairness_key`.** This
section recommended two separately *shipped* steps; per section 10 it is three
steps in one PR, each keeping its own commit and its own proof.

**Step 1 — index the claim path (no behavior change).** The shipped ordering is
`ORDER BY COALESCE(visible_at, created_at), created_at, work_item_id`
(`go/internal/storage/postgres/workflow_control_sql.go:63`), so a plain column
index cannot serve it — this needs an **expression index on
`COALESCE(visible_at, created_at)`**. And the visibility predicate
`(visible_at IS NULL OR visible_at <= $3)` is an `OR`, which cannot become a
range scan on that expression, so it stays a **residual filter**: the index
serves the sort, not the filter. The `LIMIT 1` early stop is what keeps this flat
in backlog size, and that holds for the all-eligible backlog measured here — it
can degrade once future-`visible_at` rows interleave with eligible ones. Both
facts belong in the implementation, so nobody reaches for a plain column index
and is surprised. Claims stop scanning the backlog: 10.832 ms → 0.064 ms median
at 50,100 pending, claim order byte-identical to today. Provable by exact
row-order equivalence plus the plan change. Worth landing alone: it is a real
defect, independent of fairness, carrying none of its risk.

**Step 2 — normalise the key.** *(Added by [section 10](#10-owner-decision-2026-08-19);
this section was written without it.)* Three-segment key, target-class moved to a
real priority column, a real ecosystem column, and whatever "`PartitionKey`
decoupled" turns out to mean (see section 10 — unresolved). **This must land
before the ORDER BY change**, because it is what keeps
the priority contract intact — without it, putting fairness ahead of `created_at`
reverses the `package_registry`/`vulnerability_intelligence` ordering the owner
named as a constraint. Section 10 carries the detail.

**Step 3 — make `fairness_key` load-bearing in the ORDER BY.** **The ordering
for this step is not settled.** Two candidate expressions have been written into
this document and both were wrong: age-among-per-key-heads is FIFO, and a
recomputed `row_number()` is a measured no-op (section 5). A fixed per-key
sequence assigned at enqueue does rotate, measured, but carries an unpriced
schema cost and an unresolved question about clause 3. **Do not implement this
step until an ordering is chosen and measured for both rotation and plan cost.**
Head-of-key
selection, supporting index, the section 7 telemetry, and the section 6 table
including the work-conserving counter.

*(Resolved by section 10: the normalisation retires the parse, so this is the
"update" branch, not the "preserve" branch. Left below as written, since the
reasoning for why it is a contract at all still holds.)*

The ORDER BY step must also either preserve the key's colon-delimited segment
layout or update `status_registry.go:97,108` in the same change. That query extracts
`SPLIT_PART(fairness_key, ':', 4)` as an ecosystem label, so the key's shape is
already an undeclared contract with an operator-facing surface. A regression test
pinning that status output belongs in the same PR.

Separate commits keep the diffs honest: step 1 changes performance and not
behaviour, steps 2 and 3 change behaviour. Section 10 takes the argument below
about shipping them apart and records why the owner overrode it.

### Cost

| Item | Estimate |
| --- | --- |
| Step 1 | One migration, one index, an order-equivalence test, a plan assertion. Small. |
| Step 2 (normalisation, per section 10) | Key-shape change in the **2** schedulers that carry a target-class segment (`package_registry`, `vulnerability_intelligence`), a priority column, an ecosystem column, and a pin on the `SPLIT_PART` status query. **Not in this section's original estimate.** |
| Step 3 (was step 2) | Rewritten candidate CTE, second index, dispatch tests, live contention proof, telemetry with its four artifacts. Moderate. |
| Migration risk | Two `CREATE INDEX CONCURRENTLY` on a hot table. Build time on a large `workflow_work_items` is the operational risk and needs measuring on a realistic table first. |
| Rollback | ~~Step 1 is a dropped index. Step 2 needs a flag or a revert.~~ **Superseded (section 10):** one PR means one revert, so the steps must be kept separately revertible deliberately if that property is wanted. |

### Open questions for the owner

1. **Is `fairness_key` the right unit?** *(Section 10: the selected option
   normalises it to a 3-segment key, which changes the rotation granularity, not
   only the encoding — but whether that coarsening was intended is itself
   unconfirmed. See "The fairness unit changes meaning".)* The design takes it as the
   schedulers already build it — some per-account, some per-provider, some per-scope. If the
   intended unit differs, that decision precedes any code; it is the design.
2. **Cardinality bound for telemetry** (section 7). How many distinct keys does a
   large single-instance family reach in production? That sets both the top-N
   bound and the cost of the loose index scan.
3. **Does step 1 ship on its own?** *(Answered in section 10: no.)* I would
   recommend yes.
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

## 8a. Status of the recommendation

**Option B's mechanism is unresolved, and the recommendation is weaker than the
rest of this document.**

What survives unchanged: the measured starvation in section 3, the claim-path
index defect in section 4, the anti-serialization test in section 6, the
telemetry in section 7, and the whole of section 10's decision record. Step 1 —
indexing the claim path — is still a real, provable, standalone improvement.

What does not survive: the specific ORDER BY that makes fairness load-bearing.
Three formulations have now been proposed here. The first was age-based selection
described as rotation. The second, a recomputed `row_number()`, is mathematically
identical to FIFO and was measured claiming 40 of 40 items from the burst key.
The third, a fixed per-key sequence, rotates when every key starts from zero —
but was then measured **starving an established key 40 to 0** against a newly
onboarded one, because independently zeroed counters are not comparable across
keys with different lifetime volumes. It also changes the schema and raises a
genuine question about the invariant in section 6.

I am recording this rather than quietly substituting a fourth candidate, because
the pattern is now consistent across all three: a plausible ordering expression,
an argument for why it rotates, and no measurement of the case that breaks it.
The third is the sharpest lesson — it *was* measured, and it rotated, because the
scenario I measured happened to be the one where it works. A measurement of the
favourable case is not evidence.

The next proposal should arrive with its rotation measured under **unequal
lifetime volume and unequal arrival rates**, and its query plan explained, not
with a better argument.

## 9. Sign-off request

This is a design for review, not a plan I intend to execute. Before any code is
written I need a decision on:

- **Option B over A and C**, and the two-step split.
- **Question 1**, the fairness unit — the load-bearing one.
- **Question 3**, whether the index fix ships independently.

I have written no production code for this issue and will not until the above is
settled. The probe harness is throwaway and is not in this branch.

## 10. Owner decision, 2026-08-19

The decision was made by selecting one of four options I offered. The exact text
of the selected option, verbatim, because everything below is an interpretation
of it and the interpretation should be checkable against the source:

> **Full fair claiming now** — Do all three steps including the ORDER BY change.
> Fixes the measured 25.26s starvation, but must not regress the
> package_registry/vulnerability_intelligence priority ordering — which needs the
> normalisation anyway, so this is option 2 plus more risk in one PR.

### What "option 2" was

The owner's reply refers to "option 2" without naming it. It was the second of
the four options offered, labelled *"Index fix, then normalise `fairness_key`"*:

> Index first, then a second PR: 3-segment key, target-class moved out to a real
> priority column, real ecosystem column retiring the `SPLIT_PART` parse,
> `PartitionKey` decoupled. Sets up fair claiming as priority-then-round-robin
> later. More work, unblocks the real fix.

Everything below about normalisation comes from this text.

### This is a three-step plan, and section 8 originally described two

Section 8 as first written recommended **two** steps: index the claim path, then
make `fairness_key` load-bearing in the ORDER BY. It has since been updated to
show three — see its supersession blockquote and the Step 2 annotation. The
selected option says *three*,
because the option list carried a middle step section 8 does not have — the one
offered as "index fix, then normalise `fairness_key`":

> 3-segment key, target-class moved out to a real priority column, real ecosystem
> column retiring the `SPLIT_PART` parse, `PartitionKey` decoupled.

I did not notice this when first recording the decision, and the omission
mattered. **It reverses section 10's earlier claim about the fairness unit.** An
earlier revision of this section said the decision accepted `fairness_key` as the
19 schedulers already build it. It does the opposite: the selected option
normalises the key to three segments and retires the positional
`SPLIT_PART(fairness_key, ':', 4)` parse in favour of a real ecosystem column.
The undeclared shape contract at `status_registry.go:97,108` is not something the
implementation must preserve — it is something the decision explicitly removes.

### What the option settles, and how directly

| Question | Settled by | Directness |
| --- | --- | --- |
| Option B over A and C | "all three steps including the ORDER BY change", plus the **absence** of any weight, quota or borrowing language | **Inference.** Option C is Option B plus a weight and an accounting window (section 5), so it also changes the ORDER BY — the phrase does not exclude it. What excludes C is what the reply does not say. |
| One PR, not two | "in one PR", stated | Verbatim |
| Accepting the bundled risk | "option 2 plus more risk in one PR" | Verbatim, and named as risk |
| The fairness unit | The normalisation step redefines it, and the segment arithmetic says how | **Direct, and it changes the rotation granularity — not just the encoding.** See below. |
| Priority contract preserved | "must not regress the package_registry/vulnerability_intelligence priority ordering" | Verbatim, and it is a constraint, not a preference |

Most of this is carried by the option text rather than inferred from the label.
Two rows are not: the choice of B over C rests on an absence, and the fairness
unit is discussed below. Both are marked.

### The fairness unit changes meaning, not just encoding

This deserves more than a table row, because "normalise the key" could mean a
tidier representation with the same rotation behaviour, and here it does not.

The key today is **four** segments. Both schedulers build it the same way:

```go
// package_registry_scheduler.go:350
fmt.Sprintf("%s:%s:%s:%s", scope.CollectorPackageRegistry, instance.InstanceID,
    packageRegistryTargetClass(target), target.Ecosystem)
// vulnerability_intelligence_scheduler.go:303 — same shape, source in place of ecosystem
```

The option asks for a **three**-segment key with "target-class moved out to a real
priority column". Four minus one is three, and the segment named for removal is
the class. So `<collector>:<instance>:<class>:<ecosystem>` becomes
`<collector>:<instance>:<ecosystem>`, and **two work items that are in different
fairness groups today — a `configured_direct` target and a `broad` target for the
same instance and ecosystem — land in the same rotation slot afterwards.** That is
a change to what the unit *is*, not to how it is written down.

It also explains the rest of the sentence. `SPLIT_PART(fairness_key, ':', 4)`
(`status_registry.go:97,108`) reads the fourth segment, which is the ecosystem.
Drop the class and there is no fourth segment at all. Postgres `SPLIT_PART`
returns an empty string for a part that does not exist, the query wraps it in
`NULLIF(BTRIM(...), '')`, and its `WHERE` clause requires that to be
`IS NOT NULL` — so every `package_registry` work row fails the predicate.

Being precise about what that costs, because this sentence has now been wrong
twice in opposite directions: the **planned/completed/stale/failed/rate-limited
counts silently collapse to zero**, not a mislabelled ecosystem. The row set is
not necessarily empty, because the query ends in
`FROM work_counts FULL OUTER JOIN warning_counts`
(`go/internal/storage/postgres/status_registry.go:143-145`) and `warning_counts`
reads `fact_records` (`:111-117`), which `fairness_key` does not touch. So
warning-derived ecosystem rows still appear with every work count at zero; absent
warning facts, the result is empty. The regression test should pin the collapsed
counts, not an empty result set.

Note also that this surface is **narrower than an earlier revision implied**. The
query filters `WHERE collector_kind = 'package_registry'`, so only that key
reaches the extraction — `vulnerability_intelligence` emits a 4-segment key too
but never reaches this query. That is why the option pairs the removal with "real
ecosystem column retiring the `SPLIT_PART` parse" rather than leaving the parse
to be re-indexed, and why the regression test pinning that status output is not
optional.

And "sets up fair claiming as priority-then-round-robin later" names the ordering
model: an explicit priority column ordered first, then round-robin across
fairness keys. That is how the priority contract survives — it stops being
microseconds of spacing inside `created_at` and becomes a column.

**That ordering also relocates the starvation rather than eliminating it, and the
design must say so.** Priority ordered first means head-of-line blocking *across*
priority tiers: if the 3,000-item burst key is `configured_direct` and the four
small keys are `broad`, the priority column drains the burst first and the small
keys wait exactly as long as they do today. The rotation only rotates *within* a
priority class.

This is not an argument against the model — cross-tier precedence is the owner's
stated constraint, and a priority column that could be starved by a lower tier
would not be a priority column. It is an argument against how section 6 states
its result. "Small-key time to first claim: materially lower" is unscoped and
therefore overclaims; it holds **within a priority class** and nowhere else. The
proof table needs a **mixed-priority skew workload** alongside the single-class
one, so the anti-serialization gate cannot pass while low-priority keys still
starve. Without that row, the gate measures the easy case and calls it fixed.

**The one thing this does not settle** is whether class disappearing from the key
is intended to change rotation behaviour or is a side effect of wanting it in a
column. The arithmetic is unambiguous about the *effect*; the option text does not
say whether the effect was the point. Worth confirming before the ORDER BY change
is built, because it is the difference between two fairness groups and one.

### `PartitionKey` decoupled — what it refers to is not determinable from source

An earlier revision of this section claimed this phrase meant `partitionKey()`
(`collector/extensionhost/mapping.go:69-73`), which returns the fairness key
verbatim, and that failing to decouple it would coarsen extension-host routing
along with the fairness unit. **That was wrong on every point, and a reviewer
traced it to source.**

`package_registry` and `vulnerability_intelligence` never reach `extensionhost`.
They run as their own binaries and build `PartitionKey` themselves, with no
reference to `FairnessKey`:

```go
// packageregistry/packageruntime/source.go:317
PartitionKey: fmt.Sprintf("%s:%s", target.Base.Provider, target.Base.Ecosystem)
// vulnerabilityintelligence/vulnruntime/source.go:357-362
func partitionKey(target TargetConfig) string { ... string(target.Source) + ":" + target.Ecosystem }
```

Neither package imports `extensionhost`. Neither partition key contains
target-class, so the coarsening I described is not something this decision could
introduce. And the proof obligation I attached — a test pinning that
extension-host partitioning does not coarsen — asked for proof about a path this
work never touches.

`extensionhost` is the runtime for the `component_extension` scheduler, which is
its own entry in section 2's list with its own key shape
(`component_extension_scheduler.go:166-172`) that has no class segment either.

**So what does "`PartitionKey` decoupled" refer to?** The phrase is in the
selected option verbatim, so it means something. It is not determinable from
source which consumer it addresses, and rather than invent a second answer, this
is an open question for the owner. Section 2's `partitionKey()` bullet remains a
true fact about `extensionhost` — it is simply not a fact this decision changes.

### Section 8's split argument, and why it does not survive

Section 8 argued for shipping step 1 alone on two grounds. The provability
ground — "bundled, neither is cleanly provable" — is answered by keeping separate
commits with their own proofs: an order-equivalence proof for the index, a
starvation proof for the ORDER BY change.

The **risk-isolation** ground is not answered, and I should not pretend it is.
Section 8 said step 1 was "worth landing alone: a real defect, independent of
fairness, carrying none of its risk," and gave the two steps independent rollback
stories — a dropped index versus a flag or a revert. Bundled, that is gone: one
PR, one revert, and rolling back the fairness change takes the index fix with it
unless the implementation keeps them separately revertible on purpose.

The owner accepted this explicitly — "plus more risk in one PR" — so it is a
priced decision, not an oversight. But section 8's recommendation and its rollback
row are **superseded by this section**, not reconciled with it, and I have marked
them so rather than leaving the document arguing both positions.

### Still open, and not settled by this decision

**Telemetry cardinality.** Nobody has measured how many distinct `fairness_key`
values a large single-instance family reaches, which bounds the top-N telemetry
in section 7. An earlier revision of this section resolved it by committing to a
knob with a conservative default plus a distinct-key gauge. **That was my
proposal, not a decision, and I was asserting it without the proof this document
demands of everything else.** A gauge re-evaluated on every scrape is a recurring
production query whose cost scales with exactly the cardinality nobody has
measured — the Prove-The-Theory-First rule applies to it as much as to the claim
path. It needs its own cheapest-shim measurement before it is committed to.
Recorded here as an open item.

**Weights.** Option C's weighting remains deferred, unchanged from section 8.

### The proof obligation the bundling creates

The mechanism is unchanged; what has to be proven before it lands is not. Two
obligations are now non-deferrable:

The **aggregate-throughput assertion** in section 6 is the only thing separating
this change from the failure mode the repository forbids outright — a fairness
rewrite that quietly costs throughput is a serialization workaround with a better
name.

The **priority-ordering regression test** is now a stated constraint rather than
a caution. `targetCreatedAt(observedAt, ordinal)` at `target_priority.go:43-49`
spaces priority by microseconds inside `created_at`, and
`TestPackageRegistryWorkPlannerPrioritizesDirectOwnedBeforeBroadTargets` pins it.
Any ORDER BY change that puts fairness ahead of `created_at` reverses that
contract unless the normalisation moves target-class into a real priority column
first — which is precisely why the selected option bundles the normalisation
rather than treating it as optional.

Sections 1-7 and 9 stand. Section 8's split recommendation, its step boundary,
and its rollback row are superseded.
