#!/usr/bin/env python3
"""Readiness enrollment for ./cmd/webhook-listener (generated, data only).

Do not edit by hand; regenerate with
`python3 scripts/generate-live-postgres-readiness.py`.
"""

TABLE = {
    "./cmd/webhook-listener": {
        "go/cmd/webhook-listener/cicd_actions_handler_live_test.go": (
            "TestWebhookHandlerAcceptsSignedGitHubWorkflowRunWithoutGitRefresh",
            "TestSignedActionsDeliveryPersistsInRealInboxLive",
            "TestWebhookHandlerAcceptsSignedWorkflowJob",
            "TestWebhookHandlerRejectsBadActionsSignatureBeforeStorage",
        ),
    },
}
