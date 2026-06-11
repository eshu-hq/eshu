# Worker image for the Scorecard OCI-adapter remote Compose proof
# (#1980/#1923). Unlike the process-adapter proof image (Dockerfile), this image
# does NOT bundle the scorecard collector binary: under the OCI adapter the
# collector is the separate digest-pinned artifact (Dockerfile.oci) that the
# worker launches with `docker run`. This image therefore only adds the docker
# CLI plus the install helper and the OCI manifest to the eshu base, so the
# component-extension worker can resolve the digest-pinned artifact through the
# mounted container runtime.
#
# Build (after the eshu base image):
#   docker build -t eshu:local -f Dockerfile .
#   docker build --build-arg ESHU_IMAGE=eshu:local \
#     -f examples/collector-extensions/scorecard/oci.worker.Dockerfile \
#     -t eshu-scorecard-oci-worker:local .
ARG ESHU_IMAGE=eshu:local
FROM ${ESHU_IMAGE}
USER root
# docker-cli speaks to the host daemon over the mounted socket; it never runs a
# nested daemon. The artifact it launches stays isolated (uid 65532, no network,
# read-only root, all caps dropped) regardless of this worker's privileges.
RUN apk add --no-cache docker-cli
COPY examples/collector-extensions/scorecard/manifest.oci.yaml /opt/scorecard/manifest.oci.yaml
COPY examples/collector-extensions/scorecard/scripts/proof-install-oci.sh /usr/local/bin/proof-install-oci.sh
RUN chmod +x /usr/local/bin/proof-install-oci.sh \
    && mkdir -p /data/.eshu/components && chown -R eshu:eshu /data/.eshu
ENV ESHU_COMPONENT_HOME=/data/.eshu/components
# The worker runs as root in this proof only to read the mounted docker socket;
# the launched extension is still confined by the OCI adapter's isolation flags.
USER root
ENTRYPOINT ["/usr/local/bin/eshu-collector-component-extension"]
