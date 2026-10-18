# #8329 trusted-deletion attestation for aws/oci/tfstate: no-regression evidence

Scope: branch `feat/8329-tfstate-attestation`. AWS, OCI registry, and
Terraform-state collectors gain the `allow_empty_successor` operator
attestation that Jira/PagerDuty/Confluence ship (#8267, #8322), stamped onto
the claimed generation as `TrustedDeletion`. The extension host already
forwards SDK `Generation.trusted_deletion` (#8322), so no mapping change was
needed. The guard predicate `RefuseDestructiveSuccessor` is untouched.

No-Regression Evidence (config-parse-time booleans plus one post-parse gate):
the old code built generations without the stamp; the new code reads one
extra optional boolean per target scope/target/runtime (O(1), no allocation
beyond the config structs, no I/O, no lock, no retry change) and copies it
through one struct assignment per claim setup. Tfstate adds one post-parse
conjunction over three in-memory int64 counters
(`IsLegitimatelyEmptyBody`), also O(1) with no I/O. The flag defaults false
on every surface, so unattested inputs take the identical path before and
after: the stamp expression is `false || false`, and the tfstate gate
short-circuits on the flag before reading the counters. Baseline and after
suites: `go test ./internal/collector/terraformstate/
./internal/collector/tfstateruntime/
./internal/collector/awscloud/awsruntime/
./internal/collector/ociregistry/ociruntime/ ./internal/workflow/
./cmd/collector-aws-cloud/ ./cmd/collector-oci-registry/
./cmd/collector-terraform-state/ ./internal/envregistry -count=1` all `ok`
(GOTOOLCHAIN=go1.26.9). Input shape: attested and unattested configs on both
surfaces per collector, plus the legitimate-empty tfstate fixture
(`resources:[]`, outputs absent) and the missing/over-ceiling/mismatched
negatives. Queue/row counts: not applicable, the key is read at setup (or
once per claim from already-loaded config) before any enqueue and touches no
rows. Change is safe because the key defaults false on every surface and the
guard predicate itself is untouched.

Collector contract (idempotency/retry/ordering): claimed collection reuses
the work item's generation identity on every path (AWS stamps
`item.GenerationID`, OCI reuses it in `scanTarget`, tfstate requires the
resolved generation to match the claim and returns no generation on
mismatch), so a retried claim re-collects the identical generation and the
commit stays idempotent. The stamp is a deterministic function of
(static config, parsed shape): a retry of the same claim against the same
provider truth yields the same `TrustedDeletion` value, so retries cannot
flap the backstop decision. Ordering is unchanged: one generation per claim
in claim-poll order with the current fencing token; the flag adds no queue,
no reorder, and no cross-claim state. The tfstate gate additionally keeps
failure shapes (missing, over-ceiling, mismatched) on their existing
warning/no-generation paths with `TrustedDeletion=false` even when attested,
so an attested collector cannot launder a failed read into a stored empty
successor.

No-Observability-Change: the collection path emits no new spans, metrics, or
logs. `TrustedDeletion` travels inside the existing generation identity the
freshness hint already mints. No operator dashboard or alert changes. The
three new `ESHU_*_ALLOW_EMPTY_SUCCESSOR` variables are declared in
`go/internal/envregistry/entries_collectors.go` with the generated reference
doc updated.
