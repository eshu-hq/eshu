#6950 batch 3. Moves all 110 caller files from the 48
`facts.*` docs-compat spellings (28 const/type aliases, 20 func
forwarders; 23 drop the `Documentation` prefix, the rest keep
their identifier under the `docs` qualifier) to `facts/docs`,
then deletes the emptied `go/internal/facts/compat_docs.go`.

Callers: collector 49, query 16, doctruth 11, reducer 9,
storage 6, semanticdocs 4, cmd 4, facts-root 4
(schema_version.go, semantic.go + tests, now reaching the docs
accessors directly), mcp 3, ifa 3, cli 1. Files that already
import another docs package use the `factsdocs` alias (9 test
files). No logic touched: const aliases and one-line forwarders
requalified to identical values.

Detectors: `kind_real_consumer.go` and
`kind_real_consumer_dispatch.go` learn the `docs.*` selector
spellings through a `factsPackageSelectorNames` table so the
consumer/disclosure gate keeps passing (fail-closed const
lookup).

Docs: facts and docs doc.go/README.md/AGENTS.md past-tense the
retired surface; query and reducer readmes use the `docs.*`
spelling. Ledger rows 6950-docs-batch3-before/after in
docs/internal/measurements.jsonl.

Evidence (before c3cc407cb0 / after 22b2fa4a17, 21 targets):
`go test -count=1` 20 ok / 1 pre-existing host-only fail
(TestFetchChurnZombiesDrainedByReaper, fails identically on the
clean base on this host, green in CI) on both sides, ok-set
byte-identical after timing strip. `go build ./...` and
`go vet ./...` clean. Gates green:
verify-fact-kind-registry, verify-factschema-diff,
verify-payload-usage-manifest, verify-contracttest, the mcp
consumer-disclosure gate, and the compat-surface gate. Zero
remaining `facts.*` docs-compat refs (two intentional
past-tense mentions in facts/doc.go and facts/docs/doc.go).
Independent eshu-code-review: READY, P0/P1/P2-blocking/P3 zero
after one fixed round-1 P2.

Refs #6950
