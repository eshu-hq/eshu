# CI service image mirrors

Docker Hub's anonymous rate limit can stop hosted jobs while GitHub Actions
initializes PostgreSQL and Neo4j service containers, before Eshu tests run.
`docker-publish.yml` has a manual-only mirror mode that copies three pinned
upstream OCI indexes to dedicated Eshu GHCR packages. It does not rebuild them.

| Upstream index | Eshu GHCR reference | Index digest |
| --- | --- | --- |
| `mirror.gcr.io/library/postgres:18-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` | `ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` | `sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873` |
| `mirror.gcr.io/library/postgres:18.6-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c` | `ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c` | `sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c` |
| `mirror.gcr.io/library/neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` | `ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` | `sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` |

The digest-qualified Neo4j source is an earlier `2026-community` index. The
unqualified tag has since moved; the publisher does not follow it.

The publisher accepts no image or destination input. It checks each source
digest, copies the complete index to its digest-qualified GHCR reference, and
checks the destination digest. It creates no mutable version tags. Repeated or
concurrent copies publish the same content. The manual mirror mode skips every
normal Eshu image, chart, and release job.

## Publication and public-access gate

1. Independently review the exact `main` commit and record its full SHA. Run
   the manual publisher from that commit:

   ```bash
   gh workflow run docker-publish.yml --ref main \
     -f mode=ci-mirrors-publish -f expected_sha=<reviewed-40-character-sha>
   ```

   The publisher rejects a moved branch when `GITHUB_SHA` differs from
   `expected_sha`. It allows only a manual run on `main` in `eshu-hq/eshu`.
   The initial three indexes were published from the reviewed bootstrap branch
   in [run 38000888647](https://github.com/eshu-hq/eshu/actions/runs/38000888647)
   before the `main`-only restriction. Do not reuse that branch for refreshes.

2. New GHCR container packages start private. Before changing visibility, a
   package admin must inspect **every version** in each of the three packages
   in the GitHub organization Packages UI. Confirm that the package contains
   only the approved upstream copies above, with the exact
   index digests. A 403 or 404 from a token without `read:packages` is not
   evidence that a package is absent or safe to expose. Stop if any version
   is unknown; do not delete or expose it as part of this workflow.
3. After that package-wide inspection, an authorized package admin changes
   each package's visibility to Public in its package settings. This is an
   external, irreversible visibility change; the workflow does not do it.
4. Run the anonymous check from `main`:

   ```bash
   gh workflow run docker-publish.yml --ref main \
     -f mode=ci-mirrors-verify-public
   ```

   The job has no package-write permission or GHCR login. Its script uses a
   fresh Docker config and requires each public digest to equal the table.
   Consumers must not switch to these references until this check succeeds
   and an anonymous pull of each digest works.

## Maintenance

Consumers pin the verified index digest. Package deletion and visibility remain
administrator-controlled operations. Retain these untagged index versions and
their referenced content; exclude them from automatic untagged-version cleanup.
For an upstream refresh, review a new source digest, then update the script,
tests, table, and consumer pins together. Publish and verify the new digest
before changing consumers. Package cleanup requires a separate reviewed
retention decision. Do not substitute a moving Docker Hub tag, an unbounded
workflow input, or a cache-only registry reference for the verified GHCR digest.

The shape gate pins the full text of both mirror jobs, permissions and steps,
to `scripts/dev/fixtures/ci-image-mirror-publish-job.txt` and
`scripts/dev/fixtures/ci-image-mirror-verify-public-job.txt`. An added or
changed step needs the same change in its fixture and a security review,
because the publisher job holds package-write permission and a GHCR login.
The registry rows `ci-service-mirror-publish` and
`ci-service-mirror-verify-public` are manual and advisory: no merge waits on
either job.
