# AWS to Azure Re-platforming Demo

This is a runnable, end-to-end demo of Eshu's re-platforming wedge: turn an
observed AWS estate into a bounded, provider-neutral migration packet with
`compose_replatforming_plan`, then hand that packet to an LLM (Claude Code) to
generate `azurerm_*` Terraform and import blocks. A human reviews and applies the
result outside Eshu.

It builds on the [Hosted Replatforming Runbook](replatforming-runbook.md), which
covers the AWS observe → compare → plan stages in depth. Read that first; this
demo adds the AWS → Azure narrative and an explicit, copy-pasteable local path.

## What this demo shows

1. Ingest Terraform state and AWS posture into the Eshu graph.
2. Let the reducer materialize AWS runtime drift / IaC management findings.
3. Call `compose_replatforming_plan` to get one bounded migration packet:
   per-item source state, safety gate, owner candidates, ready/refused Terraform
   import candidates, migration waves, and blast-radius groups.
4. Hand the packet to Claude Code to generate `azurerm_*` modules and import
   blocks for the Azure target.
5. Review and apply externally.

## What Eshu does and does not do here

The boundaries in the [Hosted Replatforming Runbook](replatforming-runbook.md#what-eshu-does-not-do)
hold for this demo without exception. In particular:

- Eshu **observes, compares, and plans**. It never runs Terraform, imports
  resources, mutates cloud state, or writes repositories — on AWS or Azure.
- `compose_replatforming_plan` is **read-only** and capped at the `derived`
  truth level. Every item carries a `safety_gate`; refused import candidates are
  returned with refusal reasons, never silently omitted.
- The **LLM generates** the Azure Terraform. Eshu supplies bounded, truth-labeled
  evidence; it does not author `azurerm_*` HCL itself. A human reviews the
  generated modules and runs `terraform plan`/`apply` outside Eshu.
- Azure is the **migration target**, not an ingestion source for this demo. The
  packet is provider-neutral, so no Azure collection is required to compose it.
  (The Azure cloud collector exists as a gated claim-driven runtime; see
  [Azure Cloud Collector Contract](../reference/azure-cloud-collector-contract.md).
  It is not needed to plan an AWS → Azure migration.)

## Prerequisites and what is runnable offline

`compose_replatforming_plan` requires the `local_authoritative` profile or
higher; lower profiles return `501 unsupported_capability` (see
`specs/capability-matrix/replatforming.v1.yaml`). Both the local-binaries path
and Docker Compose provide `local_authoritative`.

| Demo input | Runnable offline? | What it needs |
| --- | --- | --- |
| Terraform state ingestion | Yes | A local `.tfstate` file (fixtures included). |
| Terraform config ingestion | Yes | A Git repo with the matching `.tf` config. |
| AWS posture ingestion | No | A **read-only** AWS account; there is no offline AWS collector. |
| Reducer drift findings | Yes (once facts exist) | The reducer joins the above by ARN automatically. |
| `compose_replatforming_plan` | Yes (once findings exist) | `local_authoritative`+ stack. |
| `azurerm_*` generation | Yes | Claude Code (or any MCP-aware assistant). |

The AWS posture layer is the one step that requires a real account. Use a
**read-only** sandbox account with no production data. A follow-up to make the
AWS posture step fully offline (a fixture/replay AWS collector mode, issue #3063)
is tracked separately so this demo can later run from a clean clone with zero
cloud credentials.

## Step 1 — Bring up a local `local_authoritative` stack

Docker Compose is the simplest path for this demo because it serves the HTTP API
on `localhost:8080` and MCP on `localhost:8081` out of the box. Set an API key up
front so you can authenticate the read calls in Step 5 (Compose enables auth and
auto-generates a key otherwise):

```bash
git clone https://github.com/eshu-hq/eshu.git
cd eshu

export ESHU_API_KEY="demo-api-key"   # used as the API bearer token below
docker compose up --build
```

The local-binaries path is good for fast iteration, but note that
`eshu graph start` does **not** start the HTTP API — its read surface is MCP
(`eshu mcp start`). If you take this path, drive Step 5 through the MCP tool
rather than the `curl` API call, or run `eshu-api` separately:

```bash
./scripts/install-local-binaries.sh
export PATH="$(go env GOPATH)/bin:$PATH"

eshu graph start --workspace-root "$PWD"   # graph + ingester + reducer + MCP, no HTTP API
```

See [Local Binaries](../run-locally/local-binaries.md) (including
[What still needs an API](../run-locally/local-binaries.md#what-still-needs-an-api))
and [Docker Compose](../run-locally/docker-compose.md) for details.

## Step 2 — Ingest Terraform state and config

Index the repository that holds your Terraform configuration so Eshu can resolve
config ownership (see [Index Repositories](../use/index-repositories.md)), then
run the Terraform-state collector against the state file(s) for the estate you
are migrating. The collector requires a redaction key and ruleset version so
sensitive state values are fingerprinted, never stored raw:

```bash
export ESHU_TFSTATE_REDACTION_KEY="demo-redaction-key"
export ESHU_TFSTATE_REDACTION_RULESET_VERSION="v1"
```

Configure one `terraform_state` collector instance through
`ESHU_COLLECTOR_INSTANCES_JSON` pointing at your state source. See
[Terraform State Collector](../services/collector-terraform-state.md) for the
full instance shape and local/S3 source options. Repository-checked-in fixture
states and matching Terraform configs are available under
`tests/fixtures/` for a self-contained run.

Without Terraform state, every observed cloud resource looks unmanaged, so this
step is what makes the demo's "managed vs unmanaged" distinction meaningful.

## Step 3 — Ingest AWS posture (read-only account)

Configure one `aws` collector instance with read-only credentials and a bounded
service/region allowlist. See
[AWS Cloud Collector](../services/collector-aws-cloud.md) and the
[AWS Cloud Coverage Matrix](../services/collector-aws-cloud-coverage-matrix.md).

Scope the scan tightly for a demo — a single region and a handful of services
(for example `s3`, `lambda`, `ec2`) keeps the run fast and the packet readable.
Use a read-only IAM role; the collector only reads.

## Step 4 — Confirm the reducer materialized findings

The reducer joins observed AWS resources, Terraform state, and owner-resolved
Terraform config by ARN, then emits durable AWS runtime drift / IaC management
findings. Confirm readiness with the proof mindset in
[Collector And Reducer Readiness](../reference/collector-reducer-readiness.md).
Treat `building`, `stale`, or `unavailable` freshness as not-yet-answerable, not
as "nothing to migrate".

You can sanity-check the inventory first with the observe-stage tools from the
[Hosted Replatforming Runbook](replatforming-runbook.md#stage-1-observe-inventory):
`find_unmanaged_resources` and `list_aws_runtime_drift_findings`.

## Step 5 — Compose the migration packet

Call `compose_replatforming_plan` (HTTP route `POST /api/v0/replatforming/plans`).
Scope it to an account (and optionally a region or service):

```bash
curl -sS -X POST http://localhost:8080/api/v0/replatforming/plans \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${ESHU_API_KEY}" \
  -d '{
        "scope_kind": "account",
        "account_id": "123456789012",
        "limit": 100
      }' | jq '.'
```

The Compose API requires a bearer token (`ESHU_AUTO_GENERATE_API_KEY` is on by
default); use the `ESHU_API_KEY` you exported in Step 1. The MCP path does not
need this header. With an MCP client (Claude Code), the equivalent prompt is:

> "Compose a re-platforming plan for AWS account `123456789012`. Summarize the
> migration waves, the blast-radius groups, the ready import count, and the
> refused import count with each refusal reason."

The response is one bounded `ReplatformingPlan` (contract `v1`):

- **`items`** — each observed resource with a provider-neutral `source_state`
  (`exact`, `derived`, `partial`, `ambiguous`, `stale`, `unavailable`,
  `unsupported`, `unknown`, or `rejected`), the AWS `management_status`,
  `confidence`, `owner_candidates`, `source_layers` (declared IaC / applied
  state / observed runtime), a `safety_gate`, and an `import_candidate` that is
  either `ready` (with a Terraform `import_block`) or `refused` (with reasons).
- **`waves`** — deterministic migration phases, ordered early-safe → review →
  blocked-last, derived only from evidence the findings already carry.
- **`blast_radius_groups`** — items grouped by dependency and risk severity.
- **`non_goals`** and **`limitations`** — the fixed read-only boundaries carried
  in every response.

The plan never fabricates dependencies and never promotes an observation to
ownership without reducer evidence. Items whose safety gate rejects promotion
surface as `source_state: rejected` regardless of provider evidence.

## Step 6 — Generate `azurerm_*` Terraform with the LLM

Hand the packet to Claude Code and ask it to translate the **early-safe wave**
first. The packet is the bridge: it tells the LLM exactly which resources are
import-ready, what they are, and how risky each move is, so the model maps
AWS-shaped resources to Azure-shaped modules.

A note on import blocks: a packet item's `import_candidate.import_block` is an
**AWS** Terraform import block — it exists to bring the unmanaged AWS resource
under Terraform management on the source side. Azure is a greenfield target in
this demo (there are no pre-existing Azure resources to import), so the model
should emit `azurerm_*` **resource definitions** for the migration, not Azure
import blocks. Generate Azure import blocks only later, against real resource
IDs, once the Azure resources actually exist.

> "Here is the re-platforming plan. For every item in wave 1 (early-safe) whose
> `import_candidate.status` is `ready`, generate the equivalent `azurerm_*`
> Terraform resource definition for a new Azure deployment. Keep one module per
> blast-radius group. Do not emit Azure import blocks — these are new Azure
> resources. For refused items, list the refusal reason and do not emit
> Terraform. Do not invent dependencies that are not in the packet."

Typical AWS → Azure target mappings the model will produce (review each — these
are starting points, not guarantees):

| AWS resource | Azure target (illustrative) |
| --- | --- |
| `aws_s3_bucket` | `azurerm_storage_account` + `azurerm_storage_container` |
| `aws_lambda_function` | `azurerm_linux_function_app` / `azurerm_function_app` |
| `aws_dynamodb_table` | `azurerm_cosmosdb_table` |
| `aws_rds_instance` | `azurerm_postgresql_flexible_server` / `azurerm_mssql_server` |
| `aws_sqs_queue` | `azurerm_servicebus_queue` |

Work wave by wave. The migration waves and blast-radius groups exist so the
model (and the reviewer) sequence the low-risk, import-ready resources before the
review-gated ones.

## Step 7 — Review and apply externally

As in the [Hosted Replatforming Runbook](replatforming-runbook.md#stage-4-review-the-human-gate),
a human owns the gate and the apply:

1. Inspect each item's `safety_gate`, `source_layers`, `missing_evidence`, and
   the truth envelope (`truth.level` caps at `derived`; check
   `truth.freshness.state`).
2. Review the LLM-generated `azurerm_*` modules against your real Azure target —
   naming, regions, SKUs, and identity are operator decisions the packet does not
   make.
3. Run `terraform plan` against Azure and confirm an expected diff before
   `apply`. Eshu never runs Terraform; the import and apply happen in your own
   Terraform workflow.

## Producing the recorded walkthrough

The launch deliverable is a 10–15 minute screen recording of this runbook. It is
an operator task (it needs a real read-only AWS sandbox account and an Azure
target subscription) and is not produced by Eshu. Record the steps above in
order, narrating the safety gate, the migration waves, and the human review gate,
and link the recording from the launch page once published.

## Safety and truth recap

- Every Eshu tool here is read-only, account- or scope-bounded, and capped at
  `derived` truth under `local_authoritative`+.
- The migration packet is provider-neutral; no Azure ingestion is required to
  plan an AWS → Azure migration.
- The LLM generates Azure Terraform; a human reviews and applies it. Eshu
  observes, compares, and plans only — on both clouds.
