# CLI Admin

## Purpose

`admin` holds the logic behind the `eshu admin` command tree. Two unrelated
groups of operations share the directory because they share the command
prefix, not because they share a mechanism:

- **Work-item administration** — which admin HTTP endpoint each subcommand
  calls, what request body it sends, and the `--reason` / idempotency-key
  rules on replay.
- **Bootstrap credential** — opening the sealed one-time admin credential
  envelope (`eshu admin initial-credential`) and regenerating it
  (`eshu admin reset-initial-credential`), each with a durable
  governance-audit event.

## Ownership boundary

This package owns *what to ask for*. It does not own process wiring: reading
cobra flags, building the `*APIClient`, opening Postgres, resolving the
data-encryption keyring, printing, or mapping errors to exit codes. All of
that stays in `go/cmd/eshu/admin.go` and
`go/cmd/eshu/admin_initial_credential.go`, because `go/cmd/eshu` is
`package main` and nothing can import it.

What that boundary means concretely, verified against the non-test source:

- **Environment variables read: none.** No non-test file imports `os`. The
  names that do appear in the source, none of them read here, are
  `ESHU_AUTH_SECRET_ENC_KEY`, `ESHU_AUTH_BOOTSTRAP_MODE`, and
  `ESHU_ADMIN_USERNAME`/`PASSWORD` in operator-facing error strings, and
  `ESHU_POSTGRES_DSN` in comments only.
  `go/cmd/eshu/admin_initial_credential.go` reads `ESHU_POSTGRES_DSN`
  directly and hands `os.Getenv` to `secretcrypto.KeyringFromEnv`, which is
  what reaches the encryption-key variables. The same holds transitively for the call paths this
  package reaches — `query.IdentityHash`, `secretcrypto`'s `Seal`/`Open`/
  `EnvelopeKeyID`, `governanceaudit`, and the `pgstorage`
  `IdentitySubjectStore`/`GovernanceAuditStore` methods used here contain no
  environment read either.
- **Binaries executed: none.** No `os/exec` import, so `PATH` is never
  consulted.
- **Files written or read: none.** Nothing here creates, opens, or writes a
  file, and nothing calls `os.MkdirTemp`/`os.CreateTemp`, so `TMPDIR` is
  never consulted. (`keyring.Open` decrypts an envelope; it is not a file
  open.)
- **`Client` errors are returned verbatim.** They already read as operator
  guidance — `API error 404: <body>` for an HTTP status, or
  `request failed: Post "http://host/path": dial tcp …` for a transport
  failure — and `cmd/eshu` prints them straight to stderr. These returns lost
  `go/.golangci.yml`'s `go/cmd/*` `wrapcheck` exemption when they left
  `package main`, so each carries a `//nolint:wrapcheck` naming that reason
  rather than a wrap that would print the context twice.
- **Network calls: none of its own.** HTTP requests go through the `Client`
  interface the caller supplies; Postgres statements go through the
  `pgstorage.ExecQueryer` the caller supplies. Base URL, API key, timeout,
  TLS, and proxy behavior (`HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`, honored by
  `http.DefaultTransport`) are all properties of `cmd/eshu`'s `*APIClient`,
  decided there.
- **Printing: none.** Functions return values and errors; the wrapper
  renders them.
- **Randomness:** `crypto/rand` backs the replay idempotency key, the
  replacement password and recovery code, and the replacement MFA factor id.
  `bcrypt.GenerateFromPassword` draws its salt from the same source.

Test files are the one exception and are deliberately out of that boundary:
`credential_test.go` and `credential_audit_test.go` read
`ESHU_POSTGRES_DSN` to decide whether to skip their real-Postgres proofs,
and `credential_test.go` also calls `t.Setenv` for `ESHU_POSTGRES_DSN` and
`ESHU_AUTH_SECRET_ENC_KEY`.

## Exported surface

Work-item side, all taking a `Client` and returning the decoded response
body as `any`:

| Function | Input struct | Endpoint |
| --- | --- | --- |
| `Reindex` | `ReindexInput` | `POST /api/v0/admin/reindex` |
| `TuningReport` | — | `GET /api/v0/admin/shared-projection/tuning-report` |
| `ListWorkItems` | `ListWorkItemsInput` | `POST /api/v0/admin/work-items/query` |
| `ListDecisions` | `ListDecisionsInput` | `POST /api/v0/admin/decisions/query` |
| `Replay` | `ReplayInput` | `POST /api/v0/admin/replay` |
| `DeadLetter` | `DeadLetterInput` | `POST /api/v0/admin/dead-letter` |
| `Skip` | `SkipInput` | `POST /api/v0/admin/skip` |
| `Backfill` | `BackfillInput` | `POST /api/v0/admin/backfill` |
| `ListReplayEvents` | `limit int` | `POST /api/v0/admin/replay-events/query` |

Credential side:

- `RetrieveInitialCredential`, `ResetInitialCredential` — take an open
  `pgstorage.ExecQueryer` and a resolved `*secretcrypto.Keyring`, return a
  `BootstrapCredentialPayload`
- `BootstrapCredentialPayload` — the plaintext bundle (username, password,
  recovery code) sealed inside the credential envelope

Shared:

- `Client` — the two-method (`Get`, `Post`) view of `cmd/eshu`'s
  `*APIClient`, declared here at the point of use so this package never
  depends on the CLI's process-level client type

The audit helpers and their four reason codes are deliberately unexported:
they are reached only through `RetrieveInitialCredential` and
`ResetInitialCredential`, so an admin credential operation cannot happen
without its audit event.

See `doc.go` for the godoc-rendered contract.

## Dependencies

- `internal/storage/postgres` — `ExecQueryer`, `IdentitySubjectStore`,
  `GovernanceAuditStore`, the bootstrap tenant/workspace constants, and
  `ErrBootstrapCredentialNotFound`
- `internal/secretcrypto` — `Keyring.Seal`/`Open`, `EnvelopeKeyID`,
  `ErrDecrypt`
- `internal/query` — `IdentityHash` and the `GovernanceAuditAppender`
  interface
- `internal/governanceaudit` — the audit event type and its bounded
  decision/actor/scope enums
- `golang.org/x/crypto/bcrypt` — password hashing on reset

Consumed by `go/cmd/eshu` only.

## Telemetry

None. No metrics, spans, or logs are emitted here; these are foreground CLI
operations whose outcome is the command's own output and exit code. The
governance-audit events the credential functions append are durable
compliance rows, not operator telemetry, and are appended fire-and-forget:
an audit failure never fails the credential operation.

## Gotchas / invariants

- **`Replay` refuses a blank `Reason` before any HTTP call**, and its error
  names the `--reason` flag rather than the struct field, because it reaches
  an operator typing a command. Changing that string changes operator-facing
  stderr.
- **A blank `ReplayInput.IdempotencyKey` is filled in, not left empty.** A
  fresh random key per invocation keeps a retried single invocation safe
  without letting two separate invocations collide.
- **Empty string fields are omitted from request bodies; numeric and boolean
  fields are always sent.** An unset filter must not narrow a query, but
  `limit` and `force` carry the CLI's flag defaults.
- **A reset always installs a NEW MFA factor row** rather than reusing the
  old one, so a login racing the reset can never read a factor whose hash
  has not been committed.
- **A reset never touches a TOTP factor** enrolled after bootstrap;
  `ResetBootstrapCredential`'s revocation is scoped to
  `factor_kind='recovery_code'`. The real-Postgres round-trip test asserts
  that against a live row.
- **Audit events carry a key id, never plaintext.** The signatures take only
  a `keyID`, so widening them is the change a reviewer should question.
- **`BootstrapCredentialPayload` is live secret material.** Callers print it
  once and never persist it.

## Related docs

- `go/cmd/eshu/README.md` — the CLI binary and its subcommand groups
- `docs/public/reference/http-api.md` — the admin endpoints this package
  calls
