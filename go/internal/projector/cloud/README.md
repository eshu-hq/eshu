# Cloud projector domains

This documentation-only namespace groups projector intent builders for cloud
inventory and runtime drift.

- `inventory/` builds cloud-inventory-admission intents.
- `runtime/drift/` groups provider-specific and provider-neutral runtime-drift
  intent builders.

The leaf packages select facts and construct reducer intents. Root projector
assembly continues to own ordering, queue writes, retries, and telemetry. This
namespace adds no runtime ownership and is not an independently extractable
service boundary while its leaves depend on internal Eshu contracts.
