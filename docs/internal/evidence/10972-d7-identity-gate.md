# #10972 D7: The Identity Gate On The Fan-Out Origin

Refs #10972. Arbiter ruling: `.claude/arbiter-pr4-d7.md`, section (b). This
note covers branch `fix/10972-d7-identity-digest-fanout`.

## What changed

Every finding row now carries two digests in provenance. `content_digest`
(the full digest) is unchanged in meaning and still decides the row write,
the winners dirty capture and the advisory-group enqueue. `identity_digest`
is the full digest with the fields computed only from EPSS and KEV facts
left out. It decides whether a pass originates a holder fan-out for the row.
A pass whose own generation holds an EPSS or KEV fact (cause `epss`, `kev`,
or `mixed` with either) still fans out on the full digest.

The enrichment class, verified in source:

| Field | Where it is computed |
| --- | --- |
| `epss_probability`, `epss_percentile` | the selected EPSS observation (reducerderived/v1 `findings.go:36-37`) |
| `known_exploited` | the KEV listing (`findings.go:38`) |
| `priority_reason`, `priority_score`, `priority_bucket`, `priority_reason_codes`, `priority_contributions`, `exploited_tier` | `withSupplyChainImpactPriority` (`core/priority.go:91-121`); its other inputs are identity fields |
| `provenance.selected_epss_score_date`, `provenance.selected_epss_model_version` | `core/provenance.go:420`, `:423` |
| `provenance.advisory_group_epss_source`, `provenance.advisory_group_kev_source` | `core/provenance.go:462`, `:465` |

Every other field is identity. `TestIdentityDigestClassifiesEveryPayloadField`
mutates each top-level field and fails on any field in the wrong class, so a
new field is identity (it fans out) until someone classifies it.

The fan-out lookup ids are the union of the changed CVE ids and the touched
group ids. Both are now judged on the identity digest for a non-enrichment
pass, so the echo cannot survive through the group set. With no lookup ids
the pass reads no holders and runs no in-flight read, so echo-driven
in-flight flags stop too. A stored row written before this change has no
identity digest and reads as changed, so it fans out once after rollout.

## No-Regression Evidence:

- Statement count: unchanged. The stored-findings read gains one column on
  the same row set (`COALESCE(payload #>> '{provenance,identity_digest}', '')`),
  and the fan-out runs fewer holder reads and fewer in-flight reads, never
  more.
- CPU: one extra SHA-256 over a copy of the typed payload per written row, the
  same order of work as the existing content digest.
- Payload size: one 64-character hex string per row.
- The full `content_digest` value is unchanged.
  `TestContentDigestValueIsPinnedForAFixedFinding` pins it as a literal hex
  value computed on the pre-D7 code for a fixed fixture finding. The
  byte-exact batch payload test alone could not show it, because it
  recomputes its expectation with the production digest.
- Live proofs on PostgreSQL 18.3 (the coordinator's runs):
  - At the RED commit, `TestEPSSDateOnlyRerunOfANonEnrichmentScopeReopensNobodyLive`
    and `TestFanOutIdentityGateKEVListingSeenByANonEnrichmentScopeReopensNobodyLive`
    failed: the EPSS-only and KEV-only reruns reached 21 holders.
  - At the fix commit, every case passed: both of those,
    `TestEPSSScopesOwnPassStillReopensEveryHolderLive`,
    `TestFanOutIdentityGateKEVScopesOwnPassReopensEveryHolderLive`,
    `TestFanOutIdentityGateMixedChangeStillFansOutLive`, and
    `TestFanOutIdentityGateScopeHoldingEPSSFansOutOnTheFullDigestLive`.

## Observability Evidence:

`eshu_dp_supply_chain_impact_fanout_total{outcome="identity_gated",cause}`
counts, per committing cause, the lookup ids a non-enrichment pass did not
fan out for. After the roll, read it beside `reopened` and `replay_flagged`
with the same `cause` for three hours. The share of gated ids over gated plus
fanned-out ids is the change-class share the arbiter asked for. A holder whose
copy lags on EPSS shows it in the copy's `provenance.selected_epss_score_date`
against the EPSS scope's own row.

## Accuracy

A holder copy's EPSS and KEV freshness now depends on the EPSS or KEV scope's
own fan-out alone. While that fan-out is dead-lettered, copies refresh on
their own cadence, and a stale copy can outrank a fresh one in the winners.
That is the bound `HOLDER_FANOUT.md` already states (owed marker, dead-letter
reopen). No echo is promised.
