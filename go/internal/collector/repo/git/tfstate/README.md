# tfstate

## Purpose

`go/internal/collector/repo/git/tfstate` owns Terraform backend-expression warnings from repository HCL.

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

`git` drives the repository snapshot and the fact stream. It calls into this
package during emission; this package never calls back into `git`. Anything
both sides need lives in `go/internal/collector/repo/git/model`.

## Exported surface

- `TerraformStateBackendExpressionWarningFactCount` — the pre-count for the
  generation estimate.
- `EmitTerraformStateBackendExpressionWarnings` — emission through the shared
  writer.

## Notes

The name is `tfstate`, not `tfstate`: `collector/terraformstate` already
exists and this package imports it for backend-config parsing.

`tfstate_candidate.go` stayed in git. It logs through
`NativeRepositorySnapshotter`, so moving it here would close a cycle back into
git.

## Verification

```bash
cd go && go test ./internal/collector/repo/git/... -count=1
```

Changes to emitted facts also need the golden-corpus gate (B-7) and the B-12
snapshot; see `docs/public/reference/local-testing/golden-corpus-gate.md`.
