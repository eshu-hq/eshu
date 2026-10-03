# Claim Evidence Lives In Known Locations

This page holds the full rule that the root [`AGENTS.md`](../../AGENTS.md)
summarizes under "Claim Evidence Lives In Known Locations". Read it before you
downgrade any capability, maturity, or support claim as "unvalidated".

A dangling evidence pointer is NOT proof of absence. Before downgrading any
capability, maturity, or support claim as "unvalidated" — especially an
outward-facing, marketing-visible one such as a `capability-matrix` support
tier or a `product-claims` maturity — agents MUST exhaustively check the
committed-evidence locations below. A specific proof-ID resolving to nothing
(e.g. a `remote_validation` ref with no artifact) means the pointer was never
wired, NOT that the capability is unvalidated; the evidence usually lives
elsewhere in this list. Downgrading a genuinely-validated claim is a
marketing-damaging false negative.

The evidence found MUST substantiate the specific tier the claim asserts —
this does NOT license retaining a top-tier claim on lower-tier proof. A
`production` / deployed-tier `supported` claim needs deployed evidence: a
committed `docs/internal/remote-validation/<slug>.md` production artifact, a
`scripts/run-remote-e2e-*` / compose driver, or a live-backend
`docs/internal/evidence/*.md` — NOT merely a local unit test that only
exercises a lower profile. When matching-tier evidence genuinely exists,
VALIDATE (wire the pointer to it), keep the claim, and confirm with the owner
before any bulk change. When it does NOT, take the action the remote-validation
contract already mandates: commit the matching deployed-validation artifact, or
downgrade the claim to the tier its committed evidence actually supports. Never
retain a `production:supported` matrix row (or a GA `product-claims` maturity)
whose sole committed evidence is a lower-tier test.

Committed validation evidence lives in:

- `docs/internal/evidence/*.md` — per-issue validation records, including live
  NornicDB Bolt-driver before/after validations of query/graph truth.
- `docs/internal/remote-validation/<slug>.md` — production-validation artifacts
  for capability-matrix `remote_validation` proof-IDs (#5407 gate).
- `go/internal/**/*_test.go` (e.g. `internal/query`, `internal/mcp`) — the
  `go_test` suites the matrix local profiles cite.
- `scripts/run-remote-e2e-*.sh` plus `docs/public/run-locally/docker-compose.*.yaml`
  — deployed / e2e drivers (the matrix `compose_e2e` evidence kind).
- `testdata/cassettes/` and `testdata/golden/e2e-20repo-snapshot.json` (the B-12
  golden snapshot) — replay/golden evidence.
