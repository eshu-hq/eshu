# Cloud runtime-drift projector domains

This documentation-only namespace groups the runtime-drift intent builders.

- `aws/` builds the AWS-specific runtime-drift intent.
- `multi/` builds the provider-neutral GCP and Azure runtime-drift intent.

The leaves remain separate because their triggers, domains, entity keys, and
reducer ownership differ. Root projector assembly retains their existing
dispatch positions and all queue and telemetry ownership.
