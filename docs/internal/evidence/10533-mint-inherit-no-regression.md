# 10533: GHSA window mint inheritance adds no hot-path cost

Issue #10533, refs #10517, refs #10483, part of epic #9959. Coordinator-minted
GHSA window scopes inherit their anchor's host-minted opt-in and installation
mapping at both host gates, resolved only through the strict
`GHSAStreamScopeID` parser. Exact static entries still decide first; unopted,
unmapped, and parser-rejected lookalike scopes fail closed exactly as before.

## What the change does

`hostMintOptedIn` falls back to the anchor scope only after no static entry
matches the claimed scope, and `NewAppTokenProvider` looks up
`Installations[anchor]` only on an exact miss. The token still stamps the
window scope. No SQL, lock, retry, or queue-shape change anywhere on the
claim path.

No-Regression Evidence (#10533): the fallback adds one strict url.Parse only
on the already-missing opt-in path (exact match still decides first), plus
one map lookup on installation miss. Baseline `290076840d` vs branch head,
same-machine `go test ./internal/collector/extensionhost/ -count=3`
(linux/amd64, local, full package suite including the 7-case mint table):
1.734s vs 1.753s, inside run-to-run variance. Terminal dispositions
(auth_denied pre-launch, static-path passthrough) are pinned by table
tests, not timing.

No-Observability-Change (#10533): the fallback adds no metrics, spans, or
log lines; mint and deny outcomes ride the existing ClaimOutcome and
failure-class paths unchanged.
