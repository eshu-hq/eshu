# Arbiter ruling: #10671 NVD fallback source (2026-11-15)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu` (Fable). Subject: issue #10671, "CVE List v5 whole-corpus mirror
as NVD fallback". Two passes: the first ruling from five research lanes, and a
confirm-or-amend pass after the coordinator re-verified the load-bearing facts
first-hand. The amendments in the second part override the first part where
they conflict. The design is
[10671-nvd-feed-transport-fallback.md](../design/10671-nvd-feed-transport-fallback.md);
the measurements are in
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md).
Environment identifiers are redacted per #7803.

# Part 1. First ruling (five research lanes)

> Coordinator note, 2026-11-15: Part 2 and the design record supersede these
> Part 1 statements. The arbiter's text below is kept as written.
> - Follow-up 2 names a design file path that does not exist; the file is
>   `docs/internal/design/10671-nvd-feed-transport-fallback.md`.
> - "Lease stays the component default 600 s" is wrong: the code default is 60 s
>   and QA runs 10 m (Part 2, amendment 6).
> - The Modified span is about nine days, and the per-year seed is required, not
>   conditional (Part 2, amendment 1).
> - QA enable no longer waits on the #10706 horizon (Part 2, amendment 5).
> - The meta-404 rule (non-retryable, "must page", Part 1 lines on errors, R3,
>   and follow-up 1) is replaced: a missing or failing `.meta` means "not
>   covered", the API runs, and a counter pages; it never fails a claim.
> - "Raw `vulnerability.*` rows write no dirty-log rows directly" is right, and
>   it means rows per NVD fact is unmeasured; the design states it as an
>   assumption with a trip line, not a measurement.

### Ruling

**No CVE List v5 whole-corpus mirror. The NVD fallback is the official NVD 2.0 JSON data feeds (nvd.nist.gov/feeds/json/cve/2.0, same NVD_CVE 2.0 record shape, sha256 .meta) used as a second transport inside the existing single-anchor 6h NVD window claim (records sliced by lastModified into [cursor, cursor+chunk), same nvd_envelope, same stable fact keys, same scope grammar and cursor CAS), so every NVD-only field translates as identity and no contract changes; CVE List v5 remains the shipped owned-CVE class A/C/D supplement with cheap additive reads only; build the transport now, enable on QA only after the #10706 drain gate holds and the failing-first ladder is green.**

### Architecture

Feed-sliced NVD window transport (no new source, no mirror, no new scope family). The coordinator keeps minting exactly one NVD rolling window per planner run from the single cursor (planNVDWindow, go/internal/coordinator/vulnerability/nvd_window_planner.go:48-71; chunk const 6h at :26). Inside the window claim the satellite selects a transport per claim by coverage, not by a health heuristic: (1) GET nvdcve-2.0-modified.meta (verified live 2026-11-15T04:57Z: HTTP 200, TTFB 0.17 s, lastModifiedDate 2026-11-14T20:00:08-05:00, gzSize 7,350,752, sha256 9670229A...); (2) if the planned window satisfies window_end <= feed.lastModifiedDate AND window_start >= feed_span_start (the feed's oldest lastModified, about 8 days back; measured by the lane as 9 days), the window is COVERED and is served from the feed: stream the .gz, verify sha256 against .meta while streaming, stream-decode vulnerabilities[] (never hold the 86 MB), keep records whose lastModified falls in [start, end), and hand them to the unchanged NVDCVE -> envelope path (nvd_types.go:87-98 already carries vulnStatus, configurations, all four CVSS families; the feed header matches NVDCVEResult at nvd_types.go:22-35 with format NVD_CVE / version 2.0, confirmed on the API probe too); (3) otherwise the existing API walk (A+B: 6h chunk, 600 s envelope, 6 s pacer) runs unchanged. Commit is the same CommitClaimedScopeGeneration with the same scope id, generation and fencing token, and the same AdvanceNVDWindowCursor CAS-max (go/internal/storage/postgres/vulnerability/nvd_window_cursor.go:47-58), armed only for nvd/_window scopes (go/internal/collector/extensionhost/nvd_window_cursor.go:49-50). Correctness argument: a feed generated at T >= window_end holds, for every record, the lastModified value as of T; a record modified at t1 in the window and again at t2 in (T, now) appears in the window under the feed (t1 state) and is upserted again by the later window containing t2 under the same stable key (cve_id + source nvd; affected_product key cve_id + source + match_criteria_id + criteria, nvd_envelope.go:171-177), so the final state equals an API walk and nothing is double counted. Why a healthy system never uses the feed: the cursor is within one chunk of head only when lag < feed age, so window_end > T and coverage fails; the feed is reachable only when the cursor has fallen behind a daily feed generation, which is the degraded state. A config kill switch forces API. Lag bound under a total API outage: feed generation age (about 24 h, one sample) plus one chunk. Windows older than the Modified span (lag > ~8 days) are served by the per-year feeds through the same slicer, loaded through the already-merged baseline-store substrate (stream to disk, sha256, latest+previous retention; go/internal/vulnsource/cvelist_baseline.go) only if the lag measurement in required_proof says it is needed; year feeds are keyed by CVE id year, so a long-lag seed needs all year files, which is why it is gated on measurement. The CVE List v5 path is untouched for the fallback role: owned-CVE claims only (cvelist_collect.go:63 fails closed without cve_ids), classes A/C/D, source value cve_list, rank 5 above nvd 6 (provenance_selection.go:87-90, :113-116).

### Source choice

Source: NVD itself, via the NVD 2.0 JSON vulnerability data feeds (Modified for lag <= its span; per-year feeds only if measured lag exceeds it). Rationale ranked by accuracy, performance, reliability: (a) it is the US NVD source of record the owner fixed on 2026-11-05 (9872 design banner), and it carries configurations/cpeMatch with matchCriteriaId, vulnStatus, cvssMetricV2/V30/V31/V40 and NVD reference tags verbatim, so class B (affected_product) and NVD severity/status survive the outage with zero contract delta; (b) it is a static path that answered in 0.17-0.19 s while the API took 6.5 s (my probe) and 8-16 s with one 503 (alternatives lane, 6 requests), so it is independent of the API's latency face; (c) one GET of 7.35 MB replaces a paged walk, and it needs no new scope family, cursor table, readiness leg, scan-gate change or migration. Rejected for the fallback role: CVE List v5 whole-corpus mirror in any of D1/D2/D3 forms: 0/72 NVD analyst cpeMatch reproducible and 0/13,405 cpeMatch carry matchCriteriaId (census lane), vulnStatus has no equivalent beyond REJECTED, NVD Primary CVSS absent on 5/5 pre-2022 samples; emitting affected_product from it needs an additive-minor contract field plus a dedupe rule on finding identity (writer_identity.go:28-29,73-74), the migration 254 CASE (254:209-219) and the scan-gate caps (256 authorities / 50,000 facts, supply_chain_impact_authorized_advisory.go:21-22) all block it, readiness fails closed above 200 targets (readiness_postgres_query_targets.go:236) against 627 upstream shards, the per-claim 10k/2 MiB caps (cvelist_collect.go:26-27) make a full shard a non-retryable terminal (source_state.go:306-308), and a seed would flip SeveritySource to cve_list on every owned finding (rank 5 beats 6), rewriting about F findings into a dirty log that already cannot drain. fkie-cad mirror: different trust root, no license, unsigned commits, 49 h stall at probe. Vulnrichment: already arrives via CVE List ADP containers. OSV/GHSA: severity only. Keyed API tier: duration is bound by TTFB, not pacing (recovery ruling 2026-11-12 item 2), stays a measurement follow-up.

### Mode

`fallback_only`

### Decisions

1. D1 Architecture: feed-sliced transport inside the existing NVD window claim; one source (NVD 2.0 feeds); no CVE List whole-corpus mirror; no third-party mirror; no new scope family, cursor table, readiness leg or contract change.
2. D2 Mode: fallback by coverage. The feed serves a window only when window_end <= feed.lastModifiedDate and window_start >= feed span start; a caught-up cursor never meets the condition, so the API remains primary when healthy. Kill switch forces API. No health heuristic, no flapping.
3. D3 Field dispositions: under the ruled source every NVD field (cpeMatch with matchCriteriaId, vulnStatus, cvssMetricV2/V30/V31/V40 with source/type, reference tags, sourceIdentifier) translates as identity; contract class none. CVE List dispositions below apply only to the retained owned-CVE supplement.
4. D4 Single-anchor invariant unchanged: one window per planner run, one claim in flight, cursor advances only by the commit-tx CAS to the committed window end, never past the feed's generation timestamp (guaranteed by the coverage rule), never hand-edited.
5. D5 Overflow on the feed path is a prefix commit at a whole-lastModified-group boundary with the cursor advanced to that boundary (the remainder is replanned from the new cursor); it is never a shed and never ErrNVDCapacity terminal. The API path's shed/ErrNVDNoProgress semantics stay as ruled on 2026-11-12.
6. D6 Write budget is the existing NVD budget: <= 10,000 facts and <= 2 MiB per claim (nvd_output_budget.go:14-15), one window per planner run, no new dirty-log row class. Enabling catch-up on QA waits for the #10706 horizon plus re-tick deploy and its 2 h bar re-read.
7. D7 Rollout: build now in the satellite (plus core kill switch, telemetry, docs); do not wait for NVD API recovery because the transport is what ends the wait. QA enable is gated on the proof ladder, the #10706 gate, and the recovery ruling's pre-soak items (A+B deployed, timed head-window walk recorded).
8. D8 CVE List v5: keep as shipped (owned CVEs, 100/claim, 1 rps); file the cheap additive reads (cvssV2_0, reference tags into reference_type, rejectedReasons and dateRejected) as a separate small issue; do not build D1/D2/D3, do not regenerate shards (D2b), do not register cve_list as a public advisory authority.
9. D9 Correct the record: cvelist_shard.go:93 and 9961 evidence assume Facts == Records; the envelope emits 1 + distinct reference URLs (cvelist_envelope.go:34-49). The 9872 Phase 5 CVE List mirror rules are superseded for the NVD-fallback role; they remain provisional for any future catalog product, which is not this issue.
10. D10 Independent correctness fix surfaced by the lanes, filed separately: the class B product path picks its CVE row by source rank alone (provenance.go:50; product.go:54), so an unscored cve_list row hides NVD's score; make it score-aware like the package path.

### Field dispositions

| Field | Disposition | Contract class | Proof |
|---|---|---|---|
| affected_product.criteria / match_criteria_id / vulnerable / version_* / source_configuration_* / source_node_* (NVD cpeMatch) | translate (identity) under the ruled feed source; keep_nvd_only for the CVE List supplement | none | Feed carries configurations[].nodes[].cpeMatch unchanged (NVDCVE.Configurations, nvd_types.go:97; alternatives lane: 4,522 of 12,818 Modified-feed records carry cpeMatch). CVE List: 0 of 13,405 cpeMatch carry matchCriteriaId and 0/72 NVD analyst entries reproduced (census lane); the NVD stable key needs match_criteria_id (nvd_envelope.go:171-177) and the finding identity includes it (writer_identity.go:28-29,73-74), so a mirror product fact would double-count. Reducer hardcodes affected_product to nvd (derived_partition.go:342-348). |
| cve.status (vulnStatus) | translate (identity) under the feed; approximate under CVE List (PUBLISHED/REJECTED only) | none | Feed vulnStatus histogram over 12,818 records (alternatives lane). Retraction reads only rejected/withdrawn case-insensitively (retraction.go:53-64), so CVE List REJECTED retracts and PUBLISHED is inert; the analysis states have no CVE List equivalent (census: 13-CVE NVD sample Modified 7, Deferred 2, Awaiting 2, Analyzed 1, Undergoing 1). |
| cve.cvss_metrics.cvss_v2 (and v30/v31/v40 with source/type, exploitability, impact) | translate (identity) under the feed; CVE List: translate CNA cvssV2_0 as an additive read (2.19% of recent records), NVD Primary v2 stays NVD-only | none (untyped map, additionalProperties true) | nvd_types.go:109-114 carries all four families with Source/Type at :118-119. cveListBestCVSS reads only v40/v31/v30 (cvelist_envelope.go:147-169); 5/5 pre-2022 sampled CVEs had NVD Primary v2/v3 with no CVE List CVSS (census lane). |
| reference.reference_type (NVD tags) | translate (identity) under the feed; CVE List: approximate via an additive read of references[].tags (23.4% equivalence on same URLs) | none | Satellite sets reference_type to empty on cve_list (cvelist_envelope.go:112); census lane: 15 of 64 NVD tag assignments matched after normalization, x_refsource_* has no NVD analogue. |
| cve.source_identifier | translate (identity) under the feed; approximate under CVE List (assignerShortName, different namespace) | none | cvelist_envelope.go:81 emits AssignerShort; NVD uses an email or CNA UUID in 12/1 of 13 sampled records (census lane). |
| cve.modified_at / published_at / descriptions | translate (identity) under the feed; CVE List: translate published_at and descriptions, approximate modified_at (dateUpdated tracks CVE record edits, not NVD analysis edits) | none | cvelist_envelope.go:79-82; census lane 100% en descriptions on 4,438 published records. |
| cve.cwes (NVD Primary weaknesses, NVD-CWE-noinfo/Other) | translate (identity) under the feed; approximate under CVE List (CNA cweId 91.14%, text-only 3.79% missed) | none | cvelist_envelope.go:83 reads problemTypes cweId only; census lane CVE-2022-3845 NVD CWE-79 vs CNA CWE-707. |
| cve.cvss_score / cvss_vector / severity_label (selected metric) | translate (identity) under the feed; approximate under CVE List (CNA then ADP; equals NVD Secondary 8/8 v3.1, differs from NVD Primary 3/3 post-2021) | none | cvelist_envelope.go:125-144; reducer rank cve_list 5 above nvd 6 (provenance_selection.go:87-90) means a scored cve_list copy wins severity; owner must accept CNA-first severity or change the rank (tests provenance_cvelist_test.go:27,43 pin it). Not changed by this ruling. |
| cve.ssvc / unaffected / replaced_by | keep as cve_list-only enrichment (no NVD equivalent; not a fallback field) | none | cvelist_envelope.go:88-94; readers: ssvc and unaffected have no core reader (contract-consumers lane), replaced_by shipped under item 1.5. |
| cve.rejectedReasons / dateRejected (CVE List only) | translate as additive payload keys in the CVE List supplement (follow-up), not a fallback field | none (untyped) | 100% of 15 rejected delta records carry both (census lane); satellite models rejectedReasons at cvelist_types.go:53 but never emits it. |
| vulnerability.affected_product.source (kind-level attribution) | drop (do not add) under this ruling | would be additive-minor; not needed | Only a second affected_product emitter needs it; the ruled source is NVD itself, so derived_partition.go:342-348 and factschema_decode stay correct. |
| vulnerability.source_snapshot for the feed transport | translate: record transport=feed, feed lastModifiedDate and sha256 as additive snapshot payload keys | none (untyped payload keys); if a typed field is wanted later it is additive-minor | Snapshot payload already carries license/attribution keys the same way (cvelist_envelope.go:62-63). |

### Write budget

Bounded by existing constants, no new row class. Per claim: <= 10,000 facts and <= 2 MiB serialized (nvd_output_budget.go:14-15, same for the feed path), one ingestion generation, 2 dirty-log scope rows from the active_generation_id pointer trigger (migration 272:112-117), plus finding rewrites the reducer already produces for NVD windows. Per planner run: exactly one window (planNVDWindow, nvd_window_planner.go:48-71), so at the issue's stated 30-minute run cadence the NVD path can write at most 10,000 facts per 30 min = 333 facts/min and 4 scope rows/h whatever the transport; the feed makes each window cheaper and faster, not larger. Raw vulnerability.* rows write no dirty-log rows directly (migration 272:10-17; write-budget lane re-verified). The dirty-log inflow attributed to NVD catch-up (1.2-1.5k rows/min, #10706 comment, 13 ticks, cited only, internally inconsistent per the write-budget lane) is finding rewrites plus scope rows and is unchanged per window by this ruling; what changes is that catch-up resumes at all. Governor: do not raise planner cadence or chunk for the feed path until a QA measurement shows dirty_log inflow (rate of eshu_dp_supply_chain_impact_winners_capture_rows_total by source) <= 0.75 x measured identifiers consumed per minute after the #10706 horizon deploy; the lane's 764 rows/min break-even is derived (assumed 0.75 utilisation, derived mu) and is not a gate number. Unbounded fan-out is excluded by construction: no corpus facts, no shard regeneration, no catalog rows for unowned CVEs, bounded upserts by fact_id with fencing_token filter (10354 evidence EXPLAIN). Feed download cost: 7.35 MB gz per claim, re-fetched per claim (no cache needed at 2 windows/h), streamed, decompressed bytes capped (fail closed above 1 GiB, the largest year feed is 553 MB), RSS bounded by streaming (R7).

### Pacing and claim posture

Feed path: meta GET timeout 10 s; gz GET total timeout 120 s with a 100 KB/s stall floor (7.35 MB in <= 75 s worst case; year feeds get 600 s inside the 600 s claim envelope only after the lag measurement says they are needed); no inter-request pacer (one or two GETs per claim); sha256 verified while streaming, mismatch is RETRYABLE (feed regenerated between meta and gz reads) with a fresh meta on the next attempt; HTTP 5xx and timeouts are retryable; HTTP 4xx on the feed path is non-retryable only for the meta (a missing feed is a configuration-class event that must page), never for a transient gz 404 during regeneration (retry once with fresh meta). Attempt budget stays 10 (defaults.go:21), retry_after stays the satellite default 60 s, lease stays the component default 600 s with 20 s heartbeat; the host deadline is still the dispatch lease (claimed_service_dispatch.go:33 -> request.go:63 -> source.go:185). API path posture unchanged from the 2026-11-12 ruling (6 s pacer, 6h chunk, 600 s envelope). Claim and queue behavior during catch-up: no backfill wave, no new work-item class, no extra instance; the same one-window-at-a-time single-anchor serialization; fairness key unchanged; no concurrent same-cursor windows. Lease safety: a feed-served attempt that outlives its lease is fenced by the claim-row lock in the commit tx and fact upserts filter on fencing_token (ingestion.go:129-131; 10354 EXPLAIN Conflict Filter), identical to the API path. Overflow (> 10,000 facts or > 2 MiB in one 6h window) commits whole lastModified groups as a prefix and advances the cursor to that boundary: no shed, no terminal, remainder replanned next run. Telemetry (required, not optional): transport label {api,feed} on the existing window claim counters and duration histogram, gauge eshu_dp_vulnerability_nvd_feed_age_seconds (now minus feed lastModifiedDate), counter for sha256 mismatches, keyed/anonymous boolean already ruled; never log the key or the feed body.

### Cutover-back rule

Signal: coverage, computed per claim from read-only inputs (planned window from the single cursor; feed .meta lastModifiedDate and the feed's oldest lastModified). When the NVD API recovers and the cursor catches up to within one feed age of head, every newly planned window has window_end > feed.lastModifiedDate, coverage fails, and the satellite uses the API with no state change, no operator action and no flag flip. Who decides: the satellite, deterministically, per claim; the coordinator decides nothing new (it already holds when the cursor is at or past now, nvd_window_planner.go:63-65). Operator override: one config kill switch (nvd_feed_transport: off) forces API for the next claim onward; in-flight claims finish. No cursor hand-edit: the cursor advances only through AdvanceNVDWindowCursor in the commit tx, CAS-max, to the committed window end (or the prefix-commit boundary), which the coverage rule keeps <= feed.lastModifiedDate. No double counting: identical scope grammar (nvd/_window/<start>-<end>), identical stable fact keys, fencing on upsert; a record re-modified after the feed generation is re-upserted by the later window that contains its new lastModified, which is the same convergence the API path has. The NVD readiness leg is unchanged and still certifies from nvd window state rows, so readiness reflects true catch-up; no CVE List state row can satisfy it (readiness_source_age.go:53 excludes cve_list by design). Observability of the cutover: the transport label on claim metrics flips from feed to api; the existing lag gauge and the new feed-age gauge show lag < feed age at the moment coverage stops. Hysteresis is unnecessary because the condition is monotone in cursor position for a given feed generation; a flap test (R2b) proves zero transport oscillation over a synthetic Nov 9-10 recovery-then-relapse pattern.

### Rollout sequencing

Build now; do not wait for NVD recovery, because the drain stall is the problem this transport removes and the build touches only the satellite's NVD window path plus a core kill switch, telemetry and docs. Order: (0) record this ruling and the design doc; re-pin the pre-soak items of the 2026-11-12 ruling (confirm A+B is deployed on QA by image label, record a timed live 6h head-window walk; at today's 6.5-16 s TTFB a 6h worst slice of ~18 pages at 50/page is 225-400 s, inside 600 s, so A+B alone may already unwedge the API face: measure, do not assume). (1) Failing-first tests R1-R8 red in both repos. (2) Satellite feed transport green hermetically with a fixture feed; golden claim-plane trace. (3) Live disposable-DB proof against the real Modified feed for one covered window, diffed against an API walk of the same window if the API answers. (4) QA enable behind the kill switch only after the #10706 horizon plus immediate re-tick is deployed and its 2 h re-read meets the bars (keyed p95 <= 15 s, watermark age <= 60 s), because resuming NVD catch-up re-adds the inflow #10706 is draining. (5) Soak under the unchanged D4 bar of the recovery ruling (lag < 2x chunk, zero new failed_terminal nvd/_window, shed tally 0, zero invalid_result). (6) Only if the lag measurement shows lag beyond the Modified span: year-feed seed through the baseline-store substrate as its own issue. CVE List additive reads and the product-path representative fix proceed in parallel lanes; neither gates the transport.

### Failing-first regression plan

1. R1 (collector, red first): feed-slice equivalence. Fixture nvdcve-2.0 gz with records straddling a window boundary and an API fixture for the same window; the emitted facts (stable keys and payload digests) from the feed path equal the API path exactly; records with lastModified outside [start,end) are excluded; a record at exactly end is excluded (half-open).
2. R2 (collector): coverage selector. window_end > feed.lastModifiedDate -> api; window_start < feed span start -> api; covered -> feed; kill switch -> api; R2b flap test over a synthetic recovery-then-relapse sequence yields zero transport oscillation for a fixed feed generation.
3. R3 (collector): error classes. sha256 mismatch -> retryable (not ErrorClassNonRetryable); gz 5xx/timeout -> retryable; meta 404 -> non-retryable with a distinct class; decompressed size over cap -> fail closed retryable; nothing on the feed path maps to ErrNVDCapacity terminal.
4. R4 (collector): overflow prefix commit. A covered window whose slice exceeds 10,000 facts commits only whole lastModified groups, reports cursor advance to that boundary, emits no shed, and a replay from the boundary yields the remainder with zero duplicate stable keys.
5. R5 (core extensionhost): claim-plane golden trace for a feed-served window: same scope id grammar, commit policy arms the NVD cursor writer, cursor advances to window end or prefix boundary and never beyond feed.lastModifiedDate; lease > deadline > walk relation unchanged.
6. R6 (core live, existing fencing family): two attempts of the same window (feed attempt expired, API or feed retry commits) produce one final row set; stale attempt is fenced by fencing_token.
7. R7 (collector benchmark): streaming parse of a synthetic 553 MB feed holds RSS under a stated bound (propose <= 256 MiB) and never buffers the whole file; threshold asserted.
8. R8 (core telemetry gate): transport label present on window claim metrics, feed-age gauge registered, key never logged (telemetry-coverage doc updated).
9. R9 (core, independent): product path representative prefers a scored observation: cve_list row unscored + nvd row scored -> finding carries NVD's score; currently fails.
10. R10 (collector, independent, if filed): golden vectors for CVE List additive reads: cvssV2_0 into cvss_metrics.cvss_v2, tags into reference_type, rejectedReasons and dateRejected keys.
11. R11 (core docs/tests): cvelist_shard.go accounting test updated to facts = records x (1 + refs) with the 9961 T1 threshold re-stated; the stale 1:1 comment removed.

### Required proof

1. P0 Lag measurement (decides whether year feeds are in scope): on QA, SELECT cursor_unix_micro, head_unix_micro, updated_at FROM vulnerability_nvd_window_cursors; threshold: lag <= 8 days -> Modified feed only; lag > 8 days -> file the year-feed seed issue. Companion: for y in 2002..2026 GET nvdcve-2.0-$y.meta and sum gzSize; threshold <= 1 GiB gz to fit an emptyDir seed like the CVE List baseline.
2. P1 A+B deployment and timed live head-window walk recorded (recovery ruling pre-soak); if the walk already fits <= 600 s, say so in the design doc and keep the feed transport as the latency-independent path, not as the unwedge.
3. P2 Failing-first R1-R8 red then green; go test ./... green in both repos; shared golden vectors green in both trees.
4. P3 Hermetic claim-plane trace through the extension host with a fixture feed (grant -> deadline -> feed GET -> commit -> cursor CAS) naming every knob; no new knob beyond the kill switch.
5. P4 Live proof on a disposable DB (template-clone pattern from supply_chain_impact_cve_shard_writer_live_test.go): one covered 6h window served from the real Modified feed; record wall-clock, facts, bytes, sha256, RSS; diff against an API walk of the same window when the API answers; acceptable differences are only records whose lastModified moved after the feed timestamp.
6. P5 Feed shape equality first-hand: diff 20 feed records against the API's cveId= responses (lane did this only via fkie n=1); threshold: zero top-level key differences and equal lastModified.
7. P6 NIST stance on the 2.0 feeds read first-hand (browser, the data-feeds page is a JS app for curl/WebFetch); if a deprecation date exists, file a sunset follow-up; the ruling does not change because the feed is live with sha256 meta today.
8. P7 QA enable gate: #10706 horizon plus re-tick deployed and its 2 h re-read meeting keyed p95 <= 15 s and watermark age <= 60 s; then enable the feed transport and record dirty_log inflow and consumption for 2 h at >= observed inflow.
9. P8 Soak under the unchanged D4 bar: lag < 2x chunk at hourly samples, zero new failed_terminal nvd/_window, shed tally 0, zero invalid_result, other feeds fresh, transport label observed flipping feed -> api when caught up.

### Follow-up issues the ruling requires

1. **[eshu-collector-vulnerability-intelligence] NVD 2.0 feed transport inside the NVD window claim (coverage-selected fallback).** New nvd_feed_client.go (meta GET, streaming gz with sha256 verify, streaming vulnerabilities[] decoder, decompressed-size cap), coverage selector in the NVD window collect path, prefix-commit overflow rule at lastModified group boundaries, error-class mapping in source_state.go, transport label and feed-age telemetry hooks, fixture feed testdata. Files: nvd_client.go, nvd_collect*.go, nvd_output_budget.go, source_state.go, nvd_types.go (no field change expected), docs/concepts/README.md. Depends on: none. Acceptance: R1-R4 and R7 committed red first, then green; Per-claim caps unchanged (10,000 facts / 2 MiB); no new non-retryable class except meta 404; Feed path never advances or writes any cursor itself; it only returns records to the unchanged commit path; Live proof P4 and P5 recorded with wall-clock, bytes, RSS and diff result.
2. **[eshu] Core operability for the feed transport: kill switch, telemetry, golden trace, docs.** Config nvd_feed_transport (default on once soaked, off until then) in the vulnerability intelligence config/activation; transport label on window claim metrics; eshu_dp_vulnerability_nvd_feed_age_seconds gauge; sha256-mismatch counter; telemetry-coverage doc; claim-plane golden trace R5; widen NVDWindowCursorReader to return (cursor, head, updated_at) for coordinator-visible lag; design doc docs/internal/design/10671-cvelist-mirror-nvd-fallback.md and ruling record docs/internal/arbiter/10671-cvelist-mirror-ruling-2026-11-15.md. Depends on: none (parallel with the collector issue; the golden trace needs the collector fixture). Acceptance: R5, R6, R8 green; Kill switch forces API on the next claim with a logged reason; Telemetry coverage gate passes with the new label and gauge; Design doc states the coverage rule, overflow rule, budget and cutover rule verbatim from the ruling.
3. **[eshu] Measurement: NVD cursor lag, feed span, year-feed footprint, A+B deployment status.** Read-only: P0 SQL on QA, year-feed meta sizes, feed span (oldest lastModified in Modified), timed live head-window walk (P1), QA image label check for A+B. Record in docs/internal/evidence/10671-nvd-feed-measurements.md. Depends on: QA SSO refreshed. Acceptance: Lag figure with timestamp and the SQL used; Sum of year-feed gzSize with per-year table; Decision line: Modified-only or year-feed seed required; A+B deployed yes/no with the evidence command.
4. **[eshu] NVD year-feed seed through the baseline-store substrate (conditional).** Only if the measurement issue shows lag > Modified span: stream all year feeds to the host volume via the merged baseline store (sha256, latest+previous retention), slice them with the same feed slicer, outside any shard claim; singleton seed step with its own long lease. Depends on: Measurement issue result. Acceptance: Seed runs outside the 600 s window claim and is resumable; Windows older than the Modified span are served from year feeds with R1 equivalence; Disk footprint stated and under the pod ephemeral allowance (NOT_CHECKED today).
5. **[eshu-collector-vulnerability-intelligence] CVE List supplement: additive reads (cvssV2_0, reference tags, rejectedReasons, dateRejected).** Read CNA cvssV2_0 into cvss_metrics.cvss_v2, references[].tags into reference_type (kebab-case, first tag or joined), rejectedReasons and dateRejected as payload keys. Files: cvelist_types.go, cvelist_envelope.go, golden vectors. Depends on: none. Acceptance: R10 golden vectors committed red first; No change to stable keys or to the 10k/2 MiB caps; No new typed contract field; untyped keys only.
6. **[eshu] Product-path representative must prefer a scored observation.** representative() in core/provenance.go and the severity read in core/product.go use source rank only; make the class B path fall back across observations like selectFallbackSeverity does. Add the owner decision note on CNA-first severity (rank 5 vs 6) to the ADR. Depends on: none. Acceptance: R9 red then green; Existing provenance_cvelist_test.go pins unchanged; No change to finding identity.
7. **[eshu] Record corrections: Facts != Records for cve_list shards; 9872 Phase 5 superseded for the fallback role.** Update cvelist_shard.go accounting comment and test (R11), 9961 evidence note, 9872 design (Phase 5 CVE List mirror rules remain provisional for a catalog product only), close or retitle #10671 to the feed transport, link this ruling. Depends on: none. Acceptance: R11 green; 9872 and 9961 carry a dated supersession note; #10671 acceptance boxes map to the feed-transport issues.

### What will not be done

1. No CVE List v5 whole-corpus mirror in any form (D1 full corpus as facts, D2 deltaLog tail emission, D2b shard regeneration, D3 allowlist) as the NVD fallback.
2. No third-party NVD-shaped mirror (fkie-cad or similar) as a source, and no reading of it except as untrusted comparison data.
3. No new source value, no affected_product.source contract field, no cve_list affected_product emission, no migration 254 CASE extension, no scan-gate cap raise.
4. No new scope family, no mirror cursor table, no second collector instance, no readiness leg for a mirror, no change to fetchConfirmedSources.
5. No cursor hand-edit, no concurrent same-cursor windows, no worker serialization, no index added on a theory, no page-size raise without the oversize-page fallback proof (2026-11-12 ruling stands).
6. No change to the reducer rank cve_list 5 / nvd 6 in this lane; the owner decision on CNA-first severity is recorded, not pre-empted.
7. No keyed-tier pacer change without a measurement showing keyed requests change outage-mode TTFB.
8. No enabling of feed catch-up on QA before the #10706 drain gate holds, even though the transport is cheap, because catch-up inflow is the load #10706 is draining.
9. No accept-stale (option D) or timeout-skip (option C) semantics on the feed path; overflow is a prefix commit, never a shed or a skip.

### Re-verified by the arbiter

1. Satellite envelope emits 1 vulnerability.cve + 1 reference per distinct sanitized URL: cvelist_envelope.go:34-49; cve_list stable key is (cve_id, source=cve_list): :68-71; reference_type empty: :112; CVSS read is v40/v31/v30 only: :147-169.
2. Per-claim CVE List caps 10,000 facts / 2 MiB: cvelist_collect.go:26-27; owned-only fail closed: :63; capacity and request-budget errors classed non-retryable: source_state.go:306-308.
3. CVE Services base cveawg.mitre.org, 1 s spacing, 100 ids/claim, process-global pacer: cvelist_client.go:21,33,37,43.
4. NVD affected_product stable key includes match_criteria_id and criteria: nvd_envelope.go:171-177; NVD per-claim caps 10,000 / 2 MiB: nvd_output_budget.go:14-15; NVD pacer 6 s: nvd_pacing.go:12; NVDCVEResult wrapper with resultsPerPage/startIndex/totalResults/format/version/timestamp: nvd_types.go:22-35,45-48; NVDCVE carries VulnStatus, Configurations, all four CVSS families with Source/Type: nvd_types.go:87-98,109-119.
5. Reducer ranks cve_list 5 and nvd 6 in both tables: provenance_selection.go:87-90,113-116; unknown 999: :94,118; cve_list admitted: derived_partition.go:422-428; affected_product hardcoded to nvd: :342-348; retraction only on rejected/withdrawn: retraction.go:53-64,164-175; product path reads one representative row: provenance.go:50, product.go:54; finding identity includes match_criteria_id: writer_identity.go:28-29,73-74; negated products excluded: index_build.go:154-161; evidence budget 100,000: evidence_truncation.go:98.
6. Scan-gate caps 256 / 50,000 and cve_list -> [vulnerability.cve]: supply_chain_impact_authorized_advisory.go:21-22,131-132,163-172; migration 254 CASE lists only nvd/osv/cisa_kev/first_epss and raises 'public advisory target source unsupported': 254:209-219; 64-target cap: 254:140.
7. Readiness: cve_list arm requires cve_ids: readiness_postgres_query_targets.go:78-89; 200-target overflow: :236; latest planner run per instance DISTINCT ON: :324-325; advisory family confirmed sources exclude cve_list: readiness_source_age.go:53.
8. NVD cursor: PK collector_instance_id, head_unix_micro, updated_at, GREATEST CAS: nvd_window_cursor.go:23-26,47-58; the read selects cursor and head only: :65; coordinator reader interface drops head: vulnerability_intelligence_service.go:26-27; cursor writer armed only for nvd/_window scopes: extensionhost/nvd_window_cursor.go:49-50; commit policy falls through to nil for non-GHSA scopes: commit_policy.go:133-139.
9. Scope grammar: SourceCVEList and authority-only match: scope.go:33-36,61-68; shard = 1,000 records and the stale Facts == Records assumption: cvelist_shard.go:22,93,116; 9961 evidence '1:1' at :62-69.
10. Planner mints one window per run [cursor, min(cursor+chunk, now)], holds when cursor >= now, chunk const 6h: nvd_window_planner.go:26,48-71; defaults lease 60 s, heartbeat 20 s, attempts 10, requeue cap 10: defaults.go:9-31; claim request carries Attempt and Deadline = LeaseExpiresAt: request.go:62-63; host deadline from Claim.Deadline: source.go:185; lease = ObservedAt + LeaseDuration: claimed_service_dispatch.go:33.
11. Dirty-log capture owners and pointer-trigger OLD/NEW scope rows: migration 272:10-17,112-117; keyed caps 50,000 / 0.5: supply_chain_impact_winners_keyed.go:22-23.
12. #10706 comment: ~212 identifiers per tick over 13 keyed ticks, dirty log 1,029,617 -> 1,049,014, ~1.3k rows/min inflow, bars p95 <= 15 s and watermark age <= 60 s (gh issue view 10706 --comments; cited, not re-measured).
13. Live probes 2026-11-15T04:57Z: nvdcve-2.0-modified.meta HTTP 200 TTFB 0.17 s, lastModifiedDate 2026-11-14T20:00:08-05:00, gzSize 7,350,752, sha256 9670229A... (matches the lane); .gz HEAD 200 TTFB 0.19 s; API resultsPerPage=1 HTTP 200 TTFB 6.49 s, totalResults 425,770, format NVD_CVE version 2.0.
14. Prior rulings read in full: nvd-recovery-ruling-2026-11-12.md (A+B, single anchor, no C/D, shed not accepted loss, key-fixes-duration false) and 9872 design (US NVD source of record, Phase 5 provisional, baseline store rules), plus 10354 evidence (420,477 members, 654 MB wrapper, writer EXPLAIN).

### Lane findings the arbiter rejected

1. Alternatives lane framing that the 92-page head window at 8-16 s per page 'far exceeds the 300 s lease' is stale against main: the planner chunk is 6h (nvd_window_planner.go:26) and the component lease default is 600 s, so a 6h worst slice (~900 records, 18 pages at 50/page) is 225-400 s at today's TTFB. Its conclusion (feed is latency-independent) stands; its premise numbers do not, so P1 measures A+B first.
2. Claim-lifecycle lane recommendations to mint a 627-shard mirror scope family, a second collector instance, a baseline-sync singleton claim and a new mirror cursor table: rejected as unnecessary under this ruling (not as wrong analysis); the hazards it found (200-target readiness cap, latest-run shadowing, 10k/2 MiB terminal overflow) are exactly why the CVE List mirror is not chosen.
3. Write-budget lane's statement that D1, D2 and D3 'cost roughly the same in dirty rows' rests on the unrun assumption that every owned finding's digest changes on seed (it marked this plausible); treated as unproven and moot.
4. Write-budget lane's break-even figure (~764 rows/min) and post-#10706 capacity (1,019-1,081/min): derived from an assumed 0.75 utilisation and a derived re-tick cadence; rejected as a gate number, kept as planning context; the gate is a measured QA re-read.
5. Contract-consumers lane's optional path of adding affected_product.source and emitting products from cve_list: rejected for this issue; the lane itself showed limited yield (2 of 18) and a double-count hazard.
6. Alternatives lane suggestion to wire the API key into the pacer now: deferred, consistent with the 2026-11-12 ruling that key-fixes-duration is false until measured.
7. Field-census and contract-consumers assumption that the REJECTED -> Rejected mapping matters to the fallback: moot under the ruled source (vulnStatus is identity); it remains relevant only to the retained supplement and is already shipped behavior.
8. Claim-lifecycle lane's Q5 proposal of a hysteresis band on NVD cursor lag to start/stop mirror minting: rejected in favor of the per-claim coverage rule, which needs no thresholds and cannot flap for a fixed feed generation; widening the cursor reader to expose head and updated_at is kept as telemetry.

### Not verified (NOT_CHECKED)

1. Live QA state (SSO expired): current NVD cursor lag, whether A+B (6h chunk, 600 s lease) is deployed, dirty-log inflow and consumption after the #10706 horizon, worker replica count, claim-pacing config, emptyDir and ephemeral limits.
2. NIST's published stance and SLA for the 2.0 JSON feeds and their update cadence: the data-feeds page returned only an app shell to WebFetch; first-hand evidence is limited to the live .meta (200, sha256 present) and .gz (HEAD 200) at 2026-11-15T04:57Z.
3. Feed-versus-API record equality on NVD itself (lane compared fkie vs API, n=1); P5 covers it.
4. Modified feed span and generation cadence: one lane sample (about 9 days, generated 2026-11-14 20:00 EST); the coverage rule reads the span from the file each claim so the design does not depend on the figure.
5. Total gz size of all year feeds and whether a long-lag seed fits pod storage; P0 companion command covers it.
6. Whether keyed requests change outage-mode TTFB; no key used in any probe.
7. The write-budget lane's derived figures (mu 1,019-1,081 identifiers/min post-#10706, break-even 764 rows/min) and the 0.75 utilisation assumption: treated as planning context, not gates; the #10706 inflow figure is internally inconsistent (+19,397 rows over 13 ticks vs 1.3k/min) as that lane noted.
8. Whether a cve_list fact for an owned CVE changes the finding content digest in every case (plausible per provenance digesting; not run); moot for the ruled design.
9. The census lane's old-year prevalence figures (n=17 published) and the 13-CVE NVD comparison: directional only, as the lane said; not load-bearing for the ruling.
10. The 9805-9975 drain-blocked ruling file could not be located by the write-budget lane; its bars are taken from the #10706 approval comment.
11. Whole-corpus CVE List field prevalence (midnight zip not parsed by any lane); moot for the ruled design.

# Part 2. Confirm-or-amend pass

RULING: architecture confirmed (NVD 2.0 feeds as a coverage-selected second
transport inside the single-anchor 6 h window claim, fallback only, contract
class none), amended on four points.

## Amendments

1. **Year-feed seed is required and lands before QA enable.** The Modified feed
   spans about nine days; the QA cursor is about 28 days behind, so Modified alone
   cannot cover the first window (it ends 2026-10-18) and never advances the
   cursor. The seed may be a second PR; QA enable needs both. The first
   ruling's follow-up 4 is no longer conditional.
2. **Feed horizon is the feed's maximum observed `lastModified` (M1), not the
   `.meta` write time.** The `.meta` time ran 44 minutes ahead of the newest
   record in the one sample, and NVD does not publish the generation snapshot
   time, so M1 is the only instant proven included. Cost: at most one feed
   cadence of extra lag; the cutover-back rule keeps its shape (a caught-up
   cursor always has `e = now > M1`).
3. **Coverage predicate (per claim, UTC, half-open `[s, e)`).** Inputs: the
   Modified feed's observed span `M0 = min lastModified`, `M1 = max
   lastModified`, both computed while streaming with sha256 verified; a year-set
   manifest with each year's `.meta` time `D_y` and sha256; `R = max D_y`; seed
   validity horizon `V = R - 24 h`. Covered by feeds when `e <= M1` and (`s >=
   M0` or `e <= V`). Served records are the year-set records with
   `lastModified` in `[s, e)`, overlaid by Modified records by CVE id
   (Modified wins). Otherwise the API runs. A strict per-file rule (`e <=
   D_y`) is rejected: it would send every window after Oct 21 to the API
   because of dormant year files. Evidence: the coordinator's 12,818 Modified
   records (nine days) and the arbiter's 32,939 records in the band between the
   oldest dormant file date and the Modified span start (17 paced API pages,
   equal to `totalResults`) together give 45,757 records over 25 days with zero
   records changed after their own year file was written and before the latest
   daily run. The hole between the seed and Modified spans is real only when
   `V < M0` (a seed older than about six days); re-seed, never assume.
4. **Runtime guard (policy monitor).** For every Modified record with
   `lastModified <= V`, require `D_{year(r)} >= lastModified(r)`. Trip:
   failure class `nvd_feed_year_stale` (retryable, never terminal), the year
   set is marked invalid until re-seeded, the window falls to the API, counter
   `eshu_dp_vulnerability_nvd_feed_guard_trips_total` increments, alert above
   zero. Dormant years with no record in either sample are vouched for by NIST's
   regeneration policy, which is empirical here, not documented. The design doc
   says so.
5. **QA-enable gate: split and govern, not wait for #10706.** Build and the
   disposable-database proof proceed now. QA enable is governed by a measured
   trip line, not blocked: the dirty-log inflow is OSV/GHSA-keyed (measurement
   M6), so the first ruling's premise that catch-up inflow is the load #10706
   drains was wrong and is withdrawn; the API path is already ungated, so a gate
   on the feed alone was inconsistent. Cadence stays one window per planner run
   (at most 10,000 facts per 30 minutes, about 20k dirty rows an hour worst
   case, about 4.6k expected). Trip line: NVD-proxy dirty rows above 15,000 an
   hour in two consecutive hourly samples, or oldest dirty-log `captured_at`
   older than 96 h (58 h at measurement), flips the kill switch off;
   re-enable when the oldest age is under 48 h. If a `source` label exists on
   capture rows, use it instead of the `CVE-` prefix proxy. The #10706 bars
   (keyed p95 15 s, watermark age 60 s) stay soak bars for the drain. No pausing
   or serializing OSV/GHSA.
6. **Lease correction.** Code defaults are lease 60 s and heartbeat 20 s
   (`go/internal/workflow/defaults.go:9-10`); QA sets lease 10 m and heartbeat
   30 s by environment. The first ruling conflated the two. One addition: the
   feed budget (7.35 MB at a 100 KB/s stall floor is 75 s, plus meta and
   decode) does not fit the 60 s default, so the selector chooses the API and
   emits `eshu_dp_vulnerability_nvd_feed_deadline_short_total` when the
   remaining deadline is under 150 s. The 10 m deployed lease is the feed path's
   minimum.
7. **NIST stance (F7) is advisory, not required.** The load-bearing facts are
   first-hand (live `.meta` with sha256, 20 of 20 record equality, the 45,757
   record regeneration evidence). File a one-line sunset watch follow-up.

## Added failing-first tests and proof

- R12 dormant-file acceptance: 2003 file `D` = Oct 21, `R` = Nov 14, `V` =
  Nov 13, Modified fixture with no 2003 record; window `[Nov 1 00:00, Nov 1
  06:00)` must be feed-covered.
- R13 guard trip: add one 2003 record with `lastModified` Nov 7 (after `M0`) to Modified;
  the selector returns an invalid year set, class `nvd_feed_year_stale`, routes
  to the API, increments the counter; removing the record restores coverage.
- R14 horizon: a window with `e` between `M1` and the `.meta` time routes to
  the API.
- R15 stale-seed hole: `V < M0` and `s` between them routes to the API and never
  serves a partial set.
- P9: R12 to R15 red then green. P10: a seed refresh singleton downloads only
  years whose `D_y` changed, advances `V`, and verifies the manifest sha256.
  P11: six hourly samples after enable (NVD-proxy rows an hour, total rows an
  hour, oldest age) against the trip line.

## Will not do

No strict per-file year rule, no unguarded attested horizon, no pausing or
serializing OSV/GHSA to make room for NVD, and no reopening of the first
ruling's D1 to D4, D8 to D10, or the CVE List decisions.

## Arbiter's unverified list

Which factor causes the QA failures (see the coordinator section: the
key and egress theory did not survive); the core's finalize timestamps for the
stuck scope; NIST's written regeneration policy; the snapshot-to-write gap
(one sample); and the `CVE-` prefix as an NVD proxy (OSV also carries CVE ids).

# Part 3. Coordinator verification and what was not adopted

Checked first-hand before recording: the feed's shape and freshness, 20 of 20
record equality against the API, the 25-file year footprint (237.6 MiB gzip),
the regenerate-on-change test over the Modified feed, the lag (about 28 days), the
deployed lease, the dirty-log attribution, and the pod log count (66 of 66,
same latencies).

**Not adopted: the arbiter's "step zero".** The confirm pass inferred a
deterministic 503 on the pod's keyed request shape or egress IP and proposed
reproducing it with the QA key from a laptop, then unsetting or rotating the
key. The coordinator did not read or use the key: the Postgres credential grant
does not cover it, so the test ran inside the satellite container, with the key
staying in its environment. Result (measurement M10): keyed requests from the pod
succeeded in 11 of 13 requests, in the satellite's exact request shape and at
several page depths; one fast 503 appeared at one depth and one on an
anonymous request. The key, the egress IP, and page depth are not the cause.
No key change is recommended. Why the satellite fails 66 of 66 with 12 to
118 s latencies, each attempt making a single request, is still unexplained
and stays with collector#97. The feed transport is justified by the API's
mixed failure faces (fast edge 503s and slow first bytes) whatever their
cause, so no decision in this ruling changes.

Also unchanged by the coordinator's checks: `NOT_CHECKED` items 1 and 2 of the
first ruling are resolved (QA state read live; the NIST page remains a
secondary reading).
