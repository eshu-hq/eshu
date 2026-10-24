#!/usr/bin/env python3
"""Readiness enrollment for ./internal/query/iac (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/query/iac": {
        "go/internal/query/iac/inventory_postgres_live_test.go": (
            "TestPostgresIaCInventoryStoreLive",
        ),
        "go/internal/query/iac/inventory_postgres_read_model_live_test.go": (
            "TestPostgresIaCInventoryReadModelLive",
            "TestPostgresIaCInventoryFailedGenerationServesLastProjected",
        ),
    },
}
