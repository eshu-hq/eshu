# Cloud runtime projector domains

This documentation-only namespace groups projector intent builders driven by
observed cloud runtime state. `drift/` contains the AWS-specific and
provider-neutral runtime-drift leaves.

Runtime logic stays in the leaf packages. Root projector assembly owns ordered
dispatch, queue writes, retries, and telemetry; reducers own materialization.
