# ACR Adapter Notes

Read `adapter.go`, `adapter_test.go`, `../README.md`,
`../ociruntime/README.md`, and
`docs/public/reference/collector-reducer-readiness.md` before changing this
package.

Keep ACR provider behavior limited to host validation, repository
normalization, identity construction, and Distribution client setup. Do not add
Azure SDK calls here unless architecture-owner approval explicitly moves token
acquisition into the collector and updates package tests plus public collector
docs.

Do not leak Entra tokens, service principal secrets, registry names from private
fixtures, or credentials into logs, errors, facts, metric labels, or docs.
