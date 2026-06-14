# Proof image for the PagerDuty component-extension remote Compose harness.
# It layers the reference PagerDuty collector binary, component package, and
# install script on top of the main eshu runtime image. Build the eshu base
# first, then this image:
#
#   docker build -t eshu:local -f Dockerfile .
#   docker build --build-arg ESHU_IMAGE=eshu:local \
#     -f examples/collector-extensions/pagerduty/Dockerfile \
#     -t eshu-pagerduty-component-proof:local .
ARG ESHU_IMAGE=eshu:local
FROM golang:1.26-alpine AS pagerduty-build
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY sdk ./sdk
COPY examples/collector-extensions/pagerduty ./examples/collector-extensions/pagerduty
RUN cd examples/collector-extensions/pagerduty \
    && go build -trimpath -o /out/pagerduty-reference ./cmd/pagerduty-reference

FROM ${ESHU_IMAGE}
USER root
RUN apk add --no-cache ripgrep
COPY --from=pagerduty-build /out/pagerduty-reference /usr/local/bin/pagerduty-reference
COPY examples/collector-extensions/pagerduty /opt/pagerduty
COPY examples/collector-extensions/pagerduty/scripts/proof-install.sh /usr/local/bin/proof-install.sh
RUN chmod +x /usr/local/bin/proof-install.sh /usr/local/bin/pagerduty-reference \
    && mkdir -p /component-home && chown -R eshu:eshu /component-home /opt/pagerduty
ENV ESHU_COMPONENT_HOME=/component-home
USER eshu
