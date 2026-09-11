# call — agent instructions (issue #6061, #6609)

This directory is the code-call family. The handler and the projection
runner deliberately stay in the reducer root; read `doc.go` before moving
anything else in or out.

## Invariants

- Never import the parent reducer package. If a change here needs root
  logic, the logic is either shared (hoist it to the shared tier) or the
  caller belongs in root (keep it there and call down into this package).
  `buildSymbolRuntimeIntentRows` is the worked example: it calls root-only
  refresh-fence helpers, so the handler that uses it stayed in root.
- Behavior-preserving by default: the same rows, the same partition keys,
  the same evidence_source strings. `PartitionKeyVersion`,
  `RepoRefreshEvidenceSource`, `EvidenceSource`, and
  `PythonMetaclassEvidenceSource` are persisted in shared-intent payloads
  and partition keys, and the root runner reads the evidence sources back;
  changing one is a data migration, not a refactor.
- `EntityIndex` is built once per pass and read-only afterward. Do not add
  mutation methods; the symbol-runtime builders in root share the same
  instance.
- Test doubles stay package-local. Root tests that need a helper from here
  keep their own twin (see `reducerTestRelativePath` in the parent's
  `handles_route_java_test.go`).
- Prove changes with `go test ./internal/reducer/... -count=1` (recursive),
  plus the resolution goldens here (`resolution_goldens_*_test.go`) and the
  B-7 golden corpus when row shapes could change.
