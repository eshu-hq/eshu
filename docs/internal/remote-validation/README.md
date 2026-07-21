# docs/internal/remote-validation

Evidence artifacts for `remote_validation` proof-IDs cited in
`specs/capability-matrix.v1.yaml` and `specs/capability-matrix/*.yaml`
(#5407, PR 2 of #5336).

## Convention

A matrix row's `production` profile may cite a `remote_validation` proof, for
example:

```yaml
production: {status: supported, verification: [{remote_validation: prod-code-search-exact}]}
```

That `prod-code-search-exact` ref must resolve to a committed file at:

```
docs/internal/remote-validation/prod-code-search-exact.md
```

`go/internal/capabilitycatalog/remote_validation.go`
(`CheckRemoteValidationArtifacts`) enforces this with `os.Stat`, run by
`scripts/verify-remote-validation-artifacts.sh` (CI gate
`remote-validation-artifacts` in `specs/ci-gates.v1.yaml`). A ref that
resolves to no file here fails the gate unless it is listed in the burn-down
baseline, `specs/remote-validation-baseline.txt`.

## Writing an artifact

An evidence file should record what was actually run against a real
deployed-services environment: the command or workflow, the environment
(sanitized — no credentials, hostnames, or account IDs), the date, and the
observed pass/fail outcome. It does not need to be a specific format; it
needs to be enough for a reviewer to judge whether the claim it backs is
real. Once the file exists, remove the ref from
`specs/remote-validation-baseline.txt` (or run
`bash scripts/verify-remote-validation-artifacts.sh -update`, which drops it
automatically because `remoteValidationArtifactExists` now returns true).

## Current state

This directory is empty as of #5407: every `remote_validation` ref currently
cited in the matrix (115 as of this writing, including
`prod-component-extension-inventory` and
`prod-component-extension-diagnostics`, the pair #5336 originally flagged)
predates this gate and has no committed evidence file yet. All of them are
tracked as burn-down debt in `specs/remote-validation-baseline.txt` rather
than downgraded, per that issue's acceptance criteria (state whether the
component_extensions pair is baselined or downgraded — baselined is the
explicit default here). Closing an entry requires either committing a real
artifact or an explicit, separately-reviewed decision to downgrade the
capability's claimed status.
