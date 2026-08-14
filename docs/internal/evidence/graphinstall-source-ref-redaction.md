# graph install: keep the source reference's credential out of the manifest

## What was wrong

`eshu graph install --from https://user:PW@host/build.tar.gz` copied the
reference verbatim into `Result.SourcePath`, which reaches three sinks:

- the `0600` `manifest.json` written by `atomicWriteFile`
- the `sha256 mismatch for %q` error
- `printJSON(result)` in `graph_install_cmd.go:57`, which puts it on stdout

Reproduced before the fix, sentinels synthetic:

```
Result.SourcePath = http://eshu-leak-probe-user:eshu-leak-probe-password-NOT-REAL@127.0.0.1:50039/...
verify nornicdb source binary "http://…user:password@…?token=…": permission denied
```

A second, pre-existing bug came out of the same read: archive detection ran
against the raw reference including the query string, so every presigned
S3/GCS/CDN URL was classified as a bare binary and `Install` tried to
`fork/exec` the tarball. `looksLikeArchive("https://host/build.tar.gz?token=abc")`
returned `false`.

## Where the fix went

At the assignment point in `inspectInstallSource`, not at serialisation. Three
existing sinks in two packages would each need a patch, and the exported field
would still carry the secret for the next consumer. The manifest is write-only —
nothing in the repo parses it back — so a display-only form costs nothing.

The whole query is dropped here rather than filtered per parameter, because
`source_sha256` already identifies the exact bytes and presigned URLs carry the
credential in `X-Amz-Signature`, which is not credential-shaped by name. Name
filtering is the wrong tool at this sink. `redactEndpoint` in `cmd/eshu` makes
the opposite choice for the opposite reason, and each file points at the other.

## No-Regression Evidence:

Baseline `origin/main` at `fc462effc`, same binary shape, same inputs. The change
adds one `url.Parse` plus a `strings` scan per install invocation — a one-shot
CLI path that already performs a network download and a SHA-256 over the payload.
No queue, lease, batch, worker, or Cypher path is touched; no statement text
changes. Terminal state unchanged: `go test ./internal/cli/graphinstall/... -count=1`
exit 0, `-race` exit 0, before and after. Row and queue counts are not applicable —
this path writes one manifest file and exits.

## Observability Evidence:

Operator-visible output changes shape deliberately: `source_path` in the manifest,
the `sha256 mismatch` error, and the `printJSON` stdout form now carry a redacted
reference instead of a credential-bearing one. The redacted form keeps scheme,
host, port, and path, so an operator can still identify which artifact was
installed and from where. Nothing is silently dropped — a redacted reference is
visibly redacted rather than absent.

Stated limit, asserted in the drift test rather than claimed: masking is by
parameter name, so `?sig=` and `?X-Amz-Signature=` are not credential-shaped and
survive in the endpoints where per-parameter filtering applies. Closing that
needs value-content judgement, which this repo bans.
