# #6696 AWS mismatch-resolution leaf evidence

## Moved (rename-only `git mv`, zero code changes, same package)

- `go/internal/collector/awscloud/acm_types.go` → `constants_acm.go`
- `go/internal/collector/awscloud/cloudtrail_types.go` →
  `constants_cloudtrail.go`
- `go/internal/collector/awscloud/guardduty_types.go` →
  `constants_guardduty.go`

All three files verified const-only before the rename (every top-level
declaration is a `const (` block; zero type/func/var declarations), so
each services/ package (acm, cloudtrail, guardduty) now has its
`constants_<service>.go` mate. `constants_common.go` holds genuinely
shared consts (`ResourceTypeAWSAccount`, `ResourceTypeGeneric`) with no
owning package and stays for the catalog consolidation to absorb.

## Moved-ref repoint

- `scripts/verify-dirgate.sh` comment (path-only, no line suffix).

## No-Regression Evidence:

Baseline: the pre-move file contents at origin/main 3fbf13ea6 (git
records 100% rename similarity; zero byte changes). After:
`go build` + `go test -count=1 ./internal/collector/awscloud/` green,
and the moved-file-refs gate reports no dangling references. Why safe:
identical bytes, identical package, identical symbols.

## No-Observability-Change:

No spans, metrics, logs, or status surfaces exist in or near this
change (constants files only).

## Proof (this worktree, before push)

- `go build ./internal/collector/awscloud/` clean.
- `go test -count=1 ./internal/collector/awscloud/` ok.
- `verify-moved-file-refs.sh --base origin/main` clean.
- Every touched file < 500 lines (no file bodies edited at all).
