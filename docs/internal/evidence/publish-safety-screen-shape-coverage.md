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

## C: what review found in the fix itself

Both reviewers filed against the fix above, and two of the findings are the same
class of mistake the fix was written to remove. They are recorded here because
"the screen was widened" is not the story; "the widening had its own shape
holes, in both directions" is.

### The compressed-IPv6 rule matched a prefix, not a token

Filed independently by both reviewers, which is why it was treated as mechanism
rather than taste. The unbracketed arm was spelled
`[0-9a-f]{1,4}::[0-9a-f:]*`, and `[0-9a-f:]*` matches the empty string, so any
1-4 hex-character token followed by `::` matched on its prefix alone no matter
what followed. `db::connect`, `ca::cert`, `ff::Field`, and `abc::default` are
ordinary code-topic answers and every one of them was withheld — a regression
against `origin/main`, where they published clean, and exactly the "withholding
an honest answer is its own outage" failure this file's README warns about.

Both sides are now whole hextet groups with a boundary after the last one, so
the rule means "this token IS an address" instead of "this token starts like
one". The documented `abc::def` gap stays: both halves are valid hextets and no
boundary can separate them.

### The password rule missed the ordinary case

`\bpassword` does not match after an underscore, because `\b` counts `_` as a
word character. So `DB_PASSWORD:`, `POSTGRES_PASSWORD:`, and `my_password:`
walked straight past a rule added to catch password assignments — the
underscore-joined env key is the most common spelling of the thing being
screened for, not an edge case. A single-quoted or escaped key (`'password':`,
`\"password\":`) missed for a related reason: only a bare double quote was
allowed.

The key half is now a boundary that excludes letters and digits but not the
underscore, so a prefixed key matches and `checkPassword:` still does not.

### Screening on the keyword withheld schema answers

The other direction, filed at the same line: `password: string` and
`password: String!` are TypeScript and GraphQL declarations, and the rule
rejected the whole answer on the field name. The value is now the discriminator,
which is the conclusion `evidencebundle`'s `credentialPattern` had already
reached for the same reason.

The false-positive boundary the rule now draws, in one line: a password key
assigned a **type** (`string`, `String!`, `Option<String>`, `SecretStr`), a
**count** (`random_password: 3 resources` — Terraform's `random_password` is a
real resource, so this is the same shape as the `aws_secretsmanager_secret: 5
resources` false positive already recorded), or a **placeholder**
(`<redacted>`, `***`, `${DB_PASSWORD}`) is publishable; anything else is
withheld. Both halves are pinned in one table in
`TestUnsafeStringScreensPasswordKeysByTheirValue`, because a rule that rejects
every `password:` line and a rule that rejects none each look correct against
half a table.

Two gaps accepted and now stated in the package README: a key with no separator
before the word (`PGPASSWORD:`) is shape-identical to `checkPassword:` and is
not screened, and a digits-only or punctuation-only password reads as a count or
a placeholder.

### The no-echo fix introduced a new echo

`answerquality` was changed to report WHERE an unsafe value was instead of WHAT
it was. The locator it reports was then built as
`string(result.Surface) + " result answer_summary"` — and `Surface` is a plain
string field unmarshalled from the evidence file, validated nowhere. So the
change that took an unscreened value out of the detail put one into the locator,
which is published in full. The same raw field was rendered into thirteen other
criterion details.

It only failed to leak because the surface's own row is screened first and wins
the race, which is an accident of ordering, not a contract. Every rendering of a
surface or a family now goes through an enum label. Screening the field instead
would inherit whatever the screen misses — and the finding's own example of a
value the screen misses was the `*_password:` gap above, which was real.

### The IPv4 rules classified a version string as an address

`10\.\d{1,3}\.\d{1,3}\.\d{1,3}` with nothing after it reads the first eight
characters of `version:10.0.5.300` as `10.0.5.30` and refuses the bundle. Octets
are range-checked now, and the portless rule carries a right boundary that
excludes a digit but allows a dot, because `unreachable at 10.0.5.3.` ends a
sentence and that is still an address. Mutation 6a/6b below shows both halves
are load-bearing.

### Why the first round's own table reported these clean

The two false-positive findings above (the IPv6 right boundary and the password
keyword) were filed against a change whose honest-answer table already claimed
to cover them, and the table was green. It was green because none of its rows
could reach the rule.

The table's scope-resolution group was `std::collections::HashMap`,
`tokio::spawn`, `serde::json::Value`, `k8s::api::core`, and
`cast the column with value::text`, under a comment reading "a blanket
`hex::hex` rule eats these". Every one of those has a **non-hex** letter against
the `::` — `s`, `t`, `o`, `k`, `p`, `i`, `u`, `l` — so none is a hex-only token
and none can match `[0-9a-f]{1,4}::`. Running the shipped screen over them
returns false for all five, on the broken rule and the fixed one alike. The
group was chosen to illustrate the hazard and every member was immune to it.
`db::connect`, `ca::cert`, and `ff::Field` differ only in being spelled out of
hex digits, and all three were withheld.

The password half is simpler: the table had `checkPassword: 3 callers` and
`passwords: 12 rotated`, and no type declaration at all. `password: string` and
`password: varchar(255)` were named as publishable in prose without ever being
added as rows.

Both are the same defect as A and B one level up, and the reason this file keeps
a mutation table: reverting a rule and watching a test written *from* that rule
go red proves the test observes the rule, not that the rows exercise the case
the rule is for. The mutation the first round ran was honest and passed for a
real reason — its rows moved when the regex moved. It could not fail, because
no row in it was the shape being argued about. A false-positive claim is only
tested by a row that the broken code actually rejects.

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

### Re-measured after the review fixes

The review fixes make the IPv6 pattern longer and add a `FindStringSubmatch`
plus a value classifier to the password rule, so both benchmarks were re-run
against the PR head (`0425ecea0`) in a throwaway worktree on the same machine,
in the same session, best of 5 at `-benchtime=2s`. Absolute numbers drift
between sessions — the 7480 above was a different session — so only the
same-session pair is comparable:

| Benchmark | 0425ecea0 | with review fixes |
| --- | --- | --- |
| `BenchmarkUnsafeStringHonestCorpus` | 7728 | 7774 |
| `BenchmarkUnsafeStringRejection` | 1838 | 1768 |

Both are flat: +0.6% and -3.8%, inside the run-to-run spread of these
benchmarks (the honest-corpus samples ranged 7728-8840 before and 7774-8048
after). Nothing was traded for the correctness fixes.

The gating property the 6.5x row exists to protect is unchanged: the password
rule still runs only when the string contains `password`, and the IPv6 rule only
when it contains `::`. The value classifier is new work behind that gate, so
`BenchmarkUnsafeStringPasswordGateOpen` measures the shape that pays for it —
an honest `random_password: 3 resources` line, where the gate opens and the
regex and classifier both run to completion: **953 ns/op**. That is the same
order as the `::`-carrying string's ~1.3us and it applies only to strings that
carry the word.

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
cd go && golangci-lint run ./internal/query/... ./internal/evidencebundle/... \
  ./internal/answerguardrail/... ./internal/answerquality/...              rc=0
cd go && go test -bench BenchmarkUnsafeStringRejection \
  ./internal/answerguardrail/                                              rc=0
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

Each guard added for the review findings was broken the same way: mutate the
production line, `diff` the file against a pristine copy to confirm the revert
landed, check `go build` exits 0 so a compile error cannot masquerade as a
guard firing, run the suite, then restore and `diff` again to confirm the tree
is back. All six reverts produced a red, and the positive controls
(`TestUnsafeStringPositiveControl`, `TestValidateStillRejectsUniqueLocalIPv6`,
`TestValidateKeepsPublicIPv6InFreeTextUsable`,
`TestScoreKeepsAKnownSurfaceReadable`, `TestVerdictKeepsASafeRunIDReadable`)
passed under every one of them:

| Reverted | Red |
| --- | --- |
| the compressed-IPv6 right boundary | 12 sub-failures: all 5 hex-namespace rows in both the new table and the honest-answer table |
| the password key boundary, back to `\b` | 6 sub-failures, all the underscore-joined keys |
| the key's quote class, back to `"?` | 3 sub-failures: the single-quoted and escaped keys |
| the value classifier, rejecting on the keyword | 22 sub-failures: every declaration, count, and placeholder row |
| the locator, back to the raw surface | 1 sub-failure, reporting `echoes the unsafe value "internal-S3NT1NEL-surface"` |
| the IPv4 octet range (6a) / the right boundary (6b) | 5 sub-failures each — both halves are load-bearing on their own |

The locator mutation is the one worth reading twice: only the
`missed_by_the_scanner` case goes red. The `screened_by_the_scanner` case passes
against the broken code, because the surface's own screened row fails first and
hides the leak. A test written with only that carrier would have reported the
defect fixed while it was still there.
