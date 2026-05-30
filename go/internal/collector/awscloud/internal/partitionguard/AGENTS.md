# AGENTS - awscloud/internal/partitionguard guidance

## Read First

1. `README.md` - the ARN-partition contract, how the guard distinguishes
   synthesis from parsing, and what it does and does not catch.
2. `doc.go` - godoc contract for the exported `ScanForHardcodedPartitions`.
3. `../../partition.go` - the shared `PartitionForRegion` / `PartitionForBoundary`
   / `PartitionFromARN` helpers scanners must use instead of hardcoding.
4. `../relguard/AGENTS.md` - the sibling static-guard precedent this package
   mirrors (same AST-walk-the-live-tree shape).

## Invariants

- Keep the walk source-based (`go/parser`, no type checking). It must stay fast
  and free of a `golang.org/x/tools/go/packages` dependency, like relguard.
- The guard flags a literal beginning with `arn:aws:` ONLY in a synthesis
  context (`+` operand, printf format, or an identifier bound to such a literal).
  Do NOT broaden it to flag `arn:aws:` literals in parse calls
  (`strings.HasPrefix`/`Contains`/`TrimPrefix`/`Index`) - those are legitimate
  ARN inspection, not synthesis, and flagging them would force ugly rewrites.
- The commercial prefix is exactly `arn:aws:` (note the trailing colon).
  GovCloud/China ARNs begin `arn:aws-`, so a correctly derived ARN literal is
  `"arn:"` + a helper call - never matched. Do not change the prefix to
  `arn:aws` (without the colon) or it would match `arn:aws-us-gov` too.

## When the guard fails

The failure lists each `file:line` and the offending literal. Fix the scanner,
never the guard: replace the hardcoded `arn:aws:` with a derived partition using
the region/boundary/source-ARN already in scope. Only touch this package to
improve detection precision, and add a fixture-style regression test when you do.

## Verification

```
cd go && go test ./internal/collector/awscloud/internal/partitionguard/... -count=1
```
