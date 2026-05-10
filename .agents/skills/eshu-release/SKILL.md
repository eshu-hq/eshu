---
name: eshu-release
description: |
  Eshu release, versioning, and open-source deployment pipeline.
  Use when releasing Eshu, bumping the Helm chart, publishing Docker images or
  CLI binaries, checking version mismatches, or coordinating image tag, chart
  version, appVersion, GHCR, OCI, and GitHub Release work.
---

# Eshu Release And Deployment Pipeline

Use this skill for release work that belongs in the open-source Eshu repository
and its public artifacts. Do not put non-public registry URLs, repository
names, company-specific org names, hostnames, IPs, or machine-local paths into
docs, commits, PRs, or reusable scripts.

## Repo Facts

| Attribute | Value |
| --- | --- |
| Repository | `eshu-hq/eshu` |
| Helm chart | `deploy/helm/eshu` |
| Docker image | `ghcr.io/eshu-hq/eshu` by default from `github.repository` |
| Helm OCI registry | `oci://ghcr.io/eshu-hq/charts` by default |
| CLI binaries | GitHub Releases from `v*` tags |

## Release Integrity

Treat releases as a chain of evidence:

1. Confirm the exact source commit.
2. Use a worktree or feature branch; never push directly to `main`.
3. Update `deploy/helm/eshu/Chart.yaml` intentionally:
   - `version` for Helm chart package changes.
   - `appVersion` for the Eshu application version, usually `vX.Y.Z`.
4. Run the smallest validation that proves the touched surface.
5. Push a branch and open a PR.
6. Merge through the normal review path.
7. Tag the intended commit with `vX.Y.Z` only after the release commit is known.
8. Verify GitHub Actions produced the intended image, chart, and binaries.
9. Validate any public rollout evidence that belongs in the repository.

Do not claim release completion until every public artifact points at the
intended version or digest.

## Open-Source Release Workflow

From a clean main checkout:

```bash
git worktree add ../eshu-release-vX.Y.Z -b chore/release-vX.Y.Z main
cd ../eshu-release-vX.Y.Z
```

Edit `deploy/helm/eshu/Chart.yaml`:

```yaml
version: X.Y.Z
appVersion: "vX.Y.Z"
```

Validate:

```bash
cd go
go test ./cmd/eshu ./cmd/api ./cmd/mcp-server -count=1
cd ..
helm lint deploy/helm/eshu
git diff --check
```

Commit and publish a PR:

```bash
git add deploy/helm/eshu/Chart.yaml
git commit -m "chore(release): prepare vX.Y.Z"
git push origin chore/release-vX.Y.Z
gh pr create --title "chore(release): prepare vX.Y.Z"
```

After merge, tag the exact release commit:

```bash
git fetch origin
git checkout main
git pull --ff-only
git rev-parse HEAD
git tag vX.Y.Z
git push origin vX.Y.Z
```

The tag triggers:

- Docker publish through `.github/workflows/docker-publish.yml`.
- Helm package push from `deploy/helm/eshu`.
- CLI binary release from `.github/workflows/build.yml` with
  `eshu-linux-x86_64` and `eshu-macos-arm64`.

## Helm-Only Change

Use this when Helm templates, `values.yaml`, or `values.schema.json` change but
the application version does not.

1. Bump only `deploy/helm/eshu/Chart.yaml` `version`.
2. Leave `appVersion` unchanged.
3. Run:

```bash
helm lint deploy/helm/eshu
git diff --check
```

Open a PR from a branch or worktree. Do not push directly to `main`.

## Verification Checklist

Report concrete evidence:

- source commit SHA
- release tag
- `deploy/helm/eshu/Chart.yaml` `version` and `appVersion`
- Docker image tag and digest
- Helm chart OCI location and version
- CLI release artifact names
- public rollout health checks, dashboards, logs, or sync state when applicable

## Gotchas

- `appVersion` carries the Eshu application version; chart `version` tracks
  Helm packaging.
- GitHub Actions build Docker metadata from `github.repository`, so confirm the
  repository owner before quoting GHCR locations.
- Tag builds produce release binaries; branch pushes do not create a GitHub
  Release.
- Keep non-public deployment procedures in their own restricted repositories or
  user-local notes, not in this open-source repository.
