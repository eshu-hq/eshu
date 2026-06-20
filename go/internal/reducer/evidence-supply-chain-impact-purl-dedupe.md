# Supply Chain Impact PURL Dedupe Evidence

Issue #3176 normalizes `vulnerability.affected_package` evidence that omits
`package_id` by deriving the canonical package identity from its PURL. Explicit
source-provided `package_id` values still win, and malformed or non-PURL input
remains unjoined instead of guessed.

No-Regression Evidence: baseline failed with duplicate findings for one CVE
when a canonical affected-package row and a PURL-only affected-package row
described the same package. The red command was
`go test ./internal/reducer -run TestBuildSupplyChainImpactFindingsDedupesPURLOnlyAffectedPackage -count=1`;
it produced two impact findings before the fix. After measurement:
`go test ./internal/reducer -run 'TestBuildSupplyChainImpactFindings(DedupesPURLOnlyAffectedPackage|MatchesSBOMComponentByCanonicalPackageID|MatchesSBOMComponentByVersionStrippedPURL|RefusesImagePathWithoutAttachment|ConnectsRuntimeEvidencePath|DoesNotPromoteAmbiguousImageIdentity|KeepsStaleServiceEvidenceMissing|UsesOwnedLockfileVersion|ExposesDependencyChain|ConsumesDeploymentOnlyExactServiceCatalogEvidence|ClearsCatalogAnchorMissingWhenOperationalAnchorsExist)' -count=1`,
`go test ./internal/packageidentity -count=1`,
`go test ./internal/reducer -count=1`, and
`go test ./internal/reducer ./internal/packageidentity ./internal/query -count=1`
all passed locally. Backend/version: pure in-process Go reducer tests using the
local Go toolchain, with no live Postgres, graph backend, queue, or network
dependency. Input shape: source fact envelopes containing one CVE, one
canonical affected-package observation, one PURL-only affected-package
observation, SBOM component evidence, SBOM attachment evidence, and container
image identity evidence. Terminal queue or row counts: unchanged; no reducer
queue rows, graph writes, or Postgres rows are created by these unit tests, and
the fixed reducer classification returns one terminal finding for the logical
CVE/package anchor.

No-Observability-Change: the change calls the existing pure
`packageidentity.PackageIDFromPURL` helper while decoding reducer input facts.
It adds no metric instrument, metric label, span, structured log field, status
field, route, runtime knob, worker, lease, queue domain, graph write, Cypher
statement, Postgres query, or deployment setting. Operators continue to
diagnose this path through existing reducer result summaries, impact finding
payload fields, readiness/missing-evidence envelopes, and the
`SupplyChainImpactFindings` counter emitted after reducer classification.
