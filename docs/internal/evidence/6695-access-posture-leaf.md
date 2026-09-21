# #6695 access/posture leaf move evidence

## Moved (behavior-preserving, `git mv` + package/qualifier edits only)

- `go/internal/collector/secretsiam/` →
  `go/internal/collector/access/posture/` (28 files; the exact move the
  storage-collector-tree design prescribes).
- `package secretsiam` → `package posture`. No dual import exists with
  `projector/access/posture` (also `package posture`): no file imports
  both, verified by census.
- 56 importer files repointed by import path + qualifier only
  (`secretsiam.X` → `posture.X`): gcpcloud extractors, vaultlive,
  kuberneteslive, awscloud iam + awssdk, extensionconformance test.
- Preserved byte-identically (contracts, not paths): fact kind
  `secrets_iam_posture`, all span/metric names, envelope field shapes.
- Leaf trio paths updated (README purpose + perf command, AGENTS header);
  tree doc reworded to past tense; path-only updates to the 4786 matrix
  rows (collector paths only — reducer/storage/query/postgres siblings
  untouched), gcp-iam design, 1134 design, telemetry-coverage rows,
  collector-aws-cloud test command.
- No dirgate row covers this subtree (17 non-test files, under the cap);
  no lint exclusion names the old path (verified: no `secretsiam` entry
  in `go/.golangci.yml`).

## No-Regression Evidence:

Baseline: the pre-move file contents at origin/main 3fbf13ea6 (git
records the move at high rename similarity; code edits are the package
clause, importer paths/qualifiers, and gofumpt import re-sorting only).
After: `go test -count=1 ./internal/collector/access/...
./internal/collector/gcpcloud/ ./internal/collector/vaultlive/
./internal/collector/kuberneteslive/ ./internal/extensionconformance/
./internal/collector/awscloud/services/iam/...` green on the post-move
tree. Backend/version: unit-level proof only (fixture/fake-based
suites). Input shape: unchanged source configs and envelope inputs. Why
safe: the move is invisible at runtime (same package behavior, same
contracts); the only production change is the import path.

## No-Observability-Change:

No spans, metrics, structured logs, or status surfaces are added,
removed, renamed, or re-labeled: the telemetry-coverage rows keep their
exact metric names (path cells only).

## Proof (this worktree, before push)

- `go build ./internal/collector/...` + `go vet` on the leaf and all
  importer trees clean.
- `go test` green: access/posture leaf, gcpcloud, vaultlive,
  kuberneteslive, extensionconformance, awscloud iam (+awssdk).
- `gofumpt -l` clean on all touched Go files; every touched file < 500 lines.
