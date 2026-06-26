# Evidence — #3866 cross-repo DEPENDS_ON (rc-3) in the B-7 gate

Scope: enable the B-7 golden-corpus gate to assert a real cross-repo
`DEPENDS_ON` edge (rc-3) and promote it to a required correlation. The edge is
produced by the package-consumption projection (`projection/package-consumption`)
resolving the consumer's `go.mod` `require` to the in-corpus owner repository
through the package-registry owner index.

## Change (hot path)
`go/internal/collector/git_selection_native.go`: in filesystem source mode,
synthesize a deterministic `remote_url` from the repo directory name when
`ESHU_GITHUB_ORG` is set (opt-in). Without an explicit org the field stays empty
rather than fabricating a github.com URL. This lets a package-registry
`source_hint` URL resolve to the in-corpus owner repository, which is the
identity the owner index (`FactStore.ListActivePackageOwnershipFacts`) joins
against.

The remaining changes are non-hot: a package-registry cassette identity +
source-hint fact (`testdata/cassettes/...`), Go fixture repos
(`tests/fixtures/ecosystems/{lib-common,orders-api}`), the gate script
(corpus list, `ESHU_GITHUB_ORG`, rc-3 promoted to required), and a test-file
split.

## Performance

No-Regression Evidence: the collector change adds a single string derivation per
discovered repo (basename → `repoRemoteURL`) gated behind an opt-in env var; it
introduces no new query, scan, or per-fact work. Owner resolution downstream
reuses the pre-existing, bounded `FactStore.ListActivePackageOwnershipFacts`
reader (bounded by the active package-registry generation, never the file corpus)
and the existing `(ecosystem, name)` owner-index join (#3598). End-to-end proof:
the B-7 golden-corpus gate (`scripts/verify-golden-corpus-gate.sh`) ran the full
pipeline over the corpus across repeated runs with `pipeline_wall_time` elapsed
~28–31s against the 900s baseline / 1800s ceiling — well within budget — and
rc-3 now reports `count=1` (`orders-api -> lib-common`) where it was `0`.

## Observability

No-Observability-Change: the synthesized `remote_url` flows onto the existing
`repository` fact (already emitted and committed by the git collector); an
operator inspects it through the existing repository fact/catalog surface. No new
metric, span, or log key is introduced by this change.
