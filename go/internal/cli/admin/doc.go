// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package admin holds the logic behind the `eshu admin` command tree: the
// fact work-item and reindex API calls, and bootstrap admin credential
// retrieval and reset.
//
// Two groups of callers exist. The work-item side (Reindex, TuningReport,
// ListWorkItems, ListDecisions, Replay, DeadLetter, Skip, Backfill,
// ListReplayEvents) decides which admin endpoint to call and what request
// body to send, then returns the decoded response for the caller to render.
// The credential side (RetrieveInitialCredential, ResetInitialCredential)
// opens or regenerates the sealed bootstrap credential envelope and appends
// a durable governance-audit event for the attempt, success or failure.
//
// The package touches no process state. Its non-test source imports neither
// "os" nor "os/exec": it reads no environment variable, executes no binary,
// creates or writes no file, and prints nothing. It opens no socket of its
// own either — HTTP goes through the Client interface it is handed, and
// Postgres statements go through the pgstorage.ExecQueryer it is handed, so
// base URL, credential, timeout, proxy handling, and DSN are all decided by
// the caller. ESHU_POSTGRES_DSN and ESHU_AUTH_SECRET_ENC_KEY appear here
// only inside operator-facing error text; go/cmd/eshu is what actually reads
// them. Two _test.go files do read ESHU_POSTGRES_DSN, to skip the
// real-Postgres proofs when no DSN is set.
//
// go/cmd/eshu/admin.go and go/cmd/eshu/admin_initial_credential.go are the
// cobra wrappers that resolve all of that process state — flags, the shared
// *APIClient, the Postgres connection, the data-encryption keyring, and the
// output stream — and pass it in. That split is mechanical rather than a
// design choice specific to this package: go/cmd/eshu is `package main`, so
// nothing can import it, and any symbol reading cobra flags, process
// environment, or the exit-code contract has to stay there (issue #6059,
// epic #6053).
//
// Callers must treat BootstrapCredentialPayload as live secret material.
// Both credential functions return plaintext — a password and an MFA
// recovery code — that nothing in this package logs, prints, or persists.
package admin
