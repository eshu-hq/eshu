# Evidence: retire reducer.SecretsIAMGraphProjectionFollowUpWriter (#6626 secretsiam-followup)

## What changed

One reducer-root compat alias with live callers is deleted from
`go/internal/reducer/compat_projection.go` (the stanza comment goes with it;
no other stanza in that file is touched):

- `SecretsIAMGraphProjectionFollowUpWriter` (canonical
  `secretsiam.GraphProjectionFollowUpWriter`)

Two caller files are mechanically repointed at the canonical spelling with
identical types (same interface, same method set, same construction):

- `go/cmd/reducer/secrets_iam_graph_wiring.go` (return type; the file already
  imported `secretsiam`; the `reducer` import stays for
  `reducer.DomainSecretsIAMGraphProjection`)
- `go/internal/reducer/defaults_handlers.go` (`DefaultHandlers` field type;
  the file already imported `secretsiam`)

The now-unused `secretsiam` import is dropped from `compat_projection.go`.
A guard test, `TestSecretsIAMFollowUpWriterAliasRetired` in
`compat_alias_retirement_secretsiam_test.go`, scans the whole reducer package
directory for any re-declared alias (bucket 4 pattern). The postgres
`SecretsIAMGraphProjectionFollowUpWriter` struct
(`go/internal/storage/postgres/secrets_iam_graph_projection_followup.go`) is a
different package's own type and is untouched; it already satisfies the
canonical interface (`var _ secretsiam.GraphProjectionFollowUpWriter`).

## Why this stanza

Fresh census on base `3a1d101259`: the alias had exactly two caller files and
no doc or script spellings (`git grep` over the whole tree). The stanza
comment names both callers as the deletion condition, and both now name
`secretsiam` directly. Excluded per slice scope: every other compat stanza
(cloud, decode, value families, projection runners) keeps live callers for
later slices.

## Why no regression

No-Regression Evidence: baseline `origin/main 3a1d101259`, after this head.
The retired alias was an identical type alias (`=`), so every repointed use
names the same interface with the same method set; the constructor still
returns the same postgres struct, which still satisfies the same interface,
and the `nil` flag-off posture is unchanged. No backend/version applies: no
graph query, queue, worker, lease, batch, runtime knob, or storage contract
changes. Input shape: none (spelling-only; no data read or written). Terminal
queue/row counts: unchanged by construction. Proof on this head:
`TestSecretsIAMFollowUpWriterAliasRetired` failed before the removal
(`compat_projection.go still declares ...`) and passes after;
`rg 'reducer\.SecretsIAMGraphProjectionFollowUpWriter'` over Go sources exits
1 (zero hits); `go build ./...` exit 0; `go vet ./...` exit 0;
`go test -count=1 ./internal/reducer/secretsiam/... ./cmd/reducer/...` ok;
`go test -count=1 ./internal/reducer/` ok. Live-backend execution stays with
CI (golden-corpus B-7, e2e snapshot B-12).

## Observability

No-Observability-Change: no metric, span, status field, route, or log
contract changes; nothing emitted or consumed the alias spelling, and the
trust-chain follow-up enqueue path keeps its existing spans and logs.
