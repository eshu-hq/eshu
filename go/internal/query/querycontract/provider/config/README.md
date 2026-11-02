# querycontract/provider/config

## What is here

| File | Contents |
| --- | --- |
| `types.go` | The admin provider-config wire DTOs: `Detail`, `RevisionItem`, `WriteResult`, `ConnectionTestResult`, and the store requests `CreateRequest`, `UpdateRequest`, `RevertRequest`. |

This leaf exists so the provider-config handler family
(`internal/query/admin/provider/config`) and `internal/query/testutil`
can name the same DTOs without an import cycle (#7715): the family's
in-package tests import `testutil`, so `testutil` cannot import the
family back, but both may import this stdlib-only leaf. The family
package forwards these types through aliases; the ports
(`ReadStore`, `MutationStore`, `ConnectionTester`) stay in the family.
