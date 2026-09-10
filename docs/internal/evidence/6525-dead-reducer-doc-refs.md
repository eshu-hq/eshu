# #6525 — dead reducer doc references, and the gate that prevents recurrence

## What the branch does

Three commits: repoint doc references the #6061 subpackage moves vacated, add
`scripts/verify-moved-file-refs.sh` as a blocking pre-PR gate so a future move
cannot leave a dangling pointer, and record the two deliberately historical
references in an allowlist.

## Residual debt, disclosed rather than implied away

Commit `899f2d388` says "Repoint 40 of them across 50 files" and "Thirteen dead
paths are deliberate negative fixtures", which sums to 53 and reads as a
complete sweep. **It is not complete, and the arithmetic should not be read as
claiming it is.**

Re-derived over the commit's own scope (`docs/ scripts/ specs/ go/`, distinct
`go/internal/reducer/*.go` paths):

| ref | total referenced paths | dead |
|---|---|---|
| base `c74d5b6c5` | 517 | 53 |
| head | 506 | 27 |

Of the 53: 28 distinct paths were repointed, 13 are declared fixtures, 1 is an
undeclared fixture (`container_image_identity.go`, referenced only from
`scripts/test-verify-performance-evidence-inherited-marker.sh`), 1 is an
allowlisted transcript, 2 are self-test artifacts, and **13 ordinary repointable
dead paths remain**:

    ci_cd_run_correlation_writer.go              -> reducer/cicdrun/
    container_image_identity_provenance.go       -> reducer/containerimage/
    container_image_identity_writer.go           -> reducer/containerimage/
    eshu_search_document_domain.go               -> reducer/eshusearch/
    eshu_search_document_writer.go               -> reducer/eshusearch/
    platform_materialization_writer.go           -> reducer/platformfam/
    sbom_attestation_attachment_index.go         -> reducer/sbomattest/
    sbom_attestation_attachment_writer.go        -> reducer/sbomattest/
    secrets_iam_graph_projection_extract_test.go -> reducer/secretsiam/
    secrets_iam_trust_chain_writer.go            -> reducer/secretsiam/
    factschema_decode.go                         -> reducer/schemadecode/

Those categories were tallied across separate measurements and **do not
reconcile**: they sum to 56 against a base count of 53, an excess of 3. At least
one path is counted twice — a repointed path that is also a fixture, or a listed
remainder already inside the 28 — and splitting them correctly needs a full
re-run of the sweep, which is not done here. The LIST is the checkable part:
every entry can be confirmed with `git cat-file -e <base>:<path>`. Read the
category tally as approximate and the list as exact.

The first ten were missed because they are cited from
`docs/internal/design/4784-reducer-derived-fact-governance.md` and
`4786-contract-integration-matrix.md`, which this branch never opened.

**The eleventh was dead at the base too; what differs is why it is still here.**
An earlier revision of this note called it "this branch's own doing". That was
wrong, and `git cat-file -e c74d5b6c5:go/internal/reducer/factschema_decode.go`
fails while the subpackage form resolves at that same base — so the reference
was already dead before this branch started, exactly like the other ten. What is
actually different is that it is the one path this branch tried to clear and had
to put back; why the 4784/4786 explanation does not cover it is stated further
down this section. Commit
`899f2d388` repointed the three `factschema_decode.go` references in the Go
comments of `go/internal/relationships/gcp_evidence.go`, and `fa222cef9`
REVERTED that file. The reason is recorded in that commit: the
parser-relationship kit reasons over file PATHS rather than diffs, so any
change under `go/internal/relationships/**.go` — a comment edit included —
demands relationship `*_test.go` coverage and a relationship-mapping docs
update, neither of which is meaningful for a comment. Teaching the kit to
recognise a comment-only change is the principled fix and is a larger change
than this branch should carry.

At this branch's head that file still carries three references (lines 28, 39
and 62) to `go/internal/reducer/factschema_decode.go`, a path that does not
exist — the live file is `go/internal/reducer/schemadecode/factschema_decode.go`.
Unlike the other ten, this one is NOT reachable from 4784 or 4786, so that
explanation does not cover it.

**The head row above predates this revert.** The table was measured in
`9e8939007`, which is an ancestor of `fa222cef9` (`git merge-base
--is-ancestor` confirms it), so its `27` counts the state before the three
references came back. Read the head dead-path count as one higher than the
table shows. The table is left as measured rather than silently re-stated,
because the number it reports is what that run actually produced.

**Why disclosing them matters more than the count being wrong.** The new gate is
branch-scoped: it only inspects paths the branch itself vacates. All eleven were
already dead at the base, so the gate will never report them, and there is no
decreasing baseline ledger (the sibling `verify-doc-citations.sh` has one) to
keep them visible. Without this note the sweep, the guard, and the commit
message would each independently hide the same 11 paths — and the eleventh is
the one a reader is most likely to be surprised by, because the branch touched
it and then put it back.

## Gate cost, measured

The gate runs one full-tree `git grep` per vacated path, so cost is linear in
the size of the move set. Measured on this host:

| vacated paths | wall | user | CPU |
|---|---|---|---|
| 0 (this branch) | instant | — | — |
| 19 (`--base 392351ffd^`, the RDS/S3/EC2 move) | 15.4 s | 2.95 s | 754% |
| 294 (`--base 85f7458e1`, a 52-commit window) | 132 s | 45 s | 1051% |

`85f7458e1` is a pre-rebase object that exists only in the authoring clone —
`git branch -r --contains` finds no remote ref for it — so the 294-path row
and the 43-reference proof below are **not reproducible from a fresh clone as
written**. They are recorded as measured rather than restated against a base
that was never measured. The `392351ffd^` rows are on `main` and are the ones
a reader can re-run.

15 s for a realistic extraction PR is acceptable for a blocking gate. The
header's cheapness claim is correctly scoped to the no-move case; this table is
the number that was missing.

## Gate behaviour, proven rather than asserted

- fires correctly at repo scale: `--base 85f7458e1` -> exit 1, **43 dangling
  references**, each naming the correct repoint target
- clean on a slice already fixed: `--base 392351ffd^` -> 19 vacated, no findings
- **fails closed**: `--base 0000000000000000000000000000000000000000` ->
  exit 2, "the scan did not run" — never a silent pass. The all-zero ref is
  used deliberately so any clone reproduces it; the gate fails at the `git
  diff` before any sweep, so the control is one git call
- the self-test's five negative controls assert exit 1, so it proves failure
  and not merely success: move-and-forget, outright deletion, a move+rewrite
  git leaves unpaired, an allowlist entry leaking to another referencing file,
  and the widened-base control that makes the attribution case non-vacuous

Known scope limit, tracked in #6525: the gate matches only fully-qualified
`go/internal/...` paths, so any other spelling is invisible to it. The live
examples sit in `go/internal/exposure/sink_catalog.go`, and the spelling there is
neither repo-relative nor `go/`-prefixed — it is a `reducer/<file>.go` shorthand
inside the `Provenance:` strings. Measured at this head: seven such shorthands,
of which THREE no longer resolve because #6061 moved their targets into
subpackages —

    reducer/iam_escalation_materialization.go   -> reducer/iamescalation/
    reducer/sql_relationship_materialization.go -> reducer/sqlrelationship/
    reducer/security_group_reachability.go      -> reducer/secgroup/

three of the other four name their subpackage and resolve; the fourth,
`reducer/shell_exec_materialization.go`, is flat and resolves because that file
was never moved. Stating the shorthand
rather than "repo-relative" matters, because a reader looking for
`internal/exposure/...`-style paths in that file finds NONE — the blind spot is
wider than one alternate spelling.

Second known limit, same class: the reference scan is `git grep -F`, so a
vacated path is matched as a plain substring. A vacated `a/b/c.go` therefore
also matches a longer literal that merely starts with it, such as
`a/b/c.golden.json`. Reproduced deliberately; there are zero such pairs in the
tree today, so it reports no false positive now, and it can only ever produce
a false POSITIVE (a reference reported that is not one), never a false
negative that lets a genuinely dead reference through.

## Why the perf-evidence gate fired on this branch

Worth recording, because the demand is spurious. `is_comment_only_change()` in
`scripts/verify-performance-evidence.sh` recognises comments only at column 0 —
it strips the leading `+`/`-` without trimming whitespace. The one hot file it
flagged, `reducer_queue_readiness_sql.go`, has a diff consisting entirely of a
single **tab-indented** comment line inside a `var` block, so its own
suppression logic failed to suppress it. Ten of the eleven changed runtime files
were correctly auto-skipped.

The markers below are therefore truthful rather than ceremonial: this branch
changes no behaviour.

No-Regression Evidence: comment-only across the changed Go runtime files,
verified by parsing each with `go/parser` WITHOUT `ParseComments` and reprinting
via `go/printer` — 15 of 17 changed `.go` files are byte-identical after that
reprint. The two exceptions are both test/tooling-only: a path inside a
`t.Fatalf` string in a `_test.go`, which the gate excludes, and the
`scriptWorkflowSoundSubsetCount` 41 -> 42 test-expectation bump for this gate
itself. No production behaviour changes, so there is no runtime
path to measure. The new gate is a pre-PR shell script, not a runtime path; its
cost is the table above.

No-Observability-Change: this change adds no runtime signal and removes none. A
dangling reference surfaces as a gate failure naming `file:line` and the vacated
path; nothing is emitted at runtime.
