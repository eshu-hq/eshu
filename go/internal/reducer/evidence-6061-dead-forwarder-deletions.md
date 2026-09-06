# #6061 — deleting two dead compat forwarders from the reducer root

Two `*_compat.go` files left behind by earlier #6061 family moves are removed and
their callers repointed at the packages that own the symbols:

| file | what it held | why it was removable |
| --- | --- | --- |
| `search_vector_build_compat.go` | 6 type aliases into `searchvector` | no external `reducer.SearchVectorBuild*` caller |
| `iam_permission_grant_compat.go` | 4 unexported consts | sole in-root production caller repointed; the rest were test-only |

Every symbol was either unexported — so by construction no caller outside package
`reducer` could exist — or exported with no external caller. Nothing was forwarded
onward; the call sites now name the owning package directly.

A third file originally slated for deletion here, `decode_seam_compat3.go`, is kept.
Deleting it repoints `go/internal/relationships/gcp_evidence.go`'s doc comment away
from the `factschemaEnvelope` name it cites, which drags `go/internal/relationships/`
into the diff and trips the `parser-relationship-kit` gate's relationship-source
matcher (any non-test `.go` change under that directory). That gate has no
comment-only exemption, and satisfying it would mean adding a relationship
`*_test.go` and a mapping-doc edit purely to justify a one-line comment repoint --
noise that makes those docs worse, not lockstep. The decode-seam forwarder deletion
is left for its own follow-up, where paying the relationship-kit cost is a
deliberate, explained choice rather than a side effect of this PR.

Reducer root goes 339 -> 337 non-test files.

## Why this needs a performance marker at all

The `perf-evidence` gate fires because the repointed call sites sit in files on its
hot list (`service.go`, `iam_instance_profile_role_edge_rows.go`). The change itself
removes a layer of indirection rather than adding one, but "removing a wrapper is
free" is exactly the kind of claim this repo has been wrong about before: a wrapper
can be the thing a callee inlines *into*, and deleting one can push a caller across
the inlining budget in either direction. So it is measured, not assumed.

No-Regression Evidence: compared Go's inlining decisions for `./internal/reducer` at
`origin/main` (`7e5da0bc7`, this branch's merge base) and at this branch, in two
worktrees on the same machine:

    env -u GOROOT go build -gcflags='-m' ./internal/reducer 2>&1 \
      | rg 'can inline' | sed 's/^.*can inline //' | sort -u

    base: 828 inlinable functions
    head: 828 inlinable functions

    set difference (base minus head): none
    set difference (head minus base): none

`search_vector_build_compat.go` and `iam_permission_grant_compat.go` held only type
aliases and unexported consts -- neither is a function, so their removal cannot
change the inlinable-function set by construction, and the build confirms it: both
revisions inline exactly the same 828 functions, with no name lost and none gained.
`factschemaEnvelope` appears unchanged in both sets, since `decode_seam_compat3.go`
(its home) stays.

Behaviour is unchanged for the same reason the files were safe to delete: each was a
pure alias or one-line delegation, and every caller now names the same underlying
symbol.

No-Observability-Change: no metric, span, log field, or failure class is added,
removed, or renamed. The two telemetry-coverage rows that named these files are
deleted with them. Each carried only a `No-Observability-Change:` marker and no
required metric, so the verifier's required-metric set is unchanged — confirmed by
running its own extraction at both revisions and getting an empty difference in both
directions.

That deletion also takes `docs/public/observability/telemetry-coverage.md` from 1142
to 1140 lines against a 1142-line grandfather pin. That is the point of landing this
first: the page had been sitting exactly at its pin, so any family move needing to add
a row had nowhere to put it. The pin is deliberately left at 1142: the line-cap gate
will print a NOTE suggesting a re-pin to 1140, and acting on it would consume the
headroom this change exists to create, so ignore that NOTE until a family move uses it.
