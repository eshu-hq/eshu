# Evidence: two credential screens that stamped an artifact as screened while carrying what they screened for

Two separate screens, both on live paths, both stamping their output as clean
while publishing the value they promised to catch. They are unrelated in code
and share only a failure pattern: the test fixtures that guarded them all had
the same shape, so the gap never showed.

- `answerguardrail.UnsafeString` — the publish-safety screen for Ask Eshu
  responses and the offline answer-quality scorecard
  (`go/internal/answerguardrail/guardrail.go`).
- `evidencebundle`'s private-host rules — the screen behind an evidence
  bundle's `redaction.rules: screened_private_endpoints`
  (`go/internal/evidencebundle/bundle.go`).

Both were reproduced against binaries built from `origin/main` at `fc462effc`
before anything on this branch was touched, so this is the product's behaviour
and not something the change introduced.

## A: `answerguardrail.UnsafeString`

The screen matched the literal substrings `http://` and `https://`, so a
connection string on any other scheme carried its password straight through.
`rawAddressPattern` was IPv4-only while its doc comment promised "raw
addresses". The credential fragment list spelled `password=` and not the YAML
and log-line form `password:`.

Driven through `eshu answer-quality-scorecard`, which runs the same screen, with
the value placed in `run_id` and again in a captured `answer_summary`. The
sentinel `S3NT1NEL` is synthetic.

| Value | origin/main | this branch |
| --- | --- | --- |
| `bolt://neo4j:S3NT1NEL@graph.example.com:7687` | rc=0 `[ok] publish_safety` | rc=1 `[!!]` |
| `postgres://eshu:S3NT1NEL@db.example.net:5432/eshu` | rc=0 `[ok]` | rc=1 `[!!]` |
| `svc:S3NT1NEL@host/tool` | rc=0 `[ok]` | rc=1 `[!!]` |
| `[fd00::1]:7687` | rc=0 `[ok]` | rc=1 `[!!]` |
| `fd00::1` | rc=0 `[ok]` | rc=1 `[!!]` |
| `password: S3NT1NEL` | rc=0 `[ok]` | rc=1 `[!!]` |
| `10.42.7.9` (positive control) | rc=1 `[!!]` | rc=1 `[!!]` |
| `http://graph.example.com:7687` (positive control) | rc=1 `[!!]` | rc=1 `[!!]` |
| `go/cmd/eshu/testdata/ask-eshu-local-proof-scorecard.json` (honest fixture) | rc=0, score 100 | rc=0, score 100 |

The two positive-control rows are the reason the first six rows mean anything.
A screening harness whose assertion has never fired reports every case clean and
looks identical to a screen that works.

The full before/after on the first row:

```text
before:  Answer-quality scorecard PASSED
           run   : bolt://neo4j:S3NT1NEL@graph.example.com:7687
           score : 100
           [ok] publish_safety: all captured prompts passed

after:   Answer-quality scorecard FAILED
           run   : [redacted: run_id failed publish safety]
           score : 88
           [!!] publish_safety: unsafe run metadata in run_id
```

Six of the pristine outputs carried the sentinel; none of the fixed outputs do.

### Findings no longer echo the value they refused

`answerguardrail.Finding` documents itself as describing a failure "without
echoing the unsafe value", and that package's README states it as an invariant.
`answerquality.scorePublishSafety` did the opposite, building
`"unsafe publishable evidence: " + unsafe`.

That is defensible as a local diagnostic, and it is not one. A
`CriterionScore.Detail` is printed to stdout, returned in the CLI's error to
stderr, serialized into the `--json` verdict, and copied verbatim into a
generated `FollowUpIssue` body. Three of those four are forwarded artifacts. So
the scorer now follows the neighbouring contract: a failure names the field
(`unsafe publishable evidence in api result answer_summary`) and never the
value. `Verdict.RunID` gets the same treatment, since the CLI prints it in the
header.

## B: `evidencebundle` colon boundary

Both private-host rules required the character before the host to sit outside
`[0-9A-Za-z.:-]`, so that a host spelled as the tail of a longer word
(`notlocalhost:80`) does not match. A colon was inside that set, and a colon is
exactly what precedes a host in a scope handle and a labelled diagnostic.

```text
before:  eshu evidence bundle export --scope 'repo:db.internal:5432' --out b.json
           rc=0; "repo:db.internal:5432" appears 4x in a bundle stamped
           validation.status "passed" with redaction rule
           "screened_private_endpoints"
         eshu evidence bundle export --scope 'repo:10.0.5.3' --out b2.json
           rc=0; same, 4x

after:   both exit 1 with "private endpoint is not allowed in evidence bundle"
         and write no file
         eshu evidence bundle export --scope 'repo:demo/service' --out bok.json
           rc=0, still exports (the widening did not just start refusing)
```

The pattern fix is **adopted from commit `6341dd234`** (branch
`6059-cli-evbundle`), not written here: remove the colon from the boundary for
the hostname and IPv4 rules, and split the unique-local IPv6 alternative into
its own `privateULAv6Pattern` keeping the original boundary.

Its stated reason for the split was verified rather than taken on trust.
`TestRelaxingTheULABoundaryWouldRejectPublicIPv6` compiles the rejected shape —
the ULA rule carrying the relaxed boundary — and shows it matching
`peer 2001:db8:fd12::1 unreachable` on the middle hextet, while the shipped
pattern does not. The same test also shows the relaxed shape still matching a
real ULA, so the demonstration is not passing on a broken regex.

Added on top of that commit: an IPv4-mapped IPv6 case (`::ffff:10.0.5.3`), a
third public-IPv6 case, a test pinning the residual gap the split necessarily
leaves (`peer:fd00::1` is not screened), and the corresponding entry in
[Evidence Bundle](../../public/reference/evidence-bundle.md#redaction) so the
limitation is stated rather than rediscovered.

## Why no existing test caught either

For B, every case in `live_redaction_test.go` writes `"dial tcp <host>"`, which
always puts a **space** before the host. For A, no fixture carried a
non-http-scheme DSN at all.

Both are the same failure: the guard was real, the assertion was real, and the
fixture only ever exercised one carrier shape. That is why the new tables in
this change carry an `alreadyCaughtValues` row set that rides along with every
sweep — a table cannot report its new rows clean without having also exercised
something it is known to reject.

The two axes are distinct and a fix for one does not cover the other. B's hole
is the **boundary** character; A's is the carrier **shape**.
`TestUnsafeStringIsIndifferentToTheSurroundingDelimiter` runs 20 carriers across
12 prefixes and 11 suffixes (2,640 combinations, counted in the assertion) and
finds no boundary sensitivity in A at all.

## No-Regression Evidence (publish-safety screen):

`UnsafeString` runs once per publishable string on the Ask response path, so
three added regexes are a runtime cost, not a free correctness win.

`BenchmarkUnsafeStringHonestCorpus` (8 honest strings — prose, colon-joined
citation handles, routes, a timestamp), best of 5 at `-benchtime=2s`, same
machine, same corpus:

| Shape | ns/op | vs before |
| --- | --- | --- |
| before this change | 6015 | — |
| new rules, ungated | 38906 | **6.5x** |
| new rules, gated (shipped) | 7480 | 1.24x |

The ungated row is why every regex is gated in `UnsafeString` on a substring it
cannot match without: `@` for userinfo, `::` for a compressed address, seven
colons for an uncompressed one, and the literal `password`.

The residual 1.24x is not spread evenly, and a split benchmark says where it
sits:

| Corpus | before | after | delta |
| --- | --- | --- | --- |
| the 7 corpus strings carrying no `::` | 5251 | 5502 | +4.8% (~36ns/string) |
| the 1 string carrying `::` | 678 | 1968 | +190% (~1.3us) |

So ordinary answer prose pays about 36ns per screened string — the gate scans
themselves — and only text carrying a scope-resolution operator pays the regex.
An Ask response screens on the order of tens of strings, which puts the ordinary
case at roughly a microsecond per response against a request measured in
milliseconds.

`evidencebundle`'s patterns run once per bundle export, not on a hot path, and
the change moves one alternative between two compiled patterns without adding a
scan.

## No-Observability-Change:

Neither package emits telemetry, by design —
`go/internal/answerguardrail/README.md` states it explicitly, and callers decide
how to surface findings. No stage, worker, queue, query, or retry path is added
or changed, so no new metric, span, or log applies.

What an operator sees does change, and it is caller-side and already covered:
the Ask response carries the existing
`runtime answer guardrail blocked publishable prose: publish_safety` and
`derived deterministic summary withheld: failed publish-safety scan`
limitations for more inputs than before, the scorecard's existing
`publish_safety` criterion fails for more inputs, and an evidence bundle export
returns the existing `private endpoint is not allowed in evidence bundle` for
more inputs. All three are pre-existing signals firing on a wider set, not new
ones.

## Verification

Every command run after the final edit. Exit codes captured directly.

```text
cd go && go test ./internal/query/... ./internal/evidencebundle/... \
  ./internal/answerquality/... ./internal/answerguardrail/... -count=1     rc=0
cd go && go test ./internal/query/... -race -count=1                       rc=0
cd go && golangci-lint run ./internal/query/... ./internal/evidencebundle/... rc=0
bash scripts/verify-package-docs.sh                                        rc=0
bash scripts/verify-dirgate.sh --all                                       rc=0
bash scripts/verify-performance-evidence.sh                                rc=0
git diff --check                                                           rc=0
```

Mutation proof, each revert confirmed with `git diff` against `origin/main`
before the run and each red confirmed to be an assertion failure and not a
build failure (`go build` exit 0 first):

- reverting `guardrail.go` alone: 18 sub-failures across the three
  `internal/query` publish-path tests, with the `already_screened_control`
  subtest still passing in all three;
- leaving the colon in the two `evidencebundle` boundaries: 13 sub-failures,
  with `TestValidateStillRejectsUniqueLocalIPv6` and
  `TestValidateKeepsPublicIPv6InFreeTextUsable` both still passing;
- reverting `score.go`'s locator change: every echo assertion fails while
  `TestVerdictKeepsASafeRunIDReadable` passes.
