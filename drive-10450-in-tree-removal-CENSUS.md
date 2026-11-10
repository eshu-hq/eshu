# Drive step 4 prep — fresh store census (`eshu-hq/eshu#10450` in-tree removal lane)

Lane: `drive/10450-in-tree-removal` off `origin/main` `7e5e91712b`.
Probes read-only against live Postgres (`127.0.0.1:55433`), schemas
inspected via `\d` before citing (no guessed columns).
Window: 2026-11-10T10:28Z–11:05Z. Spelling: `<env>` elides the QA
environment qualifier per repo gate (exact strings live on `#10450`).

## 1. Reducer depth gate — NOT MET

`fact_work_items` reducer pending/claimed/retrying/running: **12,809**
(was 12,277 at the last `#10450` note; rising). Gate needs `<10,000`.

## 2. Fleet scope census, keyed on (`scope_id`, `source_uri`)

Fleet scope: `sbom-attestation://<env>/sbom-fixture-cyclonedx` (sole
non-window `sbom-attestation:` scope in `scope_generations`).

Generations:

| status | count | max ingested_at |
| --- | --- | --- |
| superseded | 586 | 2026-11-09 11:20:52Z |
| active | 1 | 2026-11-09 11:50:52Z |
| failed | 1 | 2026-11-09 21:45:29Z (window generation, see §3) |

Facts (`fact_records`, equality-scoped):

| fact_kind | count | source_uri | max observed_at |
| --- | --- | --- | --- |
| sbom.document | 588 | fixture URI | 2026-11-10 03:45:30Z |
| sbom.component | 2940 | fixture URI | 2026-11-10 03:45:30Z |
| sbom.dependency_relationship | 1764 | fixture URI | 2026-11-10 03:45:30Z |
| sbom.external_reference | 588 | fixture URI | 2026-11-10 03:45:30Z |
| sbom.warning | 1764 | fixture URI | 2026-11-10 03:45:30Z |
| reducer_sbom_attestation_attachment | 587 | (none, expected: derived downstream) | 2026-11-09 11:50:54Z |

Shape check: 587 in-tree generations × 14-shape (1+5+3+1+3+1
attachment) = 8218 rows, plus the 13 window rows = 8231. Exact.
`payload.collector_instance_id` is empty on **all** 8231 rows
(in-tree and host-minted alike), confirming the Leaf-1 criterion
tweak: "matching source" must key on (`scope_id`, `source_uri`),
never the instance field.

## 3. Window generation — FAILED at projection, delivered twice

- `component-generation:835bcf4b…` status **failed**. Its 13 facts
  carry observed+ingested `2026-11-10 03:45:30Z`: a **second**
  host-minted delivery recommitted the same 13 stable rows
  (idempotent generation-scoped commit, 13 rows total, no dupes).
  First delivery was 2026-11-09 21:45:29Z.
- Projector item for the window gen: `dead_letter`,
  `retry_exhausted`, updated 2026-11-10 03:46:18Z:
  `enqueue reducer intents: reducer admission wait timed out:
  waited 5m0.658s exceeding 5m0s (reason high_water,
  queue_depth 10074)`.
- Fleet `fact_work_items`: projector 587 succeeded + 1 dead_letter;
  reducer 1111 succeeded + 63 superseded; **zero open items**.

## 4. Claim-plane fencing proof (`workflow_work_items` + `workflow_claims`)

- 587 completed in-tree claims, owner pod `eshu-cx-sbom-att-cx-<env>-…`,
  steady ~30 min cadence, last completed **2026-11-09 11:50:52Z**.
  No in-tree claim since (~23h silence; cause NOT established from
  the store — planner still planned the 12:25Z zombie below, the
  worker never claimed it; kubectl NOT_CHECKED from this box).
- 2 sbom-family pending items, both attempt 0, no claim rows, no
  owner, no lease:
  - fleet zombie (`sbom-att-cx-<env>` instance, planned 12:25:14Z),
  - host item (`sbom-8877-posfact-instance-1`, planned 13:22:25Z).
- No dual-active: no item carries both an in-tree and a host claim.
  Host-side claims live in the separate plane DB (not inspected
  here); the eshu-side host item is untouched since planning.
- Fencing holds: exactly one planner (in-tree coordinator) wrote
  eshu-side items; exactly one producer class completed each item.

## 5. Regeneration proof (no hand-edits to generated surfaces)

On `origin/main` `7e5e91712b`, both generators byte-clean:

- `go run ./cmd/capability-inventory -mode generate` (from `go/`):
  `no reconciliation findings`, 161 catalog entries / 649
  surfaces rewritten, `git status` clean afterwards.
- `bash scripts/generate-fact-kind-registry.sh`: wrote 326
  generated files, `git status` clean afterwards.

## 6. Lane verdict: removal criteria NOT met — no deletion

Against the `#10450` acceptance list:

- Serving cutover with matching census: NO (window gen failed;
  extracted path not serving; in-tree silent 23h unexplained).
- Dark restored with zero residue: NO (13 host facts + failed gen
  + 1 dead letter + 2 pending items remain).
- Reducer gate: NO (12,809 vs `<10,000`).
- Arbiter verdict on the removal plan: NOT OBTAINED (packet in
  `drive-10450-in-tree-removal-ARBITER-PACKET.md`; the arbiter-eshu
  role cannot be spawned from this lane's toolset — coordinator
  to run it).

Fail-closed: this lane deletes nothing. The local commit carries
this census, the regen proof, the candidate deletion set, and the
arbiter packet. No behavior change, so no TDD owed. No pre-push /
pre-pr / PR (coordinator-owned).
