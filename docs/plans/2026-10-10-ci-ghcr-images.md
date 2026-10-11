# CI GHCR Images Implementation Plan

> Execute in the assigned `ci/queue-efficiency` worktree. Publication requires the image design ruling and the proof below.

**Goal:** Remove Docker Hub image pulls from blocking CI without removing checks.

**Architecture:** Preserve upstream image indexes and their children in dedicated GHCR mirrors. Use explicit CI image overrides for hosted live jobs and a prebuilt tools image for the static matrix. Keep product defaults, check names, selection, and aggregates.

**Tech Stack:** OCI indexes, Skopeo, Docker Buildx, Compose, GitHub Actions, Bash.

## 1. Source ledger and bootstrap

Record exact upstream digests, platforms, source recipes, licenses, notices, and destination names. Include both Postgres and both Neo4j versions. Exclude unexecuted image families. Verify no foreign layers. Copy with `--all --preserve-digests`; compare raw manifests and children. Publish only after source audit and local tools candidate proof. Make dedicated packages public and link the repository. Test digest pulls with an empty credential configuration.

## 2. Tools candidate

Create `scripts/ci/tools.Dockerfile` and a package inventory. Use an Ubuntu 24.04 digest, Go 1.26.9, verified pinned downloads, and the tools required by each static matrix command. Do not include Eshu source, binaries, credentials, or local configuration. Add `scripts/ci/test-tools-image.sh` before implementation; record RED then GREEN. Test the current checkout, Bash, writable caches, CGO/race compilation, fixture commands, and runner runtime compatibility. Test nested Docker mounts if a command needs them.

## 3. CI consumers

Add CI-only Postgres and Dockerfile base override seams. Set digest-pinned GHCR runtime images in workflows, preserve the query-plan Neo4j version and NornicDB digest, and set Buildx driver image explicitly. Run static jobs in the published tools container with explicit Bash. Never fall back to Hub after a pull failure.

## 4. Regression and execution proof

Seed failures for unpinned images, missing mirrors, implicit BuildKit pulls, mismatched update inputs, and failed pulls. Exercise production consumers rather than only fixture lists. Prove cold anonymous pulls, representative Compose health and cleanup, and a real Buildx build using mirrored bases and builder. Measure comparable tools setup/startup before making a timing claim.

## 5. Promotion and merge

Rebase on the latest base. Run focused proof and docs checks. Rewrite PR claims for the final scope. Complete independent final-diff review and self-review; capture attestation, run `make pre-push`, verify receipt, and push the feature branch. Use linuxdynasty for PR actions. Wait for stable complete checks in two reads and all review threads resolved. Run pre-enqueue validation and obtain final arbiter approval. Enqueue through GitHub and watch until merge is verified. Update the local dashboard and close the watcher automation only after closeout.
