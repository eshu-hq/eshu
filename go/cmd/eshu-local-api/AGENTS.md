# AGENTS.md — cmd/eshu-local-api guidance for LLM assistants

1. `wiring.go` — the wiring sequence: how Postgres and the graph driver open,
   how the reader ports compose, and where each handler family mounts. Read it
   before touching any handler or route.
2. `wiring_router.go` — the route table: which routes mount on the API mux and
   which stay wireAPI-only (first-run setup, OIDC/SAML/GitHub login, provider
   configs, sign-in policy, ask, CLI-support). `wiring_router_completeness_test.go`
   fails on any live route without a row.
3. `main.go` — `main`, telemetry bootstrap, signal handling, and shutdown. The
   binary takes its listen address on `ESHU_API_ADDR` and has no `-addr` flag.
4. `CUTOVER_OWNERSHIP.md` lives in this directory (moved here from
   `go/cmd/api/` by the `eshu-hq/eshu#11456` removal with the serving code
   it describes): the ownership map and the `cutover_ownership_test.go`
   pin in this directory read it from here.
5. `README.md` — lifecycle summary and build/test commands.
6. `diagnostic/` — the opt-in status-route timing collector; see its own
   `AGENTS.md` before changes there.

Binary contract: `eshu-local-api` serves NornicDB or an unset backend and
answers `/healthz`. Neo4j sessions use `eshu-core-api`; unknown backends are
refused pre-spawn by the supervisor, not here. Keep the telemetry service
name and the `--version` display name as they are: renaming them is a
behavior change outside the relocation.

A behavior change needs a failing focused regression first. Run
`go test ./cmd/eshu-local-api/...` from `go/` after edits.
