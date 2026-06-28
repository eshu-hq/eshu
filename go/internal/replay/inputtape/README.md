# replay/inputtape

Record and replay HTTP traffic at the `http.RoundTripper` boundary so HTTP-backed
collectors run credential-free and offline.

## Purpose

The input tape is the *input* side of the deterministic-replay framework. The
cassette flavor (`replay/cassette`) records collector **output** (fact
envelopes); the input tape records collector **input** (the raw HTTP responses a
collector receives from a provider). Recording at the transport boundary keeps
the collector code under test unchanged — only its HTTP client is swapped — so a
replayed run exercises the real parsing, normalization, and fact-emission code
against recorded provider responses.

## Modes

| Mode | Constructor | Behavior |
| --- | --- | --- |
| Record | `New(Config)` | Proxies each request to a real transport, returns the live response, and accumulates a redacted request→response pair. |
| Replay | `NewReplayer(Tape, Config)` | Serves responses from the tape keyed by request; **any unmatched request is a hard error** (`ErrUnmatchedRequest`). No network I/O. |

Replay never falls through to the network. An unrecorded request fails loudly
rather than silently issuing a live call — this is the load-bearing safety
property of the replay framework.

## Request matching

Each request reduces to a deterministic SHA-256 key over:

- HTTP method
- URL path
- the **sorted** query
- the canonicalized request body (when present)

The key is independent of header order, query order, and — for JSON bodies —
object key order. Two parameter classes adjust the key:

- **Secret parameters** (`Authorization`, `Cookie`, `X-Api-Key`, `X-Amz-Security-Token`,
  …; query `token`, `access_token`, `signature`, …) are redacted to `<redacted>`
  before the key is computed and before the request is stored. A credential-free
  replay request still matches, and the tape never carries a live secret.
  Extend with `Config.RedactHeaders` / `Config.RedactQueryParams`.
- **Volatile query parameters** (per-run timestamps, nonces) normalize to a
  `<volatile>` sentinel **in the key only** (the stored request keeps the real
  value for review). A replay request stamped at a different instant still
  matches. Declare them with `Config.VolatileQueryParams`. Most observability
  collectors stamp `time.Now()` into `start`/`end`; without naming those, their
  requests never match on replay.

The same `Config` redaction/volatile sets must be supplied at record and replay
time so a request reduces to the same key on both sides.

## Tape format

A tape is a versioned JSON document written canonically (sorted object keys,
interactions sorted by `request_key`, secrets redacted) via the shared
`replay.Canonicalize` core. It is stable, reviewable in diffs, and byte-identical
when re-recorded from equivalent traffic.

```json
{
  "schema_version": "1",
  "collector": "loki",
  "interactions": [
    {
      "request_key": "<sha256-hex>",
      "request": {
        "method": "GET",
        "path": "/loki/api/v1/labels",
        "query": { "end": ["1700000000"] },
        "header": { "Authorization": ["<redacted>"] }
      },
      "response": {
        "status_code": 200,
        "header": { "Content-Type": ["application/json"] },
        "body": { "present": true, "encoding": "json", "data": "{ ... }" }
      }
    }
  ]
}
```

Request and response bodies are stored by encoding: `json` (canonicalized text,
so the tape stays reviewable and the key is key-order independent), `text`
(verbatim UTF-8, e.g. a YAML rules body), or `base64` (opaque/binary).

## Wiring

The `RoundTripper` is an `http.RoundTripper`, so it installs as an
`*http.Client.Transport`:

```go
recorder := inputtape.New(inputtape.Config{VolatileQueryParams: []string{"end"}})
client, _ := loki.NewHTTPClient(loki.HTTPClientConfig{
    BaseURL: baseURL,
    Client:  &http.Client{Transport: recorder},
})
// ... run collection against the live endpoint ...
_ = inputtape.WriteTape("testdata/inputtapes/loki/example.json", recorder.Tape("loki"))
```

Replay loads the tape and serves it with no server running:

```go
tape, _ := inputtape.LoadTape("testdata/inputtapes/loki/example.json")
replayer, _ := inputtape.NewReplayer(tape, inputtape.Config{VolatileQueryParams: []string{"end"}})
client, _ := loki.NewHTTPClient(loki.HTTPClientConfig{
    BaseURL: baseURL,
    Client:  &http.Client{Transport: replayer},
})
```

Collector seams that accept the tripper:

- `loki`, `grafana`, `prometheusmimir`, `tempo` — `HTTPClientConfig.Client`
  (an `sdk.HTTPDoer`; `*http.Client` satisfies it).
- `pagerduty`, `jira`, `confluence` — a `*http.Client` field.
- SDK collectors (`awscloud`, `gcpcloud`, `azurecloud`) — the SDK accepts a
  custom HTTP client. For AWS, set `aws.Config.HTTPClient` (or
  `sts.Options.HTTPClient`) to `&http.Client{Transport: tripper}`; see
  `sdk_demo_test.go`.

## No-Regression Evidence

No-Regression Evidence: the replay path performs no network I/O and acquires no
credentials — `RoundTrip` in `ModeReplay` resolves the request key and reads a
map under a briefly-held mutex, returning `ErrUnmatchedRequest` on a miss. The
record path holds the mutex only across the in-memory map update, never across
the wrapped network round trip, so concurrent requests proceed in parallel.
Verified by `go test ./internal/replay/inputtape/... -race -count=1` (includes
`TestConcurrentRecordIsRaceFree`).

## No-Observability-Change

No-Observability-Change: the package emits no metrics, spans, or log lines.
Collector-level telemetry records normally because the tripper is wired through
the collector's existing HTTP client; the tape is transparent to it.
