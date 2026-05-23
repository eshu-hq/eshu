# ACR Adapter Notes

Read `adapter.go`, `adapter_test.go`, and the OCI registry ADR before changing
this package.

Keep ACR provider behavior limited to host validation, repository
normalization, identity construction, and Distribution client setup. Do not add
Azure SDK calls here unless an ADR explicitly moves token acquisition into the
collector.

Do not leak Entra tokens, service principal secrets, registry names from private
fixtures, or credentials into logs, errors, facts, metric labels, or docs.
