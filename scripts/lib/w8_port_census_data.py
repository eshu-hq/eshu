#!/usr/bin/env python3
"""W8 census classification data (pin/writer tables + route sets + short()).

Data-only sibling of scripts/gen-w8-port-census.py. All pin constants
verified present at the REVERIFIED_CORE named in the main script; scope
notes record method-level partials. Code is deliberately NOT a family PIN
(a family pin would mark all 27 Code rows pinned on the 2-route
CodeSearch pin + 4-route CodeFlow pin): Code coverage is route-level via
CODE_SEARCH / CODE_FLOW, mirroring CONTENT_POINT.
"""

# family -> (pin const, file:line, scope note)
PIN = {
    "Freshness": ("FreshnessReadAdmission v0", "q/freshness/read_admission.go:26", "all 3"),
    "AdminIdentityReads": ("AdminIdentityReadAdmission v0 + GovernanceAuditReadAdmission v0", "go/internal/query/admin/identity/read_admission.go:16", "family"),
    "AdminProviderConfigReads": ("ProviderConfigReadAdmission v0", "go/internal/query/admin/provider/config/admission.go:37", "family"),
    "SignInPolicyReads": ("SignInPolicyReadAdmission v0", "q/auth/read_admission.go:21", "family"),
    "Compare": ("CompareReadAdmission v0", "q/compare/read_admission.go:24", "POST"),
    "IaC": ("IaCReadAdmission v0", "q/iac/read_admission.go:24", "9 of 11 (+2 partial: resources SearchActive, dead Content fallback)"),
    "Metrics": ("MetricsReadAdmission v0", "q/metrics/read_admission.go:20", "timeseries"),
    "TerraformConfigStateDrift": ("TerraformDriftReadAdmission v0", "go/internal/query/terraform/drift/read_admission.go:20", "findings POST"),
    "SemanticSearch": ("SemanticSearchReadAdmission v0 (record-only)", "go/internal/query/semanticsearch/read_admission.go:20", "search POST"),
    "SecretsIAM": ("SecretsIAMReadAdmission v0", "q/secrets/read_admission.go:66", "4 of 5 (posture-summary S3 grant leg OUT)"),
    "Status": ("StatusGovernanceReadAdmission v0 + StatusHostedReadinessReadAdmission v0 + StatusReadAdmission v0 (pre-existing narration/snapshot)", "q/status_governance_read_admission.go:73", "all 3 (governance Summary + hosted Run/RunSingle + narration record-only)"),
    "Content": ("ContentSearchReadAdmission v0", "q/content_search_read_admission.go:103", "4 searches (point reads ride ContentPoint pin)"),
}
CONTENT_POINT = {"POST /api/v0/content/entities/read", "POST /api/v0/content/files/lines",
                 "POST /api/v0/content/files/read"}
CODE_SEARCH = {"POST /api/v0/code/search", "POST /api/v0/code/symbols/search"}
CODE_FLOW = {"POST /api/v0/code/flow/cfg-summary", "POST /api/v0/code/flow/pdg-summary",
             "POST /api/v0/code/flow/reaching-def", "POST /api/v0/code/flow/taint-path"}
# Status route -> covering pin (verified: Extra trio per #10405 in-scope legs;
# governance/hosted/narration per #10538 comments; the other 12 Done aggregate
# reads terminated in the D6 wave on StatusReadAdmission v0, whose snapshot legs
# the narrow mints explicitly leave covered).
STATUS_PIN = {
    "GET /api/v0/status/operations": "StatusExtraReadAdmission",
    "GET /api/v0/index-status": "StatusExtraReadAdmission",
    "GET /api/v0/status/index": "StatusExtraReadAdmission",
    "GET /api/v0/status/governance": "StatusGovernanceReadAdmission",
    "GET /api/v0/status/hosted-readiness": "StatusHostedReadinessReadAdmission",
    "GET /api/v0/status/answer-narration": "StatusReadAdmission",
    "GET /api/v0/collector-readiness": "StatusReadAdmission",
    "GET /api/v0/collectors": "StatusReadAdmission",
    "GET /api/v0/ingesters": "StatusReadAdmission",
    "GET /api/v0/ingesters/{ingester}": "StatusReadAdmission",
    "GET /api/v0/status/collector-readiness": "StatusReadAdmission",
    "GET /api/v0/status/collectors": "StatusReadAdmission",
    "GET /api/v0/status/freshness-causality": "StatusReadAdmission",
    "GET /api/v0/status/ingesters": "StatusReadAdmission",
    "GET /api/v0/status/ingesters/{ingester}": "StatusReadAdmission",
    "GET /api/v0/status/operator-control-plane": "StatusReadAdmission",
    "GET /api/v0/status/pipeline": "StatusReadAdmission",
    "GET /api/v0/status/semantic-extraction": "StatusReadAdmission",
}
# family-level writer admission (constants verified at census SHA)
WRITER_FAM_ADM = {
    "GitHubLogin": "GitHubLoginAdmission v0", "OIDCLogin": "OIDCLoginAdmission v0",
    "SAML": "SAMLLoginAdmission v0", "BrowserSessions": "BrowserSessionAdmission v0",
    "LocalIdentity": "LocalIdentityAdmission v0", "SessionList": "SessionResolverAdmission v0",
    "Profile": "SessionResolverAdmission v0", "Setup": "SetupAdmission v0",
    "SignInPolicyMutations": "SignInPolicyAdmission v0",
    "AdminTerminalFailure": "TerminalFailureAdmission v0", "AdminRetirement": "RetirementAdmission v0",
}
# exact-route writer admission (certain mappings only)
WRITER_ADM = {
    "POST /api/v0/admin/replay": "RecoveryAdmission+ReplayLedgerAdmission v0",
    "POST /api/v0/admin/reopen": "RecoveryAdmission+ReplayLedgerAdmission v0",
    "POST /api/v0/admin/refinalize": "RecoveryAdmission+ReplayLedgerAdmission v0",
    "POST /api/v0/admin/recover-generations": "RecoveryAdmission+ReplayLedgerAdmission v0",
    "POST /api/v0/supply-chain/impact/suppressions": "SuppressionAdmission v0",
    "GET /api/v0/auth/local/api-tokens": "APITokenAdmission v0", "POST /api/v0/auth/local/api-tokens": "APITokenAdmission v0",
    "POST /api/v0/auth/local/api-tokens/{token_id}/revoke": "APITokenAdmission v0",
    "POST /api/v0/auth/local/api-tokens/{token_id}/rotate": "APITokenAdmission v0",
    "DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}": "AdminIdentityAdmission v0",
}
# approved but not yet minted (post-mint leftovers only)
PIN_APPROVED_UNMINTED = {
    "SemanticEvidence": "none (SemanticSearch record-only covers search, not evidence)",
}

SHORT = {"go/internal/query/": "q/", "go/cmd/api/": "cmd/", "go/internal/runtime/": "rt/"}


def short(p):
    """Shorten a repo path for table display."""
    for k, v in SHORT.items():
        if p.startswith(k):
            return v + p[len(k):]
    return p
