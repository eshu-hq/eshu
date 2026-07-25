# Cross-scope CI-evidenced base-image lineage fixture (issue #5810).
#
# Unlike container-base-lineage (#5460, rc-167), this fixture's CHILD image
# has NO Kubernetes manifest, NO content_entity reference, and NO OCI config
# source label anywhere in this repository -- its ONLY build-provenance
# evidence anywhere in the corpus is a ci.run/ci.artifact pair recorded by the
# ci_cd_run collector in its OWN, separate CI-run scope
# (testdata/cassettes/cicdrun/supply-chain-demo.json), naming this
# repository's canonical id as the digest's builder. Before #5810, the
# repository-scoped container_image_identity intent this Dockerfile's `file`
# fact enqueues could never see that cross-scope CI evidence, so
# BuildProvenanceRepositoryIDs never resolved for the child and no
# DERIVED_FROM edge existed alongside rc-167's Dockerfile+OCI-label pair (see
# rc-170, testdata/golden/e2e-20repo-snapshot.json).
#
# The builder stage's tag base is never observed as a manifest, mirroring
# container-base-lineage: a builder stage's OS packages do not ship in the
# final image and must not appear as runtime lineage.
#
# This fixture deliberately carries NO source files and NO Kubernetes
# manifest, so the child digest's only path to a decision is the OCI registry
# manifest (identity resolution) plus the cross-scope CI bridge (build
# provenance) -- exactly the shape #5810 fixes.
FROM golang:1.24 AS builder
WORKDIR /src
RUN go build -o /out/app ./...

FROM ghcr.io/acme/ci-lineage-base@sha256:c10000000000000000000000000000000000000000000000000000000010ba5e
COPY --from=builder /out/app /usr/local/bin/app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/app"]
