# In-Tree Collector Removal: AWS Cloud (#8852) + Terraform State (#8868)

Date: 2026-10-27. Branch: `drive/8852-8868-collector-removal`, base `b7021e694d`.
Verdict behind the tier2/v25 retirement: arbiter ruling recorded in this doc;
owner ordered the removal and waived the rollback rehearsal (roll-forward only).

## Scope

The production producers are the extracted satellites
(`eshu-hq/eshu-collector-aws-cloud`, `eshu-hq/eshu-collector-terraform-state`),
live with converging claims (see #8852 / #8868). This change deletes
the superseded in-tree producers and repoints every registry, contract, spec,
and doc that named them. Core-owned kernels stay: every file under
`go/internal/collector/terraformstate/`, `go/internal/collector/tfstateruntime/`,
and `go/internal/collector/awscloud/` is still imported by live core code
(storage, coordinator, parser, scope, facts — verified by whole-tree reference
census, zero dead files), so those packages are untouched.

Deleted in-tree producers:

- `go/cmd/collector-aws-cloud/` (binary, config, service, record mode,
  fixture estate, redaction derivation tests, docs)
- `go/cmd/collector-terraform-state/` (binary, config, service, S3/DynamoDB
  sources, target factory, docs)
- Product-image builds: `Dockerfile` (2 lines),
  `scripts/install-local-binaries.sh` (2 lines)
- the tier2 tfstate drift fixture estate (incl. the v25 generation) and
  the AWS runtime-drift fixture estate (exact paths in the revival recipe
  below)
- `docker-compose.tier2-tfstate.yaml`,
  `docker-compose.tier2-tfstate-v25.yaml`
- `scripts/verify_tfstate_drift_compose_tier2.sh`,
  `scripts/verify_tfstate_drift_compose_tier2_v25.sh`,
  `scripts/lib/tfstate_drift_tier2_compose_common.sh`,
  `scripts/verify_aws_runtime_drift_compose.sh`

Repointed (satellite owns the producer; kinds, projection, and read surfaces
stay core-owned): fact producer records (7), fact-kind families (7),
surface inventory overlays (readiness `implemented` → `partial`), replay
manifest (2 baseline + 2 fault rows removed), golden-corpus specs (2 removed,
2 cassette-replay aliases added), env registry stanzas, Helm deployment
comments, telemetry rows, skill baselines, and the service/collector docs
listed in the PR.

## Verification matrix

No-Regression Evidence: this change is strictly subtractive — it deletes the
in-tree producers and repoints registries/specs/docs, adding no Cypher, worker
claim, lease, batching, concurrency, or runtime compose/Helm path. Baseline is
the pre-removal tree at `b7021e694d`; after-measurement is the removal head.
Backend/version: local Go toolchain, no backend input-shape change (no new
facts, scopes, or queue shapes). Terminal counts: `go build ./...` clean, `go
vet` on touched packages clean, replay-coverage gate `--blocking` PASS
456/456 with 0 stale, `capability-inventory -mode verify` clean,
fact-kind registry `verify` clean. Safe because every deleted code path is
either gone from the product image or re-homed to the satellite, and every
remaining reference resolves (hostile-read census in the review record).

Observability Evidence: the AWS record-mode `collector.record.*` CLI log
events (`started`/`pseudonymized`/`completed`) leave with the deleted
the in-tree record-mode emitter (`record.go` in the removed command); no other emitter exists in-tree. All
runtime metrics, spans, and status signals are unchanged: the DP instruments
live in the kept kernels (`awsruntime`, `tfstateruntime`, `recordpolicy`) and
their `telemetry-coverage.md` rows were kept; only the record-mode row was
deleted and the pseudonym policy row now names the removal. No metric, span,
or status-signal name changed.

| Check | Result |
| --- | --- |
| `go build ./...` at removal head | clean |
| `go vet` on touched packages | clean |
| Replay coverage gate `--blocking` | PASS, 456/456, 0 stale |
| `capability-inventory -mode verify` | clean |
| Fact-kind registry `verify` | clean (run before push; generator output committed) |
| Tier-1 tfstate drift (`verify_tfstate_drift_compose.sh`) | NOT_CHECKED at removal head (needs live Docker; run before merge or keep this row) |
| Remote-e2e / live compose suites | NOT_CHECKED (live stack; same standing rule as every removal PR) |

## Live-source status (2026-10-27)

- aws-cloud satellite: claims completing, ~3,170 generation-stamped facts.
- terraform-state satellite: first-try completions all day; factless runs
  converge via the #9257 fix (runs `390eb145`, `258d1b25`, 0 fact items,
  `finished_at` set, no manual touch). Coordinator on `385b72ad`.
- Superseded in-tree Deployments keep rendering (chart templates retained
  with satellite-source comments); the product image no longer ships either
  binary.

## Coverage gap accepted (tier2/v25 + aws fixture E2E retired)

Retired, not carried: the tier-2 and v2.5 terraform-state drift end-to-ends
(buckets A/B/D/E plus the C/F two-pass) and the AWS fixture-mode drift proof.
Tier-1 proves the reducer drift handler with handcrafted facts but does NOT
prove the satellite→eshu claimed ingestion path (claim/scope/fact-shape
drift). Until revival, a regression in satellite-emitted terraform_state or
aws fact shape is invisible to eshu E2E and caught only by satellite
conformance and live ops signals. This gap is accepted, not equivalent
coverage.

## Revival recipe

Restore base: `b7021e694d`. Restore paths:

```bash
git checkout b7021e694d -- \
  docker-compose.tier2-tfstate.yaml \
  docker-compose.tier2-tfstate-v25.yaml \
  scripts/verify_tfstate_drift_compose_tier2.sh \
  scripts/verify_tfstate_drift_compose_tier2_v25.sh \
  scripts/lib/tfstate_drift_tier2_compose_common.sh \
  tests/fixtures/tfstate_drift_tier2 \
  scripts/verify_aws_runtime_drift_compose.sh \
  tests/fixtures/aws_runtime_drift
```

Then repoint each harness at the pinned satellite image and its claimed-mode
activation (minio-backed S3 for tier2/v25; fixture estate for the AWS flow)
and re-assert the same drift-counter deltas. Blocker: the terraform-state
satellite has no operator S3-endpoint config (`s3http.go:63-79`, endpoint
override is test-only) and no fixture mode; the AWS satellite has no fixture
mode. Revival needs a satellite feature (new operator surface, own review
including endpoint-trust/SSRF posture), a satellite release, and a re-pin —
tracked by the follow-up issue linked from the PR, not by this removal.

## Follow-up

- Revival issue: eshu-hq/eshu#9288 (`Revive tfstate drift tier2/v25 E2E on
  satellite`, filed before merge, linked from the PR body).
- Rollback rehearsal: waived by owner 2026-10-27 (roll-forward only); recorded
  on #8852 / #8868 criterion 9.
