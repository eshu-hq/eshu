# Authz Token Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the #3461 token lifecycle bridge and close #3460 only where current Ask/search/API/MCP authorization propagation lacks proof.

**Architecture:** Keep existing shared-token and secret-file scoped-token behavior intact. Add a Postgres-backed generated-token resolver that reads hash-only token metadata, validates active tenant/workspace and subject state, derives grants from active role assignments, and returns the same bounded auth context consumed by existing API/MCP route gates.

**Tech Stack:** Go, Postgres SQL, `internal/query` auth middleware, `cmd/api` wiring, MkDocs.

---

### Task 1: Generated Token Resolution

**Files:**
- Modify: `go/internal/storage/postgres/scoped_api_tokens.go`
- Modify: `go/internal/storage/postgres/scoped_api_tokens_schema.go`
- Test: `go/internal/storage/postgres/scoped_api_tokens_test.go`

- [ ] Write a failing storage test proving a personal token resolves through active `identity_token_metadata`, active user membership roles, active role grants, active tenant/workspace, active token status, expiry, and revocation predicates.
- [ ] Run `cd go && go test ./internal/storage/postgres -run 'TestScopedAPITokenStore.*Generated|TestScopedAPITokenStore.*Resolve' -count=1` and confirm the generated-token test fails because no generated-token resolver exists.
- [ ] Implement the minimal storage query and result type to return token class, subject hash, role IDs, policy revision, allowed scope IDs, and allowed repository IDs without raw token material.
- [ ] Add the service-principal variant: active service principal plus active service-principal role assignments and grants.
- [ ] Run the focused storage tests until green.

### Task 2: API/MCP Resolver Wiring

**Files:**
- Modify: `go/cmd/api/wiring.go`
- Modify: `go/cmd/api/README.md`
- Modify: `go/cmd/api/doc.go`
- Test: `go/cmd/api/wiring_test.go`

- [ ] Write a failing wiring test proving `ESHU_IDENTITY_TOKENS_ENABLED=true` composes the generated-token resolver before falling back to `ESHU_SCOPED_TOKENS_FILE` and then shared-token compatibility.
- [ ] Implement the narrow adapter that maps storage token auth rows into `query.AuthContext`.
- [ ] Run `cd go && go test ./cmd/api -run 'Test.*Token|TestWireAPI' -count=1`.

### Task 3: Ask/Search Authorization Gap Check

**Files:**
- Modify only if a real uncovered gap is found: `go/internal/query/*`, `go/internal/mcp/*`, or `go/internal/ask/engine/*`
- Test: focused existing auth tests for Ask, semantic search, and MCP dispatch.

- [ ] Re-run current #3460 proof tests to establish whether Ask, semantic search, and MCP dispatch already enforce scoped authorization.
- [ ] If a gap is found, write the failing regression first and fix only that route family.
- [ ] If no gap is found, update docs/issue evidence instead of adding duplicate code.

### Task 4: Evidence And Hygiene

**Files:**
- Modify: `docs/public/operate/user-management-runbook.md`
- Modify: `go/internal/storage/postgres/evidence-notes.md`
- Modify: package docs only if contracts changed.

- [ ] Add durable `No-Regression Evidence:` and `No-Observability-Change:` notes for generated-token resolution.
- [ ] Run focused Go tests, package docs gate if Go package contracts changed, docs build if public docs changed, and `git diff --check`.
- [ ] Perform full self-review before staging, committing, pushing, or opening the PR.
