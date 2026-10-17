#!/usr/bin/env python3
"""Readiness enrollment for ./internal/storage/postgres/webhook (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./internal/storage/postgres/webhook": {
        "go/internal/storage/postgres/webhook/ignored_decision_live_test.go": (
            "TestWebhookTriggerStoreUpsertDemotesQueuedToIgnored",
            "TestWebhookTriggerStoreReapFailsIgnoredDecisionClaims",
            "TestWebhookTriggerStoreIgnoredDecisionFailIsDecisionGuarded",
        ),
    },
}
