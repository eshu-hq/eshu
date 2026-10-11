# API status diagnostic

This package owns the opt-in `ESHU_STATUS_DIAGNOSTIC_REPORT` collector. The API
registers its SDK span processor only when the output path is set. Its outer
middleware selects exact `GET /api/v0/status/ingesters/repository` requests,
creates a server-generated decimal ID and trace root, and adds
`X-Eshu-Diagnostic-ID` to the response. Its inner middleware marks arrival at
the API dispatcher after authentication and before the writer checkpoint.

The collector retains a bounded set of request and allowlisted span records. It
copies only closed role, stage, outcome, phase and status-query labels. It
never copies SQL, URL, credentials, payload, events, or raw errors. Shutdown
flushes a schema v1 JSON report atomically at mode 0600. The `complete` field
is false on capacity overflow, missing required spans, unfinished requests or
spans, or provider flush failure. A successful status response requires a
writer checkpoint, status snapshot, and reader stage. The report is private
diagnostic evidence, not an API response contract.
