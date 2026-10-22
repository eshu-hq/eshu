# AGENTS.md — internal/github/app guidance

## Scope

This package mints GitHub App installation tokens for collector runtimes. It
may sign JWTs, call the allowlisted mint endpoint, and classify mint faults.
It must never store tokens, log credentials, or define metrics.

## Rules

- Keep error strings bounded: statuses and classes only, never credentials,
  keys, tokens, JWTs, URLs, or response bodies.
- Keep the retry budget explicit and the mint client bounded (~15s) and
  context-cancellable.
- Preserve the `mint` operation label and the class vocabulary so runtimes
  keep counting the same signals after the extraction.
- Mirror any change to the transient classifier or rate-limit rules against
  the repository-discovery REST package semantics, and prove parity through
  the unmodified `repo/git` mint tests.
- Add or update `MintInstallationToken` fake-server tests for new fault
  behavior, including the credential canary.
