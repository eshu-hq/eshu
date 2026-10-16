#!/usr/bin/env python3
"""Go package paths for the live PostgreSQL readiness inventory (data only).

Shared by the per-domain inventory modules and by
live_postgres_readiness_inventory.py, which re-exports them for callers.
"""

IMPACT_PACKAGE = "./internal/query/supply/chain/impact"
STORAGE_PACKAGE = "./internal/storage/postgres"
TARGETED_PACKAGE = "./internal/storage/postgres/backfill/targeted"
ACTIVATION_PACKAGE = "./internal/storage/postgres/activation"
MAINTENANCE_PACKAGE = "./internal/storage/postgres/maintenance"
MEMBERSHIP_PACKAGE = "./internal/storage/postgres/membership"
REDUCER_PACKAGE = "./cmd/reducer"
QUERY_PACKAGE = "./internal/query"
SUMMARY_PACKAGE = "./internal/storage/postgres/status/summary"
WRITER_PACKAGE = "./internal/reducer/status/summary"
ADMIN_PACKAGE = "./internal/query/admin"
REACHABILITY_PACKAGE = "./internal/storage/postgres/code/reachability"
CONFIG_ARTIFACTS_PACKAGE = "./internal/storage/postgres/config/artifacts"
UNSCOPED_SEARCH_PACKAGE = "./internal/query/search/unscoped"
WEBHOOK_PACKAGE = "./internal/storage/postgres/webhook"
SEARCH_INDEX_RETENTION_PACKAGE = "./internal/storage/postgres/search/index/retention"
