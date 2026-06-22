# Authorization Catalog

The authorization catalog is Eshu's v1 role, grant, and data-class inventory.
It defines the product authorization vocabulary before runtime enforcement
expands across API, MCP, Ask, search, and dashboard paths.

The catalog is source-controlled at `specs/authorization-catalog.v1.yaml` and is
embedded into the generated capability artifact at
`go/internal/capabilitycatalog/data/catalog.generated.json`.

## What It Defines

| Section | Purpose |
| --- | --- |
| `roles` | Built-in product roles and their explicit action/data-class grants. |
| `data_classes` | Closed data-class names and sensitivity levels. |
| `permission_families` | Capability-id prefixes mapped to an action, data classes, scope levels, and default roles. |
| `bootstrap_owner` | First-owner posture: admin plus sensitive-data grants, then delegable roles. |
| `custom_policy` | V1 posture for custom policies. The policy language is deferred, but the model stays extensible. |

Each generated capability entry includes an `authorization` block with the
matched permission family, action, data classes, scope levels, default roles,
and whether the capability carries sensitive data.

## Built-In Roles

| Role | Intended use |
| --- | --- |
| `tenant_admin` | Manages users, memberships, provider settings, roles, grants, tokens, and workspace settings. It does not include sensitive-data grants by default. |
| `developer` | Reads granted repository, source, code graph, documentation, service context, Ask/search, and citation evidence. |
| `operator` | Reads runtime status, freshness, collector posture, service context, and recovery diagnostics. |
| `security_analyst` | Reads sensitive cloud/IaC, secrets/IAM, supply-chain, audit, and security evidence. |
| `auditor` | Reads audit, export, governance, admission, work-item, and status evidence without write power. |
| `sensitive_data_reader` | Delegated sensitive-data visibility without broad tenant administration. |
| `owner` | Bootstrap-capable owner with admin plus sensitive-data grants. Owners can delegate or remove those grants after setup. |

Sensitive-data visibility is separate from tenant administration: granting
`tenant_admin` is not the same as granting `sensitive_data_reader` or `owner`.

## Permission Families

The v1 catalog covers these required families:

| Family | Example mapped capabilities |
| --- | --- |
| `identity_admin` | Planned local/OIDC/SAML user, membership, provider, MFA, and workspace admin surfaces. |
| `roles_grants` | Capability catalog and future effective-permission/grant inspection. |
| `tokens` | Planned personal-token and service-principal token lifecycle. |
| `repository_content` | Code search, content, graph, code quality, call graph, evidence citation, and dependency reads. |
| `service_runtime` | Service/workload context, deployment stories, incidents, runtime topology, Kubernetes, and observability coverage. |
| `cloud_iac` | IaC quality, cloud inventory, drift, import, unmanaged resources, and replatforming. |
| `secrets_iam` | Secrets/IAM trust-chain, privilege posture, access paths, and posture gaps. |
| `supply_chain` | Package, SBOM, image, vulnerability, reachability, security-alert, and CI/CD correlations. |
| `docs_semantic` | Documentation facts/findings and semantic evidence/status. |
| `ask_search` | Ask Eshu, semantic search, citations, reasoning, and narration posture. |
| `operations_status` | Freshness, governance, collector readiness, surface inventory, schema, metrics, and query workflow catalogs. |
| `audit_export` | Admission decisions, work-item evidence, audit/export, and denied-decision inspection. |
| `admin_recovery` | Component extension, recovery, backfill, replay, dead-letter, bootstrap, and break-glass administration. |

Families with live capability prefixes must match at least one capability row.
Planned families may be present without live capability rows so the model covers
the user-management surface before those runtime routes land.

## Verification

The generator enforces catalog drift:

```bash
cd go
go run ./cmd/capability-inventory -mode generate
go run ./cmd/capability-inventory -mode verify
go test ./internal/capabilitycatalog ./cmd/capability-inventory -count=1
```

The real-spec tests fail if a capability lacks authorization metadata, a role or
family references an unknown data class, the bootstrap owner no longer starts
with admin plus sensitive grants, or tenant administration accidentally gains
sensitive-data visibility by default.

## Related

- [Capability Catalog](capability-catalog.md)
- [Hosted Governance Posture](../operate/hosted-governance.md)
- [Hosted Security Posture Gate](../operate/hosted-security-posture.md)
