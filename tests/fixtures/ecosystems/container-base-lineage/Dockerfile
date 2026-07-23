# Base-image lineage fixture (#5460).
#
# The builder stage intentionally uses a tag: its base must NOT become runtime
# lineage, because a builder stage's OS packages do not ship in the final image.
# Only the final stage's digest-pinned base is the runtime image's ancestor.
FROM golang:1.24 AS builder
WORKDIR /src
COPY src/ ./
RUN go build -o /out/app ./...

FROM ghcr.io/eshu-hq/base-runtime@sha256:ba5e0000000000000000000000000000000000000000000000000000000000ba
COPY --from=builder /out/app /usr/local/bin/app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/app"]
