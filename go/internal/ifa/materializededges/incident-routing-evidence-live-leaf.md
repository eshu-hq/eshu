# incident_routing_evidence live leaf (#6228)

Single-scope cassette rendered from the compiled
`IncidentRoutingEvidenceFamilyOdu` by construction (lockstep holds it):
one PagerDuty scope carrying the incident anchor, applied/observed service
facts (plus the tombstoned applied negative control), and the
PagerDutyDeclaration content_entity fact the production loader reads out of
`content_entities`. The declaration rides the same scope so the handler's
service-name-scoped declared read is race-free (projector commits content
before it enqueues the generation's intent). Evidence identity is replay
fact_ids: the reducer keys applied/observed evidence off the envelope
fact_id, and the drive derives it per envelope as
`DeriveFactID(scope, generation, stable_fact_key)`, so the guard stamps the
same derivation before running the production builder -- proving the
fixture's literal fact names instead would certify an edge set no live
drive reproduces. The writer commits the incident anchor first and then the
per-slot edge group: NornicDB has no cross-statement read-your-writes in one
transaction, so a group that creates the shared anchor fails deterministically
with a UNIQUE violation on commit instead of matching it (the pre-fix live
drive proved the failure; the post-fix drive proves the shape).

- Baseline: facts-only fixture (declared half absent live, zero edges) and
  literal fact_id guard (applied/live targets no drive reproduces).
- After: `go -C go test ./internal/ifa/... ./internal/replay/cassette/ ./internal/reducer/incident/ ./internal/storage/cypher/edge/writer/ ./internal/storage/cypher/edge/materialized/ ./internal/storage/postgres/` all ok, including the new
  declaration-envelope, stamped-identity, incident-anchor-writer, and 5-fact
  lockstep tests; `bash scripts/test-verify-ifa-fault-injection.sh` and
  `scripts/test-verify-ifa-determinism.sh` pass exit 0;
  `bash scripts/verify-cassette-author.sh` passes exit 0;
  live N-cell drive exits 0 (three exact-set asserts, cross-N digest
  02485f5f6bf35e1769981ad7f3dab124f40c01a179e57b756eea8c17efbd0536);
  live fault shard 6/8 exit 0 (trio converges to the shared baseline digest
  cbef22239deaa1fffe636250d0e3de2ccfc74a83782a9bc4395f56c8d6685165;
  baseline 17s, killworker 75s, failgraphwrite 80s);
  `go vet` and `git diff --check` clean.
- Backend/version: hermetic guard plus live matrices on the gate-owned
  disposable Compose stack (Postgres + NornicDB v1.3.2 stack image).
- Input shape: incident_routing_evidence Odu (5 facts: 1 incident anchor,
  1 applied, 1 tombstoned applied, 1 observed, 1 PagerDutyDeclaration
  content entity), 100-cell fault corpus (97 at incident merge plus the 3-cell
  publishes trio in this union).
- Terminal counts: guard green on the 3-edge set; mirror 100-cell exact
  cover.

No-Regression Evidence: pure correctness fix, no measured optimization --
the writer change adds one small incident-anchor MERGE statement before the
unchanged edge group on the same 3-row/1-incident input shape; no measurable
regression check beyond the live N-cell convergence (before: deterministic
UNIQUE failure, zero edges; after: 3-edge exact set, zero dead letters, same
digest N=1,2,4). Statement count per write goes 3 to 4 on the fixture shape;
per-statement work is one UNWIND over distinct incident uids (fixture: 1) plus
the pre-existing per-slot batches, all anchored on the backed UNIQUE
IncidentRoutingEvidence.[uid] constraint.

No-Observability-Change: no spans, metrics, logs, or status fields added.
Guard failures still report through the existing
`resolveIncidentRoutingEvidence...` problem strings, and handler failures
through the existing reducer signals, so current gate signals are unchanged.
