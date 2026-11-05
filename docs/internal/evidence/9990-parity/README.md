# Parity harness for #9990

These files are the throwaway capture behind
[the evidence note](../9990-representative-selection.md). They carry a `.txt`
suffix so no Go tool or gate treats them as source. Run them on detached scratch
worktrees and never on a branch.

To rerun, for a base commit and a head commit:

```sh
git worktree add --detach /private/tmp/parity-base <base>
git worktree add --detach /private/tmp/parity-head <head>
for w in base head; do
  python3 patch.py "/private/tmp/parity-$w"
  core="/private/tmp/parity-$w/go/internal/reducer/supplychain/core"
  cp capture.go.txt "$core/zz_capture.go"
  cp grid_test.go.txt "$core/zz_grid_test.go"
  (cd "$core" && PARITY_OUT="/private/tmp/$w.jsonl" go test -count=1 -timeout 8m .)
done
python3 compare.py /private/tmp/base.jsonl /private/tmp/head.jsonl
git worktree remove --force /private/tmp/parity-base
git worktree remove --force /private/tmp/parity-head
```

- `patch.py` adds one capture call to `BuildSupplyChainImpactFindings` and one to
  `BuildAuthorizedScanImpactFindings`. A test that exists only on one side shows up
  as a one-sided key, which `compare.py` counts and does not compare.
- `grid_test.go.txt` holds `TestParityGrid`, which feeds the grid through the same
  builder and labels each call with its shape. Every grid own-fact carries an OS
  purl, so the residual with no purl is covered by
  `representative_residual_test.go`, not by the grid.
- Each output is about 600 MB, so write it outside the repository.

## Multi-row grid (ruling 9990-f6)

The grid above gives every host one installed row, so it cannot see a group whose
joined rows are mixed (one with an installed version, one without). These files do.

- `multirow_grid_test.go.txt` holds `TestMultiRowGrid`: 3 hosts x 13 row topologies
  (one to three rows, with and without an image digest, with and without a version) x
  5 own-fact ecosystems x 2 fact shapes x 6 other facts x 3 anchors, each in both fact
  orders and both row orders, plus the own fact alone as a control.
  `multirow_auth_test.go.txt` holds `TestMultiRowAuthGrid`, the same idea through
  `BuildAuthorizedScanImpactFindings` with one target holding both rows and two targets.
- Copy each into the core package as `zz_*_test.go` on a detached worktree and run it
  with `MULTIROW_OUT=<file>`; do it for the base commit and the head, then
  `python3 multirow_compare.py <base> <head>` for rows that moved, and
  `python3 multirow_truth.py <head>` for versioned rows against the own-only control, and
  `python3 multirow_orderdep.py <base> <head>` for shapes order-dependent at the head and
  not at the base.
