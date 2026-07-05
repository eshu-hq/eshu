# securityalert/v1

Schema-version-1 typed payload struct for the `security_alert` fact family
(Contract System v1 §3.1). This is a standalone package under the public
`sdk/go/factschema` contracts module; it must not import Eshu internals.

## Fact kinds

| Fact kind | Struct | Wired consumer(s) |
| --- | --- | --- |
| `security_alert.repository_alert` | `RepositoryAlert` | reducer `security_alert_reconciliation` + `supply_chain_impact` |

`security_alert.repository_alert` is one repository-scoped security alert
reported by an external provider (GitHub Dependabot today). It is **provider
source evidence**: the alert's own state is never itself promoted as canonical
graph truth. It does drive the reconciliation read surface and seed
supply-chain-impact findings (see the accuracy boundary below), but the typed
decode is output-preserving on both.

## Required field

`repository_id` is the only required field. It is the repository/provider join
anchor both reducer consumers key on:

- the reconciliation read surface
  (`GET /api/v0/supply-chain/security-alerts/reconciliations`) keys its
  `SecurityAlertReconciliationFactFilter` on it, and
- the supply-chain-impact seeder (`securityAlertCanSeedImpact`) already required
  a non-empty `repository_id` before a security alert could contribute a
  finding.

Both collector emitters always set it (the per-repository Dependabot envelope
derives it from the committed scope; the org alert-runtime source path stamps
the per-repository id). A collector regression that drops the key now
dead-letters as `input_invalid` on both consumers instead of producing a
blank-repository reconciliation row or an empty-identity impact finding.

Every other field is optional: two collector paths emit this one kind with
different field coverage (the Dependabot envelope sets provider/advisory/
dependency fields; the alert-runtime source path additionally sets
`repository_name`, `source_freshness`, and the `collection_*` coverage fields),
so requiring any of them would dead-letter half of this kind's real traffic.

## Accuracy boundary (why this family is delicate)

The single reducer decode site (`extractProviderSecurityAlerts`,
`go/internal/reducer/security_alert_reconciliation.go`) feeds **two** consumers:
the reconciliation read surface and the `supply_chain_impact` seeder
(`appendSecurityAlertImpactFindings`). The typed struct mirrors the existing
wire payload EXACTLY — no field is added, renamed, narrowed, or widened — so the
migration cannot change what flows into `supply_chain_impact` truth. The only
behavior change is that a `repository_alert` missing `repository_id` now
dead-letters per-fact on both consumers rather than silently producing an
empty-identity row/finding. Valid facts produce byte-identical reconciliation
decisions and byte-identical impact findings.

## Normalization discipline

The struct models the raw container shapes the collector emits (`cvss`
`map[string]any`, `epss` `map[string]string`, `cwes` `[]map[string]string`); the
reducer applies its own trim / drop-empty normalization to those containers
after decode, preserving the pre-typing output byte-for-byte. The decode itself
stays a faithful mirror of the wire payload.

## Regeneration

After changing `RepositoryAlert`, from the module root:

```bash
cd sdk/go/factschema
go generate ./...
go test ./... -count=1
```

`go generate` rewrites `schema/security_alert.repository_alert.v1.schema.json`;
copy it to `fixturepack/schema/` in lockstep (the fixture pack embeds a copy
because `go:embed` cannot reach a sibling directory), and keep the
`fixturepack/payloads/security_alert.repository_alert.{valid,invalid}.json`
fixtures current.
