# Status route Terraform-state selection (#7009)

## Scope and truth

Statements #24 (last serial per state locator) and #25 (recent Terraform
warnings) of the status snapshot run under the `terraform_state` read label.
Only three code paths read `Report.TerraformState`:

- `statusReportToMapWithAWS` (`GET /api/v0/status/pipeline`)
- the index payload (`GET /api/v0/status/index`, `GET /api/v0/index-status`,
  MCP `get_index_status`)
- `status.RenderJSON` (runtime `/admin/status?format=json` and
  `cmd/admin-status`)

Health evaluation does not read it. The following surfaces never read it, so
they now request a snapshot that omits both statements. Every other section
request is unchanged.

| Surface | Selection after this change |
| --- | --- |
| `/api/v0/status/operations` | full minus Terraform |
| `/api/v0/status/hosted-readiness` (MCP `get_hosted_readiness`) | full minus Terraform |
| `/api/v0/status/operator-control-plane` (MCP `get_operator_control_plane`) | full minus Terraform |
| `/api/v0/status/freshness-causality` (MCP `get_freshness_causality`) | full minus Terraform |
| `/api/v0/status/collectors`, `/api/v0/collectors` (MCP `list_collectors`) | full minus Terraform |
| `/api/v0/status/collector-readiness`, `/api/v0/collector-readiness` | full minus Terraform |
| `/api/v0/status/governance`, `/api/v0/status/answer-narration` | full minus Terraform |
| `/api/v0/status/ingesters`, `/api/v0/ingesters` (MCP `list_ingesters`) | no fact aggregates, minus Terraform |
| Runtime `/metrics` (`serveStatusMetrics`) | full minus Terraform, then narrowed again by [7009-status-summary-metrics-scrape.md](7009-status-summary-metrics-scrape.md) |

Repository ingester detail and the live evidence bundle already skipped these
reads. Pipeline, index, runtime `/admin/status` (JSON and text), and
`cmd/admin-status` keep both reads and still fail
when either one fails. No API or MCP payload changes. A failure confined to the
Terraform reads no longer fails a skipping route. A failure in a retained read
still does. The statement text, the repeatable-read transaction, the timeouts,
and the active-generation semantics are unchanged. Governance and answer
narration still read every other section. Narrowing them further is a separate
change.

## Proof

- `TestTerraformFreeStatusRoutesSkipTerraformEvidence` covers 14 route paths.
  For each one it asserts the exact selection requested and one snapshot read.
  It also asserts byte-identical response bodies between a reader that always
  returns 3 Terraform rows and one that omits them on a skip. Before the
  change, the selection assertion failed on the 12 newly skipping paths. The 2
  ingester detail paths already passed.
- `TestTerraformRenderingStatusRoutesKeepTerraformEvidence` covers pipeline
  and both index paths. These routes still request the reads and render the
  seeded rows. It also plants a violation: the same reader with the Terraform
  rows removed. The violation changes each body, so the equality probe can
  fail.
- `TestStatusRoutesTerraformStatementInventory` runs each route through the
  production Postgres `StatusStore` over a recording queryer. Skipping routes
  issue 0 Terraform statements and return 200 when the warning statement
  fails. Pipeline and index issue 2 and return 500 on that failure.
- `TestStatusMetricsSkipsTerraformEvidence` checks runtime `/metrics`: one
  filtered read, the full-minus-Terraform selection, and byte-identical output.
- `TestStatusRoutesTerraformSelectionLive` runs on disposable PostgreSQL 18
  with the full bootstrap schema. It seeds 1 tfstate serial and 4 warnings: 3
  tfstate warnings across an active and a superseded generation, and 1 Git
  `unresolved_backend_expression`. Each skipping route returns the same bytes
  as a forced full read (5 Terraform rows versus 0). Pipeline and index carry
  the seeded rows. Forcing the skip onto them changes the body. The proof runs
  in the `live-postgres-readiness` runner.
- Mutation: reverting any one route's selection to the full selection fails
  the selection test, the statement inventory, and (for HTTP routes) the live
  test. The executor report records each mutation.

## Performance Evidence:

The full status snapshot has 26 statements. In the QA reader plan
inventory (run `c5544da49fcd5c68`, plain EXPLAIN on PostgreSQL 18.3),
statement #25 has a planner total cost of 1,579,838.23. The next most
expensive statement, #4 `active_work_summary`, costs 62,529.82. These are
planner cost units, not time. An earlier QA reader sample in
[7009-status-detail-selection.md](7009-status-detail-selection.md) measured
the recent-warning statement at 360.839 ms for 92 rows. That is a single
three-day-old sample. Local fixtures measured the statement at about 416 ms
to 597 ms. The fixtures differ in hardware, data, and storage state, so those
totals are not comparable. This change removes statements #24 and #25 from the
routes listed above.

NOT_CHECKED: endpoint p95 on the QA environment for any route, whether and how often
Prometheus scrapes runtime `/metrics`, cold-read latency, and concurrent-user
capacity. No endpoint speedup is claimed. Pipeline and index status still pay
for #25. The planned partial index on `terraform_state_warning` facts is a
separate change.

## No-Observability-Change:

The existing `eshu_dp_status_snapshot_read_duration_seconds` histogram and its
bounded read labels stay as they are. A skipping route emits no
`terraform_state` phase sample. Pipeline, index, runtime `/admin/status`, and
`cmd/admin-status` keep
that sample and propagate its errors. The `status_snapshot` span and the
database query summary labels still attribute each retained statement. No
metric, span, or log key is added.
