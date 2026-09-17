# Standalone template collector image: pure-Go binary plus manifest and
# fixture, built reproducibly with -trimpath. Replace the module path and the
# COPY roots when this directory becomes a new repository root.
FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/collector ./cmd/collector

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/collector /usr/local/bin/collector
COPY manifest.yaml /etc/collector/manifest.yaml
COPY testdata/complete.json /var/lib/collector/complete.json
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/collector", "--sdk-stdio"]
