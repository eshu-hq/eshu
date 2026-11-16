# #10739: satellite seed admission platform parameter, performance and observability

Issue: eshu-hq/eshu#10739. Seed admission is scoped to the host's `GOOS/GOARCH`,
and two admission tests read the host platform, so they failed on any Mac. The
admission platform is now a parameter. Production passes
`seedPlatformSource()`, a package variable that defaults to `hostSeedPlatform()`,
which returns the same `GOOS + "/" + GOARCH` string the loaders built inline
before. Tests pass the pinned `linux/amd64`.

This note covers the hot files the performance gate lists:
`go/cmd/collector-component-extension/activation_select.go`, `config.go`,
`seed_platform.go`, and `service.go`.

## Performance Evidence

No benchmark was run, because no runtime path changed. The argument, with the
code it rests on:

- The platform string is built once per configuration load. In the extension
  that is the single `loadRuntimeConfigObserved(getenv, grantObserver,
  seedPlatformSource())` call in `service.go` when the worker service is built.
  In the coordinator it is the single `loadConfigForPlatform(getenv, observer,
  seedPlatformSource())` call inside `LoadConfigObserved`. Neither sits in a
  claim loop, a queue path, or a per-fact path. The source adds one indirect
  call per load.
- Before the change the same two-string concatenation ran inline at the
  `VerifySatelliteSeededArtifact` call. The change moves it to a helper and
  passes it down one extra argument. Work per load is unchanged.
- `selectActivation` and `collectorKindForManifest` moved to
  `activation_select.go` with the platform parameter threaded through. The
  bodies are otherwise identical, so their cost is unchanged.
- No lock, channel, goroutine, query, or allocation-per-item changed.

No-Regression Evidence: baseline and after are the same work. One string
concatenation per configuration load, no hot-path call sites, no change to the
number of admission checks. `go test -race` on both packages passed with
`go test -count=1 -race ./cmd/collector-component-extension ./internal/coordinator`
(exit 0, run by the independent reviewer). Backend and input shape do not
apply: the change touches process-start configuration only.

## Observability

No-Observability-Change: the change emits no metric, span, or log of its own. The
admission refusal for an unpinned platform still returns the same error
(`declares no seed image for platform "<platform>": refusing admission without
the pinned platform image`), and the existing component-extension result and
status-record metrics report it as before. An operator at 3 AM sees the same
refusal text with the same platform value, because the default source returns the
host platform exactly as before. The telemetry-coverage rows for the two new files
are in `docs/public/observability/telemetry-coverage-collectors.md`.

## Why this is safe

The `loadConfigForPlatform`, `hostSeedPlatform`, `seedPlatformSource`, and
extension `loadRuntimeConfigObserved` seams are unexported, and the exported
entry points (`LoadConfig`, `LoadConfigObserved`) keep their signatures, so no
production caller can supply a platform other than the one the source returns.
Production never reassigns `seedPlatformSource`.

Two guards cover a hardcoded platform. Tests pin `hostSeedPlatform()` to
`GOOS/GOARCH`: that catches an empty value inside the helper on every host, and a
literal `linux/amd64` on any host that is not linux/amd64 (a linux/arm64 host
catches it too, because the registry pins both linux platforms). Serial tests
stub `seedPlatformSource` to `plan9/mips`, a platform no seed pin lists, and
require the loader to refuse it. They run against `LoadConfig` in the
coordinator and against `buildClaimedService`, the call the extension worker
makes at startup, so a production line that ignores the source fails on every
host, including linux/amd64. A third stub test covers the `loadRuntimeConfig`
wrapper, which only tests call. What no test can see on a linux/amd64 host is a
literal `linux/amd64` inside the default `hostSeedPlatform()` body, because it
returns the same string there.
