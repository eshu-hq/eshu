# CI-Declared-Artifact Digest Selection Evidence (#5469)

Added by `f1fd95dbc9` (#5469) as an in-place addition to
`go/internal/reducer/README.md`'s "Supply-chain impact is evidence-first"
bullet, after that README was split into sibling docs (issue #5786).
Rescued here verbatim during the rebase onto that commit so the
content and its evidence markers are not lost; see
[`gotchas-supply-chain-and-vulnerabilities.md`](../../../go/internal/reducer/gotchas-supply-chain-and-vulnerabilities.md)
for the bullet this extends.

A matched `reducer_ci_cd_run_correlation` contributes
`ci_declared_artifact_digest` and `ci_declared_image_ref` only when that one
deployment matches the finding by digest or image reference. The reducer
selects the first exact subject-digest match before considering mutable
image-reference matches. Fact order breaks ties within the same match
strength. It copies both values from that one deployment as a pair, including
an empty sibling value, so separate rows cannot assemble an identity no
deployment declared. A repository/environment/operational-anchor match
contributes deployment context but leaves both CI-declared identity fields
absent. When no digest match exists, the selected image-reference match may
carry a contradicting digest; the reducer preserves it so the query layer can
disclose the disagreement without selecting the foreign digest as the
finding's resolved identity.

No-Regression Evidence: `go test ./internal/reducer -run
'TestBuildSupplyChainImpactFindings(BakesCIDeclaredArtifact|PrefersExactDigestAcrossFactPermutations|UsesFactOrderWithinEqualMatchStrength|WeakBranchBakesNoCIDeclaredArtifact)'
-count=1` covers digest-first selection under both fact permutations,
deterministic fact-order ties within each strength, atomic pairs, weak
operational matches, and preserved digest disagreement.

No-Observability-Change: the helper still makes one in-process pass over
already matched deployment rows and adds no query, queue, graph write,
metric, span, log field, or runtime knob. The existing supply-chain reducer
execution metrics, durable finding payload, and query spans expose the
selected pair.
