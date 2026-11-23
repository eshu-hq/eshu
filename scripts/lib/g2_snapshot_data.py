"""G2 served-spec snapshot data: full inventory (v2, eshu#9669).

Classification states only. Digests, SHAs, mount/fragment matches, and
wave-table coverage are derived at generation time by
scripts/gen-g2-served-snapshot.py from the tree and from local satellite
git objects — never hand-edited into the snapshot, never fetched over
the network.

Committed judgment in this module is limited to two closed sets (tier-C
dispatch and core-owned survivors) plus evidence cites. Everything else
is derived: CUTOVER_OWNERSHIP.md supplies the row universe, the
satellite wave tables at SATELLITE_SHA supply native coverage, and the
w8 port census plus wave Transport-pin lines supply pins.
"""

GENERATOR_VERSION = "v2"
GENERATOR_NAME = "gen-g2-served-snapshot.py"

# Full-inventory scope. The stale 278/279/281 counts are superseded: the
# universe is re-measured from CUTOVER_OWNERSHIP.md on every run.
INVENTORY_SCOPE = "full"

# Core side of the base pair: the tree the snapshot was derived from.
# A generation record, not ambient HEAD (no committed file can name its
# own commit). On re-base, update this const, regenerate, run the checker.
CORE_SHA = "c16ddfa3a3f0474e60b2a12477c5ac89602354e2"

# Satellite base pair side: 33 native api/*_terminate fragments plus the
# d9 direct-termination files and setup dispatch, verified via local git
# objects only. Full hash asserted (a mistranscribed literal fails the
# satellite half of the checker instead of silently covering less).
SATELLITE_SHA = "c041d7ec95e41cba1c9874c8dc098af6a2f994f8"

# In-tree sources (paths relative to the core worktree root).
CUTOVER = "go/cmd/api/CUTOVER_OWNERSHIP.md"
W8_CENSUS = "docs/internal/w8-port-census.md"

# Classification vocabulary for 9669-G2 (arbiter G2/G8/G9 scope ruling,
# 2026-11-13). Counts must sum to the re-measured inventory.
CLASSES = (
    "native-served",    # satellite terminates natively; relay drained
    "tier-C-dispatch",  # dispatched to tier-C compat (10491-G4 re-verify)
    "relay-disclosed",  # still riding the relay catch-all, disclosed
    "absent-disclosed",  # in inventory, served by neither side, disclosed
)
SERVED_BY = {
    "native-served": "satellite-native",
    "tier-C-dispatch": "tier-C",
    "relay-disclosed": "core-relay",
    "absent-disclosed": "none-absent",
}

# Tier-C dispatch rows: closed set from tracker eshu-hq/eshu#10491
# "Dispatched tier-C (27)" section, terminal per Decision 2, G4 closed.
# Generator asserts disjointness from satellite wave coverage.
TIER_C_ROUTES = (
    "DELETE /api/v0/auth/browser-session",
    "GET /api/v0/auth/browser-session",
    "GET /api/v0/auth/local/api-tokens",
    "GET /api/v0/auth/profile",
    "GET /api/v0/auth/sessions",
    "PATCH /api/v0/auth/browser-session/context",
    "POST /api/v0/admin/backfill",
    "POST /api/v0/admin/dead-letter",
    "POST /api/v0/admin/reindex",
    "POST /api/v0/admin/skip",
    "POST /api/v0/supply-chain/impact/suppressions",
    "POST /api/v0/auth/browser-session",
    "POST /api/v0/auth/local/api-tokens",
    "POST /api/v0/auth/local/api-tokens/{token_id}/revoke",
    "POST /api/v0/auth/local/api-tokens/{token_id}/rotate",
    "POST /api/v0/auth/local/bootstrap",
    "POST /api/v0/auth/local/break-glass",
    "POST /api/v0/auth/local/break-glass/session",
    "POST /api/v0/auth/local/invitations",
    "POST /api/v0/auth/local/invitations/accept",
    "POST /api/v0/auth/local/login",
    "POST /api/v0/auth/local/mfa/totp/begin",
    "POST /api/v0/auth/local/mfa/totp/confirm",
    "POST /api/v0/auth/local/password/rotate",
    "POST /api/v0/auth/local/users/{user_id}/disable",
    "POST /api/v0/auth/local/users/{user_id}/mfa-reset",
    "POST /api/v0/auth/local/users/{user_id}/password",
)

# Core-owned survivor candidates: canonical mutations stay core-owned per
# eshu-hq/eshu#8872 ("Canonical mutations must persist through explicit
# core-owned admission contracts"), named by the #9669 census
# acceptance criterion. The arbiter confirms or trims this list; the
# checker fails closed on any survivor found outside relay-disclosed.
SURVIVOR_ROUTES = (
    "DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}",
    "PATCH /api/v0/auth/admin/sign-in-policy",
    "POST /api/v0/admin/collector-failures/acknowledge",
    "POST /api/v0/admin/recover-generations",
    "POST /api/v0/admin/refinalize",
    "POST /api/v0/admin/reopen",
    "POST /api/v0/admin/replay",
    "POST /api/v0/admin/repository-retirements",
    "POST /api/v0/admin/workflow-retire-unclaimed",
    "POST /api/v0/auth/admin/idp-group-mappings",
    "POST /api/v0/auth/admin/provider-configs",
    "POST /api/v0/auth/admin/provider-configs/{provider_config_id}",
    "POST /api/v0/auth/admin/provider-configs/{provider_config_id}/disable",
    "POST /api/v0/auth/admin/provider-configs/{provider_config_id}/enable",
    "POST /api/v0/auth/admin/provider-configs/{provider_config_id}/revert",
    "POST /api/v0/auth/admin/provider-configs/{provider_config_id}/test-connection",
    "POST /api/v0/auth/admin/role-assignments",
    "POST /api/v0/auth/admin/role-assignments/revoke",
    "POST /api/v0/auth/local/invitations/{invite_id}/revoke",
)

# D9 direct native-public routes (no wave table; exact-path registration
# in satellite api/router.go viaNative loops over d9Routes,
# d9GitHubRoutes, d9SAMLRoutes with terminated handlers). Generator
# asserts disjointness from wave coverage; the checker re-verifies the
# route consts in api/d9.go, api/d9github.go, api/d9saml.go when a
# satellite repo is provided.
D9_DIRECT_FILES = (
    "api/d9.go",
    "api/d9github.go",
    "api/d9saml.go",
)

# Setup native-dispatch routes (no wave table; D10 GET plus W7-writers
# trio registered viaNative in satellite api/router.go: setup-state GET
# via serveSetupState, the three credential POSTs via setupWriteThrough
# forward). Same disjointness + re-verification treatment as D9.
SETUP_FILES = (
    "api/setup.go",
    "api/setup_terminate.go",
)

# W8-census gap: rows present in CUTOVER with no w8-port-census row and
# no tracker #10491 section. Named here so the snapshot carries the
# follow-up instead of silently dropping the row.
CENSUS_GAP_ROUTES = (
    "GET /api/v0/repository/manifest-dependencies",
)

# Wave files whose terminate fragment is not the mechanical sibling.
# d3trio is wave-only: its trio terminates via api/d3_terminate.go
# serveD3Terminated (router d3TrioWave loop), proven by empty
# `git ls-tree ... api/d3trio_terminate.go` at the pinned SHA.
WAVE_TERMINATE_OVERRIDE = {
    "api/d3trio_wave.go": "api/d3_terminate.go",
}

# W3 IaC-6 native-dispatch lane (no wave table, no terminate fragment;
# api/iac.go route consts grouped in iacNativeRoutes + iacResourcesRoutes,
# router loops viaNative with iacWriteThrough ahead of the proxy —
# byte-identical mechanism to the setup write-through). Same
# disjointness + re-verification treatment as D9 direct routes.
IAC_FILES = (
    "api/iac.go",
)

# Per-row pin overrides where the satellite lane record is finer than
# the w8 census C2 cell. api/iac.go documents the split itself: the four
# POST legs clear on IaCReadAdmission v0, the dead POST on the
# supplemental IaCDeadContentAdmission v0 dual pin, the resources GET on
# IaCInventorySearchAdmission v0 (own slice, own pin). The checker
# verifies each const exists in-tree; the w8 census keeps the family
# pin for all six.
IAC_PIN_OVERRIDE = {
    "POST /api/v0/iac/dead": "IaCDeadContentAdmission",
    "GET /api/v0/iac/resources": "IaCInventorySearchAdmission",
}

# Satellite terminate fragment owning each direct (non-wave) source file.
# Committed and reviewable so the checker can independently resolve a
# direct row's digest without re-running generator logic.
# G2 wave pin map: the 7 MUST-TERMINATE rows and the core read-admission
# v0 pin that covers each (minted core-side by eshu-hq/eshu#10796, merged
# 64de9fc7f1; verified in-tree post re-freeze). The satellite g2_wave.go
# cites the same names; the checker verifies every const exists.
G2_PIN_OVERRIDE = {
    "GET /api/v0/collector-extraction-readiness": "ExtractionReadinessReadAdmission",
    "GET /api/v0/collector-extraction-readiness/{family}": "ExtractionReadinessReadAdmission",
    "GET /api/v0/fact-schema-versions": "FactSchemaReadAdmission",
    "GET /api/v0/fact-schema-versions/{fact_kind}": "FactSchemaReadAdmission",
    "GET /api/v0/surface-inventory": "SurfaceInventoryReadAdmission",
    "POST /api/v0/visualizations/derive": "VisualizationReadAdmission",
    "GET /api/v0/repository/manifest-dependencies": "ManifestDependenciesReadAdmission",
}

# Satellite terminate fragment owning each direct (non-wave) source file.
DIRECT_TERMINATE = {
    "api/d9.go": "api/oidc_terminate.go",
    "api/d9github.go": "api/github_terminate.go",
    "api/d9saml.go": "api/saml_terminate.go",
    "api/setup.go": "api/setup_terminate.go",
    "api/setup_terminate.go": "api/setup_terminate.go",
    # W3 lane file digests itself: no terminate fragment exists, so the
    # lane's route-const + dispatch source is the pinned fragment.
    "api/iac.go": "api/iac.go",
}


def short(sha):
    """Shorten a SHA for prose; SHAs stay full in machine-checked fields."""
    return sha[:12]
