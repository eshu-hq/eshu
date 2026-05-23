# jfrog OCI Agent Guidance

## Read First

1. `README.md` and `doc.go` for JFrog OCI boundaries.
2. `adapter.go` for Artifactory URL and credential mapping.
3. `adapter_test.go` and `live_test.go` for fake and opt-in live coverage.
4. `../README.md` for OCI registry evidence boundaries.

## Local Rules

- Keep JFrog Docker/OCI repository support separate from JFrog package feeds.
- Do not commit private hostnames, repository keys, user names, tokens,
  passwords, image repository names, account-only runbooks, or private topology.
- Keep credentials in HTTP client config only; never log, metric-label, fact,
  document, or mention them in PR text.
- Provider code prepares calls; Distribution wire behavior belongs to the
  `distribution` package.
- Live tests must skip unless explicit environment variables opt in.

## Change Rules

- Add Artifactory-specific repository topology mapping here.
- Add package-feed behavior under `packageregistry`, not here.
- Do not make JFrog metadata canonical workload, package, or source ownership
  truth.
- Do not flatten local, remote, and virtual repository topology into one
  unlabelled registry.
