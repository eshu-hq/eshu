# Advisory retraction: REJECTED and withdrawn records retract their finding (#9695)

Epic #9209, Phase 1 item 1.5 (defect D5). A `Rejected` NVD record and a
withdrawn OSV record used to change only how an advisory ranked. They now
retract the finding through one path. This note records the cost of that path
and how an operator sees it.

## What changed on the hot path

- The reducer decides retraction once per CVE group when it builds a finding
  (`supplyChainCVEGroup.retraction`). A group retracts only if every source
  observation of the CVE rejected or withdrew its advisory.
- The decision rides the existing suppression machinery as a new hidden state,
  `advisory_retracted`. Reads gain one more member in an existing
  `NOT IN (...)` constant; no ordering, index, or join changes.
- The finding's identity does not change: `finding_id`, `advisory_id`, and the
  stable fact key do not depend on status.

## Measurement

Benchmark `BenchmarkBuildFindingsMultiSourceGroups` builds findings for 500 CVE
groups of three source observations each (one NVD, two OSV) and one package
each. "live" groups carry no status; "all_rejected" groups carry
`status: Rejected` on every observation. Base is `17db234576` (the branch point
on `origin/main`); head is the working tree. The file is
`go/internal/reducer/supplychain/core/retraction_bench_test.go`; the same file
ran on both trees. Twelve rounds, base and head alternating first mover each
round, `-benchtime 100x`, GOTOOLCHAIN `go1.26.10`, darwin arm64, host load
average 15 to 33 from other sessions, so the timings are noisy. These are the
figures after the round 4 refactor (retraction judged on the provenance
observations, so a package with no CVE fact counts as live); the earlier figures
were within a point of these.

| Case | Tree | Median ns/op | Min ns/op | B/op (median) | Allocs/op (median) |
| --- | --- | --- | --- | --- | --- |
| live | base | 9,389,682 | 8,886,015 | 15,770,778 | 169,712 |
| live | head | 9,481,309 | 9,163,525 | 16,692,903 | 169,715 |
| all_rejected | base | 9,492,159 | 9,133,723 | 15,794,053 | 171,213 |
| all_rejected | head | 9,764,852 | 9,515,344 | 17,220,336 | 176,715 |

Performance Evidence (#9695): for live groups, the common case, the finding
build costs +1.0% at the median and +3.1% at the minimum (about 0.2 to 0.6
microseconds per group), with the same allocation count (+3 in 169,712) and
+5.8% bytes per op. The bytes come from three added fields on the per-source
rows (`Status`, `ReplacedBy` on the decoded CVE, the provenance observation,
and the advisory-source row). The retraction check for a live group allocates
nothing: an early version allocated the result before checking the sources, and
moving the check first removed that allocation. A fully retracted group costs
more (+2.9% median, +3.2% allocations) because it builds the explanation; such
groups are rare and each is built once per pass. The pass is dominated by the
Postgres evidence load and write, which this change does not touch.

NOT_CHECKED: a Postgres `EXPLAIN` of the read queries with the longer
`NOT IN` list. The list grows from four to five constants inside a filter that
no index serves; the winner ordering is unchanged. A live gate run on the built
binary against a real backlog was not run.

## Operator signals

Observability Evidence (#9695): no new instrument. An operator finds retracted
findings in three places, none of which needs a payload read.

- `eshu_dp_supply_chain_suppression_decisions_total` gains the closed-enum
  `outcome` value `advisory_retracted` (labels stay `domain` and `outcome`).
- The reducer evidence summary gains `suppression_advisory_retracted=N`.
- The result sub-signals gain `advisory_retracted_findings`. It is distinct from
  `findings_retracted`, which counts rows the writer tombstoned.

`TestHandlerPublishesRetractedFindingAndReportsItToOperators` proves all three
on a real handler pass with a live finding beside the retracted one.

## Convergence

Limit, stated exactly. A finding can also be derived by another scope (KEV or
EPSS enrichment, another advisory source). Every pass loads CVE, affected-package,
EPSS, and KEV facts across scopes by `cve_id`
(`probeBranches` in `go/internal/storage/postgres/facts/impact/branches.go`), so
that scope's copy usually carries the same score as the retracting scope's copy.
The winners table keeps one copy per canonical key, ordered by `priority_score`,
then whether the row carries a payload finding id, then `fact_id`, which is a hash of the finding identity, scope, and
generation (`supplyChainImpactFactRowIdentity` in `go/internal/reducer/supplychain/core/writer.go`).
A retracted copy can therefore lose to a stale active copy in another scope, by
a higher score or by the tie-break, until that scope commits a new generation.
Equal scores are the common case, so the tie-break, which is effectively
arbitrary, often decides.

What triggers a rerun. A CVE-fact change enqueues `supply_chain_impact` for the
scope that committed it only: the scope-generation intent builder in
`go/internal/projector/runtime/scope_generation_intents.go` calls
`BuildSupplyChainImpactReducerIntent(scopeID, generationID, ...)`
(`go/internal/projector/supply/chain/impact/intents.go`), and the
entity key is `supply_chain_impact:<scopeID>`. Nothing fans a CVE change out to
the other scopes that hold findings for it.

So the lag is the holding scope's own cadence today (plan section 4.3: the KEV
scope re-runs about every 30 minutes; the flow document: EPSS rescores daily).
After item 1.4 it has no bound for a scope that skips unchanged generations:
`skipUnchangedGeneration` (`go/internal/storage/postgres/ingestion_backfill_seam.go`)
is idle without a `FreshnessHint`, and item 1.4 adds the hint, so a scope
whose own evidence did not change commits no generation and never re-derives its
copy.

The operator scope. `operator:vulnerability_suppressions` is a holder like any
other, with the worst bound. Its copy overrides the source copy's state in the
winners query, and the scope re-derives only when the suppression set changes, so
it refreshes only on a suppression mutation and has no bound today. Two outcomes
follow (O1, O2). O1: a CVE rejected after the operator copy was written keeps an
`accepted_risk` copy, and once the accepted risk lapses a rejected CVE reads as
`expired`. O2: a CVE restored after the operator copy was written as
`advisory_retracted` stays hidden. An operator suppression is authoritative over
its own dimension, the risk decision, and never over whether the advisory exists:
retraction wins in the derivation, and a restored record returns the operator's
unexpired decision.

Fix and ownership. The arbiter ruled for a one-hop fan-out as a separate
follow-up in this lane (1.5b). The pass finds the holders with one exact indexed
read of the writer's own finding rows by `cve_id` (the existing
`fact_records_supply_chain_impact_lookup_idx`, no new index), chunked by changed
CVE, and drops only its own scope, so the operator scope is a holder. After the
writer commit and before the queue acknowledgement it reopens
`supply_chain_impact` for each holder's current active generation, stamped so a
reopened pass does not fan out again. A pass fans out only when it changed a
write, which a content digest decides with `suppression_state` inside it, so an
operator copy flipping between `accepted_risk` and `advisory_retracted` counts.
No winners-order change: re-derivation of the holder's copy is the mechanism.
Item 1.5 (issue #9695) stays open until 1.5b
merges, and the 1.4 skip for vulnerability scopes is gated on it. This PR (1.5a)
discloses the limit. It narrows the old wrong answer (a withdrawn or rejected
record stays active everywhere) in the scope that re-derives, and it adds a new
one: a stale `advisory_retracted` copy in another scope can hide a restored, real
vulnerability until that scope reruns (probe V2 below). The arbiter ruled the two
changes atomic: 1.5a merges only as the first half of the 1.5a plus 1.5b
sequence, after 1.5b has a clean review and green CI on the stack, and 1.5b
merges in the same sitting; issue #9695 closes on 1.5b's merge.

### Probe

Private native Postgres 18.6. The winners query is the real
`supplyChainImpactWinnerSelectSQL`, dumped from the Go constant on snapshot
commit `d9fb007806`; the three tables hold only the columns it reads. Scope A is
the OSV scope with a retracted copy. Scope B is the KEV scope with an earlier
active copy. Both have the same canonical key. The probe's `fact_id` values are
literal and short; the real ones are hashes, so 2a and 2b show both ways a tie
can fall.

| Step | Winner | State | Score |
| --- | --- | --- | --- |
| 1. A retracted 50, B stale active 80 | B (`fact:B1`) | `active` | 80 |
| 2a. equal scores (50 and 50), A's id sorts first | A (`fact:A2`) | `advisory_retracted` | 50 |
| 2b. equal scores (50 and 50), B's id sorts first | B (`fact:0B1`) | `active` | 50 |
| 2c. B reruns, content unchanged, no new generation | B (`fact:B1`) | `active` | 80 |
| 3. B commits a new generation that re-derives the copy as retracted | B (`fact:B2`) | `advisory_retracted` | 80 |
| 4. control: B's new generation still sees a live record | B (`fact:B2`) | `active` | 80 |

Step 1 is the higher-score case. Steps 2a and 2b are the equal-score case, the
common one: the same retracted finding wins or loses on the hash alone. Step 2c
restates step 1, because SQL cannot show that an unchanged scope commits no
generation; that is the code cited above. Step 3 is convergence. Step 4 shows a
rerun does not retract anything its own evidence does not support.

The probe exited 0. Not run: the full Postgres schema, a real collector rerun,
or the maintainer's atomic resweep; the probe covers the selection query only.
To reproduce, dump `supplyChainImpactWinnerSelectSQL` to `winner-select.sql`
(a throwaway test that writes the constant to a file), start a private
Postgres, and run the script below.

<details>
<summary>Probe script</summary>

```bash
#!/bin/bash
# Probe (#9695): stale active copy vs retracted copy in the winners SELECT, on
# private native Postgres 18. The SELECT is the real Go constant (dumped).
set -u
S=${S_DIR:?directory holding winner-select.sql}
PSQL="/opt/homebrew/opt/postgresql@18/bin/psql -h 127.0.0.1 -p 35953 -U eshu -d postgres -X -q -v ON_ERROR_STOP=1"
WIN=$(cat $S/winner-select.sql)
SHOW="SELECT winner_fact_id, winner_scope_id, priority_score, suppression_state, source_count FROM ($WIN) q;"

$PSQL <<SQL
DROP TABLE IF EXISTS fact_records, ingestion_scopes, scope_generations, base;
CREATE TABLE ingestion_scopes (scope_id text PRIMARY KEY, active_generation_id text);
CREATE TABLE scope_generations (scope_id text, generation_id text, status text, PRIMARY KEY (scope_id, generation_id));
CREATE TABLE fact_records (
  fact_id text PRIMARY KEY, scope_id text, generation_id text, fact_kind text,
  is_tombstone boolean NOT NULL DEFAULT false, payload jsonb NOT NULL);

-- Scope A: the OSV scope that saw the withdrawn record. Scope B: the KEV scope
-- that expanded the same CVE on an earlier pass. Same canonical_key.
INSERT INTO ingestion_scopes VALUES ('vuln-intel://osv/npm/widget','a-gen-2'), ('vuln-intel://kev/catalog','b-gen-1');
INSERT INTO scope_generations VALUES
  ('vuln-intel://osv/npm/widget','a-gen-1','superseded'),
  ('vuln-intel://osv/npm/widget','a-gen-2','active'),
  ('vuln-intel://kev/catalog','b-gen-1','active');

CREATE TABLE base AS SELECT jsonb_build_object(
  'cve_id','CVE-2026-9001','advisory_id','GHSA-aaaa-bbbb-cccc','package_id','npm://registry.npmjs.org/widget',
  'impact_status','affected_exact','repository_id','repo:payments','observed_version','1.0.0',
  'requested_range','^1.0.0','cvss_score',7.5) AS p;

INSERT INTO fact_records SELECT 'fact:A2','vuln-intel://osv/npm/widget','a-gen-2','reducer_supply_chain_impact_finding',false,
  p || '{"priority_score":50,"suppression_state":"advisory_retracted"}' FROM base;
INSERT INTO fact_records SELECT 'fact:B1','vuln-intel://kev/catalog','b-gen-1','reducer_supply_chain_impact_finding',false,
  p || '{"priority_score":80,"suppression_state":"active","known_exploited":true}' FROM base;
SQL

echo "== 1. retracted copy (score 50) in the OSV scope; stale active copy (score 80, KEV) in the KEV scope =="
$PSQL -c "$SHOW"

echo "== 2a. equal scores (50 and 50), the common case: both passes load the same KEV/EPSS rows. The tie falls to fact_id, a hash of identity + scope + generation. Here A's id sorts first =="
$PSQL -c "UPDATE fact_records SET payload = jsonb_set(payload,'{priority_score}','50') WHERE fact_id='fact:B1'"
$PSQL -c "$SHOW"
echo "== 2b. same equal scores; B's id happens to sort first (a different hash would do this) =="
$PSQL -c "UPDATE fact_records SET fact_id='fact:0B1' WHERE fact_id='fact:B1'"
$PSQL -c "$SHOW"
$PSQL -c "UPDATE fact_records SET fact_id='fact:B1', payload = jsonb_set(payload,'{priority_score}','80') WHERE fact_id='fact:0B1'"
echo "== 2c. B reruns with unchanged content, so no new generation is committed: the state of step 1 =="
$PSQL -c "$SHOW"

echo "== 3. KEV scope reruns: a new generation b-gen-2 re-derives the finding, now retracted; b-gen-1 is superseded =="
$PSQL <<'SQL'
INSERT INTO scope_generations VALUES ('vuln-intel://kev/catalog','b-gen-2','active');
UPDATE scope_generations SET status='superseded' WHERE scope_id='vuln-intel://kev/catalog' AND generation_id='b-gen-1';
UPDATE ingestion_scopes SET active_generation_id='b-gen-2' WHERE scope_id='vuln-intel://kev/catalog';
INSERT INTO fact_records SELECT 'fact:B2','vuln-intel://kev/catalog','b-gen-2','reducer_supply_chain_impact_finding',false,
  p || '{"priority_score":80,"suppression_state":"advisory_retracted","known_exploited":true}' FROM base;
SQL
$PSQL -c "$SHOW"

echo "== 4. control: KEV scope reruns but its pass still sees a live record (no retraction) =="
$PSQL <<'SQL'
UPDATE fact_records SET payload = jsonb_set(payload,'{suppression_state}','"active"') WHERE fact_id='fact:B2';
SQL
$PSQL -c "$SHOW"
```

</details>

<details>
<summary>Probe output</summary>

```text
== 1. retracted copy (score 50) in the OSV scope; stale active copy (score 80, KEV) in the KEV scope ==
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state | source_count
----------------+--------------------------+----------------+-------------------+--------------
 fact:B1        | vuln-intel://kev/catalog |             80 | active            |            2
(1 row)

== 2a. equal scores (50 and 50), the common case: both passes load the same KEV/EPSS rows. The tie falls to fact_id, a hash of identity + scope + generation. Here A's id sorts first ==
 winner_fact_id |       winner_scope_id       | priority_score | suppression_state  | source_count
----------------+-----------------------------+----------------+--------------------+--------------
 fact:A2        | vuln-intel://osv/npm/widget |             50 | advisory_retracted |            2
(1 row)

== 2b. same equal scores; B's id happens to sort first (a different hash would do this) ==
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state | source_count
----------------+--------------------------+----------------+-------------------+--------------
 fact:0B1       | vuln-intel://kev/catalog |             50 | active            |            2
(1 row)

== 2c. B reruns with unchanged content, so no new generation is committed: the state of step 1 ==
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state | source_count
----------------+--------------------------+----------------+-------------------+--------------
 fact:B1        | vuln-intel://kev/catalog |             80 | active            |            2
(1 row)

== 3. KEV scope reruns: a new generation b-gen-2 re-derives the finding, now retracted; b-gen-1 is superseded ==
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state  | source_count
----------------+--------------------------+----------------+--------------------+--------------
 fact:B2        | vuln-intel://kev/catalog |             80 | advisory_retracted |            2
(1 row)

== 4. control: KEV scope reruns but its pass still sees a live record (no retraction) ==
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state | source_count
----------------+--------------------------+----------------+-------------------+--------------
 fact:B2        | vuln-intel://kev/catalog |             80 | active            |            2
(1 row)
```

</details>

### Why reordering the winner is not the fix

The obvious change is to sort retracted copies first at the six parity-SQL
sites (the winners store, `findings_queries.go`, `findings_split_queries.go`,
`suppression_split_sql.go`, `aggregates_queries.go`, and the explain query), nine
`ORDER BY` lines in all, plus the parity pins and an `EXPLAIN ANALYZE` per site
(engineering flow section 6). I did not make it, because a second probe shows it
trades one stale answer for another. The variant adds the leading key
`(suppression_state = 'advisory_retracted') DESC` to the real SELECT.

| Case | Current ordering | Retraction first |
| --- | --- | --- |
| V1 reject: A retracted 50, B stale active 80 | B, `active` (wrong) | A, `advisory_retracted` (right) |
| V2 restore: A re-derived active 50, B stale retracted 80 | B, `advisory_retracted` (wrong) | B, `advisory_retracted` (wrong) |
| V3 restore: A re-derived active 50, B stale retracted 40 | A, `active` (right) | B, `advisory_retracted` (wrong) |

The current ordering is already wrong in both directions whenever the stale copy
wins on score or on the hash tie-break (V1, V2). Retraction first fixes V1 and breaks V3: a stale
retracted copy would then hide a restored, real vulnerability whatever its score.
Hiding a real vulnerability is the worse error, so the ordering is a policy
choice and not a bug fix. The cause is the stale copies, not the sort key. The
two designs that remove it are CVE-keyed fan-out (a CVE status change re-derives
the scopes that hold copies) and resolving status at winner or read time from the
current CVE facts. The arbiter chose the first (1.5b).

### Interaction with item 1.4 (#9694)

On `origin/main` no vulnerability collector sets a `FreshnessHint`, so every
scope commits a new generation on each run and the stale-copy window is bounded
by the scope's cadence. The 1.4 working tree sets one: in
`go/internal/collector/vulnerabilityintelligence/vulnruntime/source.go` (the
`FreshnessHint: hint` assignment; the package README's `FreshnessHint` bullet
calls it a content digest of the normalized facts). Once that lands, a scope whose own facts did not
change skips its generation, so the window has no bound. 1.4 is what turns the
window into a standing gap, so the 1.4 skip for vulnerability scopes is gated on
1.5b.

<details>
<summary>Variant probe script</summary>

```bash
#!/bin/bash
# Probe (#9695), part 2: what would "retraction first" winner ordering do?
# Same real winners SELECT, with one extra leading sort key in source_winners:
#   (suppression_state = 'advisory_retracted') DESC
# Run against the same tables as run-winners-probe.sh (rerun that first).
set -u
S=$S_DIR
PSQL="/opt/homebrew/opt/postgresql@18/bin/psql -h 127.0.0.1 -p 35953 -U eshu -d postgres -X -q -v ON_ERROR_STOP=1"

CURRENT=$(cat $S/winner-select.sql)
# Insert the extra key right after the source_winners ORDER BY canonical_key,
python3 -I - <<'PYEOF' > $S/winner-select-retraction-first.sql
s = open('$S_DIR/winner-select.sql').read()
marker = "FROM source_ranked\n    ORDER BY canonical_key,\n             priority_score DESC,"
assert s.count(marker) == 1, s.count(marker)
s = s.replace(marker, "FROM source_ranked\n    ORDER BY canonical_key,\n             (suppression_state = 'advisory_retracted') DESC,\n             priority_score DESC,")
print(s, end='')
PYEOF
VARIANT=$(cat $S/winner-select-retraction-first.sql)
SHOW_CUR="SELECT winner_fact_id, winner_scope_id, priority_score, suppression_state FROM ($CURRENT) q;"
SHOW_VAR="SELECT winner_fact_id, winner_scope_id, priority_score, suppression_state FROM ($VARIANT) q;"

reset() {
$PSQL <<SQL
DELETE FROM fact_records; DELETE FROM scope_generations; DELETE FROM ingestion_scopes;
INSERT INTO ingestion_scopes VALUES ('vuln-intel://osv/npm/widget','a-gen-2'), ('vuln-intel://kev/catalog','b-gen-1');
INSERT INTO scope_generations VALUES
  ('vuln-intel://osv/npm/widget','a-gen-2','active'),
  ('vuln-intel://kev/catalog','b-gen-1','active');
SQL
}
put() { # id scope gen score state
$PSQL -c "INSERT INTO fact_records SELECT '$1','$2','$3','reducer_supply_chain_impact_finding',false, p || '{\"priority_score\":$4,\"suppression_state\":\"$5\"}' FROM base"
}

echo "== V1 reject: A retracted 50, B stale active 80 =="
reset; put A2 vuln-intel://osv/npm/widget a-gen-2 50 advisory_retracted; put B1 vuln-intel://kev/catalog b-gen-1 80 active
echo "current ordering:";          $PSQL -c "$SHOW_CUR"
echo "retraction-first ordering:"; $PSQL -c "$SHOW_VAR"

echo "== V2 restore: the record is un-rejected. A re-derived active 50; B still holds the stale retracted copy at 80 =="
reset; put A3 vuln-intel://osv/npm/widget a-gen-2 50 active; put B1 vuln-intel://kev/catalog b-gen-1 80 advisory_retracted
echo "current ordering:";          $PSQL -c "$SHOW_CUR"
echo "retraction-first ordering:"; $PSQL -c "$SHOW_VAR"

echo "== V3 restore, stale retracted copy scores LOWER (40) than the fresh active copy (50) =="
reset; put A3 vuln-intel://osv/npm/widget a-gen-2 50 active; put B1 vuln-intel://kev/catalog b-gen-1 40 advisory_retracted
echo "current ordering:";          $PSQL -c "$SHOW_CUR"
echo "retraction-first ordering:"; $PSQL -c "$SHOW_VAR"
```

</details>

<details>
<summary>Variant probe output</summary>

```text
== V1 reject: A retracted 50, B stale active 80 ==
current ordering:
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state
----------------+--------------------------+----------------+-------------------
 B1             | vuln-intel://kev/catalog |             80 | active
retraction-first ordering:
 winner_fact_id |       winner_scope_id       | priority_score | suppression_state
----------------+-----------------------------+----------------+--------------------
 A2             | vuln-intel://osv/npm/widget |             50 | advisory_retracted
== V2 restore: the record is un-rejected. A re-derived active 50; B still holds the stale retracted copy at 80 ==
current ordering:
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state
----------------+--------------------------+----------------+--------------------
 B1             | vuln-intel://kev/catalog |             80 | advisory_retracted
retraction-first ordering:
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state
----------------+--------------------------+----------------+--------------------
 B1             | vuln-intel://kev/catalog |             80 | advisory_retracted
== V3 restore, stale retracted copy scores LOWER (40) than the fresh active copy (50) ==
current ordering:
 winner_fact_id |       winner_scope_id       | priority_score | suppression_state
----------------+-----------------------------+----------------+-------------------
 A3             | vuln-intel://osv/npm/widget |             50 | active
retraction-first ordering:
 winner_fact_id |     winner_scope_id      | priority_score | suppression_state
----------------+--------------------------+----------------+--------------------
 B1             | vuln-intel://kev/catalog |             40 | advisory_retracted
```

</details>

## QA

Not observable until NVD or CVE List data flows (the item 1.5 acceptance note;
the phases plan records no NVD data in QA). An OSV withdrawn record would show
as `advisory_retracted` only after the OSV scope regenerates on a build with
this change. No QA probe was run for this note.
