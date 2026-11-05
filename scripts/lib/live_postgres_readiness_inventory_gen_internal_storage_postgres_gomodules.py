#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/gomodules (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/gomodules": {
        "go/internal/storage/postgres/gomodules/store_live_test.go": (
            "TestListGoModuleRepositoriesRotatesEveryRepositoryLive",
            "TestListGoModuleRepositoriesSkipsInactiveScopesLive",
            "TestListGoModuleRepositoriesDetectsFromContentNotFactsLive",
            "TestGoModulePageAdmissionIsIdempotentLive",
            "TestListGoModuleRepositoriesPlansRepositoryThatLostItsGoModLive",
            "TestListGoModuleRepositoriesDropsRepositoryAfterRetractionLive",
        ),
    },
}
