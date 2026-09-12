# supply

Documentation namespace for the supply-chain OpenAPI path fragments. This
directory owns no runtime behavior; its one child is a Go package:

| Child | Package | Owns |
| --- | --- | --- |
| `chain/` | `chain` | The OpenAPI path fragments for the supply-chain routes: container image inventory, vulnerability findings and impact, security alerts, advisories, SBOM attestations, and suppression mutations — see `chain/README.md` for the full file-to-constant map. |

## Move evidence

This directory was created to nest the OpenAPI leaf that used to be
`paths/supplychain/` (package `supplychain`) as `paths/supply/chain/`
(package `chain`), per the owner's ruling that `supplychain` is a glued
compound under `docs/internal/naming.md` rule 3 (Issue #6060 lane C,
#6642/#6648).
