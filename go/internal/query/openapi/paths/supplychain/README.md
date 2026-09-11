# Supply-Chain OpenAPI Path Fragments

The OpenAPI path fragments for the supply-chain routes: container image
inventory, vulnerability findings and impact, security alerts, advisories,
SBOM attestations, and suppression mutations.

Layout:

- `routes.go` — `Routes`, composed as `impactFindings + impactExplain +
  suppressionMutation`. Unlike every other `routes.go` under `paths/`, this
  one holds no fragment of its own; it only orders three others. Do not
  add a body to it — add a new fragment file and extend the composition.
- `images.go` — `Images`; `container_images.go` — `ContainerImages`;
  `tag_history.go` — `TagHistory`;
  `vulnerability_scanner_contract.go` — `VulnerabilityScannerContract`:
  container image inventory and metadata.
- `impact_aggregate.go` — `ImpactAggregate`; `impact_findings.go` —
  `impactFindings`; `impact_explain.go` — `impactExplain`: vulnerability
  impact analysis.
- `security_alerts.go` — `SecurityAlerts`;
  `security_alert_aggregate.go` — `SecurityAlertAggregate`: security
  alerts.
- `container_image_identity_aggregate.go` — `ContainerImageIdentityAggregate`.
- `advisory_catalog.go` — `AdvisoryCatalog`;
  `advisory_evidence.go` — `AdvisoryEvidence`: advisories.
- `sbom_attestations.go` — `SBOMAttestations`;
  `sbom_attestation_attachment_aggregate.go` — `SBOMAttestationAttachmentAggregate`:
  SBOM attestations.
- `suppression_mutation.go` — `suppressionMutation`: operator-authored
  vulnerability suppressions.

`impact_findings.go` and `impact_explain.go` import `openapi/schema` for
`SupplyChainRuntimeContext`, the read-time-resolved runtime context fragment
also consumed elsewhere in the components block — see
`openapi/schema/README.md` for why that fragment lives outside this
package. No other file here imports `openapi/schema`.

At 16 files this is the largest leaf under `paths/`, closer than any other
family to the 40-non-test-file-per-directory cap the `dirgate` linter
enforces (see `tools/golangci-lint-dirgate/`). A new supply-chain fragment
is still comfortably under the cap, but check it before adding one.

## Move evidence

These 16 files moved here verbatim from the query root (Issue #6060 lane C,
#6642): `openapi_paths_supply_chain.go` -> `routes.go`,
`openapi_paths_images.go` -> `images.go`,
`openapi_paths_supply_chain_container_images.go` -> `container_images.go`,
`openapi_paths_tag_history.go` -> `tag_history.go`,
`openapi_paths_vulnerability_scanner.go` -> `vulnerability_scanner_contract.go`,
`openapi_paths_supply_chain_aggregate.go` -> `impact_aggregate.go`,
`openapi_paths_supply_chain_impact_findings.go` -> `impact_findings.go`,
`openapi_paths_supply_chain_impact_explain.go` -> `impact_explain.go`,
`openapi_paths_security_alerts.go` -> `security_alerts.go`,
`openapi_paths_security_alert_aggregate.go` -> `security_alert_aggregate.go`,
`openapi_paths_container_image_aggregate.go` ->
`container_image_identity_aggregate.go`,
`openapi_paths_supply_chain_advisory_catalog.go` -> `advisory_catalog.go`,
`openapi_paths_supply_chain_advisory.go` -> `advisory_evidence.go`,
`openapi_paths_supply_chain_sbom.go` -> `sbom_attestations.go`,
`openapi_paths_sbom_attestation_attachment_aggregate.go` ->
`sbom_attestation_attachment_aggregate.go`, and
`openapi_paths_supply_chain_suppression.go` -> `suppression_mutation.go`.
Only the package clause, file names, and (for `impact_findings.go` and
`impact_explain.go`) the `openapi/schema` import path changed; the JSON
each constant renders is unchanged.
