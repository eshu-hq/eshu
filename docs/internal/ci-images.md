# CI image sources and refresh

Required CI checks use fixed image digests. A registry pull failure still fails
its check. The image change does not change check names, required status,
queue selection, backend selection, or release attestations.

## Image boundaries

`specs/ci-images.v1.json` records the upstream source, approved GHCR destination,
index digest, platforms, source records, and notice policy for each mirror.
Its separate `tools` record pins the rebuilt CI tools index, recipe hash,
platform manifests, and package inventory digests consumed by the static gate.
Copy the full upstream index. Do not rebuild, retag a single-platform pull, or
add labels to an upstream mirror. Those acts change the content identity.

The existing PostgreSQL and main Neo4j mirror packages are reused by digest.
The refresh process must not change their tags, visibility, or access settings.
The other mirrors use dedicated `ci-mirror-*` packages. The NornicDB reference
keeps its existing GHCR digest in the separate `runtime_allowlist` ledger field;
it is an existing runtime dependency, not an image published by this mirror job.

The tools image serves the static contract gate matrix. Live Docker stacks run
on the host runner. The tools image contains Ubuntu, Go, and the CLI tools that
the matrix needs. It contains no Eshu checkout, Eshu executable, credentials,
or private build context. See `scripts/ci/tools.README.md` for its recipe,
license paths, package inventory, and local smoke test.

The tools recipe pins direct package versions and binary checksums. Ubuntu
transitive packages can advance at build time. Record the actual inventory for
each build. The consumed image digest freezes that build; the recipe alone is
not a claim that a future rebuild has identical bytes.

## Compose image selection

CI sets `ESHU_CI_IMAGES=1` and all five ledger pins: `POSTGRES_IMAGE`,
`NEO4J_IMAGE`, `ESHU_XX_IMAGE`, `ESHU_GO_IMAGE`, and `ESHU_ALPINE_IMAGE`.
The helper renders the original Compose stack in memory, then writes a minimal
image and build-argument overlay. It does not write the rendered environment
or secret values to disk or logs. The temporary directory has mode 0700 and
the overlay has mode 0600.

Append the overlay as the last Compose file. Use it for the whole stack
lifecycle, including logs, exec, inspection, and down. Remove the overlay only
after teardown. Each invocation owns its temporary path. The child wrapper
checks the prepared input signature before reuse.

Missing pins, pins that differ from the ledger, unknown images, and unknown
Dockerfile identities fail before a build starts. There is no Docker Hub
fallback in CI mode. Normal product Compose files and Dockerfile defaults keep
their original upstream references when CI mode is off. `GATE_STACK_DIR`
changes the explicit stack root; it does not change the helper's trusted ledger.
The Compose helper accepts NornicDB only at the exact repository, tag, and digest
listed in `runtime_allowlist`; an overridden NornicDB image fails preflight.

The PostgreSQL readiness fixture uses `ESHU_LIVE_POSTGRES_IMAGE`. The query-plan
profile uses `ESHU_QUERYPLAN_PROFILE_IMAGE`. These separate consumers must match
their ledger entries. Buildx uses the `CI_BUILDKIT_IMAGE` mirror explicitly so
its builder startup does not add an implicit Docker Hub pull.

## Refresh and proof

1. Read the upstream recipe, license notices, corresponding source records,
   and every platform manifest. Reject foreign or nondistributable layers.
2. Update the ledger in a reviewed branch. Preserve existing runtime versions
   unless the task explicitly includes a version update.
3. Run the publisher regression test. Prepare an exact canonical subset ledger
   for the six dedicated mirror destinations. Use explicit protected source
   and destination authfiles. Destination publication uses `linuxdynasty`.
4. Run `mirror-images.sh publish` with that ledger. Require exit 0 and one
   `COPIED` result per image. A copy result does not prove public access.
5. Set only the new dedicated packages public and link their repository.
   Run `mirror-images.sh verify` with the same ledger. It compares root
   manifests and pulls every child, config, and layer anonymously into OCI.
   Require exit 0 and one `PASS` result per image.
6. Build tools from an empty context for each supported architecture. Add the
   public repository source label and the recipe hash. Run the smoke test and
   the full selected matrix against a separate checkout per test container.
   Fixture tests must not share a mutable checkout.
7. Publish the tools image only after review. Record the registry index digest,
   package inventory, recipe hash, and anonymous pull proof. A local image
   configuration ID is not a registry index digest.
8. Update workflow pins. Prove Compose configuration parity, real startup and
   teardown, mirrored-base builds, and the selected blocking gates locally.
   Then follow the repository review, attestation, push, and merge queue rules.

Upstream mirror layers retain their license and copyright files unchanged.
Source records identify the known upstream projects and source releases. This
record is not a claim of a complete component-by-component legal audit.

## Failure diagnosis

CI step logs identify the selected image digest and the failed operation.
Distinguish source-copy failures, public-access failures, local overlay
validation failures, and runtime health failures. Fix the failing boundary;
do not mark a required check advisory to pass a registry outage. The change
adds no application runtime or OTEL instrumentation surface.
