# Proof image for the Scorecard component-extension remote Compose harness
# (#2126/#1923). It layers the reference Scorecard collector binary, the
# component package, and the install+enable+run entrypoint on top of the main
# eshu runtime image (which already builds the eshu CLI and the
# collector-component-extension worker with the CGO/tree-sitter toolchain the
# eshu module requires). Build the eshu base first, then this image:
#
#   docker build -t eshu:local -f Dockerfile .
#   docker build --build-arg ESHU_IMAGE=eshu:local \
#     -f examples/collector-extensions/scorecard/Dockerfile \
#     -t eshu-scorecard-component-proof:local .
#
# The scorecard collector is pure Go (no CGO), so its build stage is cheap and
# can be verified on its own:
#   docker build --target scorecard-build -f examples/collector-extensions/scorecard/Dockerfile .
ARG ESHU_IMAGE=eshu:local
FROM golang:1.26-alpine AS scorecard-build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY sdk ./sdk
COPY examples/collector-extensions/scorecard ./examples/collector-extensions/scorecard
RUN cd examples/collector-extensions/scorecard \
    && go build -trimpath -o /out/scorecard-collector ./cmd/scorecard-collector

FROM ${ESHU_IMAGE}
USER root
COPY --from=scorecard-build /out/scorecard-collector /usr/local/bin/scorecard-collector
COPY examples/collector-extensions/scorecard /opt/scorecard
COPY examples/collector-extensions/scorecard/scripts/proof-entrypoint.sh /usr/local/bin/proof-entrypoint.sh
RUN mkdir -p /component-home && chown -R eshu:eshu /component-home /opt/scorecard
ENV ESHU_COMPONENT_HOME=/component-home
USER eshu
ENTRYPOINT ["/usr/local/bin/proof-entrypoint.sh"]
