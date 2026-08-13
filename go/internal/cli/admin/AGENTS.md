# AGENTS.md — internal/cli/admin guidance for LLM assistants

## Read first

1. `go/internal/cli/admin/README.md` — what this package owns, and the exact
   process-state boundary it holds (the enumerated list of what it does and
   does not touch)
2. `go/internal/cli/admin/doc.go` — the godoc contract
3. `go/cmd/eshu/admin.go` — the cobra wrapper: command tree, flags, flag
   reading, `apiClientFromCmd`, `printJSON`
4. `go/cmd/eshu/admin_initial_credential.go` — the credential wrapper:
   `openAdminCredentialDB` (reads `ESHU_POSTGRES_DSN`),
   `secretcrypto.KeyringFromEnv`, and the printing of the returned plaintext
5. `go/internal/storage/postgres/identity_bootstrap_credential.go` —
   `SelectBootstrapCredential` and `ResetBootstrapCredential`, the SQL this
   package's credential side drives

## Invariants this package enforces

- **No process state.** The non-test source imports neither `os` nor
  `os/exec`. It reads no environment variable, executes no binary, creates
  or writes no file, opens no socket of its own, and prints nothing. HTTP
  goes through the `Client` interface and Postgres through the
  `pgstorage.ExecQueryer`, both supplied by the caller. If a change here
  needs a flag, an env var, or stdout, that part belongs in `go/cmd/eshu`,
  not here — this is the rule the whole extraction rests on (issue #6059,
  epic #6053).
  - `credential_test.go` and `credential_audit_test.go` are the deliberate
    exception: they read `ESHU_POSTGRES_DSN` to skip their real-Postgres
    proofs, and `credential_test.go` calls `t.Setenv` for
    `ESHU_POSTGRES_DSN` and `ESHU_AUTH_SECRET_ENC_KEY`.
    `credential_invariant_test.go` reads no environment and needs no
    database.
- **Every credential operation is audited.** The audit helpers are
  unexported and called inside `RetrieveInitialCredential` /
  `ResetInitialCredential`, so no caller can retrieve or reset without
  recording the attempt. Both outcomes are recorded — a failed retrieval
  (already consumed, wrong key) is as security-relevant as a successful one.
  `credential_invariant_test.go` is the guard: it drives both exported
  functions against a fake `pgstorage.ExecQueryer` and asserts the audit
  `INSERT` reached the connection, success and failure. Deleting either
  audit call makes those four tests fail by name. Do not weaken them into
  assertions on a stubbed appender — going through the real
  `pgstorage.GovernanceAuditStore` is what keeps
  `governanceaudit.NormalizeEvent` in the path, and a rejected event is
  indistinguishable from a missing one because `Append`'s error is
  discarded.
- **Audit events carry a key id and nothing else identifying.** No
  plaintext password, recovery code, or sealed ciphertext ever reaches an
  event. `TestAuditBootstrapCredentialEventsNoPlaintextLeak` guards this.
- **Audit append is fire-and-forget.** An audit-store failure never fails
  the credential operation, matching every other governance-audit call site
  in this codebase.
- **Errors from `Client` are returned verbatim.** They already carry the
  operator's context (`API error 404: <body>` for an HTTP status,
  `request failed: Post "http://host/path": dial tcp …` for a transport
  failure) and `cmd/eshu` prints them straight to stderr. Wrapping would
  prepend a second prefix to what the operator already reads, not add
  context, so each return carries its own `//nolint:wrapcheck`.
  - The reason those nine need a `//nolint` at all is the `Client`
    interface. `wrapcheck` reports an error returned from an *interface*
    method; the identical call in `package main` went to the concrete
    same-package `(*APIClient).Post` and was never reported. Running
    `wrapcheck` over the pre-extraction tree with no exemption of any kind
    gives 364 issues under `go/cmd/eshu` and zero on any admin file, and
    restoring `go/cmd/*` in `ignore-package-globs` leaves all nine here.
    So do not "fix" a new one by editing `go/.golangci.yml` — an exemption
    would silence it for a reason that is not the cause and would not
    generalize. Either keep the verbatim return with its `//nolint`, or
    decide the error genuinely needs context and wrap it.

## Common changes and how to scope them

- **Add a new `admin` subcommand** → add the request-shaping function (and
  its `…Input` struct, if it takes more than one argument) here, then wire
  the `cobra.Command`, its flags, and the `printJSON` call in
  `go/cmd/eshu/admin.go`. Why: the endpoint and body are the decision worth
  testing; the flag plumbing is not.
- **Change a request body** → change it here only. The wrapper passes flag
  values through and does not know the wire shape.
- **Change what an operator sees** → change `go/cmd/eshu`. The one
  exception is an error string constructed here (for example `Replay`'s
  `--reason` message); those are operator-facing too, so treat them as
  output contract.
- **Add an environment variable, a file write, or an exec** → stop. Put it
  in the wrapper and pass the resolved value in. If you genuinely cannot,
  update `README.md`, `doc.go`, and this file together, because all three
  currently assert the opposite.

## Failure modes and how to debug

- Symptom: `eshu admin facts replay` exits non-zero with
  `--reason is required …` → cause: `Replay` rejected a blank `Reason`
  before any HTTP call. Expected; pass `--reason`.
- Symptom: `eshu admin initial-credential` reports
  `cannot decrypt the sealed bootstrap credential` → cause: the configured
  `ESHU_AUTH_SECRET_ENC_KEY` differs from the key that sealed the envelope.
  The failed-retrieval audit event still carries the envelope's own key id,
  so the governance-audit table shows which key was needed.
- Symptom: `eshu admin reset-initial-credential` reports
  `no bootstrap credential exists for this deployment` → cause:
  `ResetBootstrapCredential` returned `ErrBootstrapCredentialNotFound`; the
  admin was seeded from `ESHU_ADMIN_USERNAME`/`PASSWORD`, or
  `ESHU_AUTH_BOOTSTRAP_MODE` is sso-only/disabled.
- Symptom: `eshu admin reset-initial-credential` reports
  `cannot recover the original username` → cause: `--username` was omitted
  (its default is empty, which is the normal case) and the fallback that
  reads the username off the prior envelope also failed, because that
  credential was consumed, already reset, or sealed under a different key.
  Pass `--username`.
- Symptom: the credential tests skip → cause: `ESHU_POSTGRES_DSN` is unset.
  `credential_invariant_test.go` and the other unit tests still run; the
  round-trip and audit-persistence proofs need a real Postgres.

## Anti-patterns specific to this package

- **Wrapping a `Client` error to "add context."** It changes what the
  operator reads on stderr. If more context is genuinely needed, add it in
  the wrapper where the output contract lives.
- **Printing from this package.** Return a value or a struct; the wrapper
  renders it. `fmt.Print*`, `os.Stdout`, and `os.Stderr` have no place here.
- **Exporting an audit helper or a reason-code constant.** They are
  unexported so a caller cannot perform a credential operation without its
  audit event.
- **Logging or persisting `BootstrapCredentialPayload`.** It is live
  plaintext: a password and a working MFA recovery code.
- **Asserting only against `fakeCLIAuditAppender`.** It captures whatever
  `Append` receives and never runs `governanceaudit.NormalizeEvent`, so it
  cannot catch an event the real store would silently reject.
  `TestAuditBootstrapCredentialEventsPersistToRealGovernanceAuditStore` is
  the real-Postgres complement — keep it in step.

## What NOT to change without an ADR

- **Going direct to Postgres rather than through the API.** Both credential
  commands deliberately bypass the API: an unauthenticated HTTP endpoint
  would be new attack surface, and a shared-API-key approach has no key to
  check on a fresh stack before the first admin exists. See `credential.go`'s
  file comment.
- **`BootstrapCredentialPayload`'s JSON field tags.** They must stay
  byte-for-byte identical to `go/cmd/api/seed_initial_admin.go`'s sealing
  side. That file is `package main` and cannot be imported, so nothing but
  review keeps the two in step.
- **The four audit reason codes.** They are the durable record epic #4962
  requires; renaming one orphans the history already written under the old
  value.
