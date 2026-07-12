# Python Parser

| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Functions | `functions` | supported | `functions` | `name, line_number` | `node:Function` | `go/internal/parser/python_language_test.go::TestPythonFunctions` | Compose-backed fixture verification | - |
| Django/DRF routes | `django-drf-routes` | partial | - | - | - | `go/internal/parser/python_language_test.go::TestPythonFunctions` | Explicit unsupported-route wording | Not audited as route_entries or HANDLES_ROUTE truth. |
