"""G2 served-spec snapshot data: content-search family (Phase 1, eshu#9669).

Classification states only. Digests, SHAs, and mount lines are derived at
generation time by scripts/gen-g2-served-snapshot.py from the tree and from
local satellite git objects — never hand-edited into the snapshot, never
fetched over the network.
"""

GENERATOR_VERSION = "v1"
GENERATOR_NAME = "gen-g2-served-snapshot.py"

# Family scope for Phase 1. Full-inventory G2 re-measures the inventory at
# the removal base (278/279/281 are all stale); this module grows by family.
INVENTORY_SCOPE = "content-search"

# Core side of the base pair: the tree the snapshot was derived from.
# A generation record, not ambient HEAD (no committed file can name its
# own commit). On re-base, update this const, regenerate, run the checker.
CORE_SHA = "895eff3cd33ed6dca44445bb73aca932f8b53c5f"

# Satellite base pair side, verified 2026-11-13 via local git objects only:
# the 3 point reads are router-mounted and spec-fragmented at this SHA,
# the 4 searches have no spec fragment under openapi/paths/search/.
SATELLITE_SHA = "943c1eee3897fa92149bc5cbcac04017bfb67542"
SATELLITE_FRAGMENT = "openapi/paths/search/content_point_reads.go"
SATELLITE_SEARCH_DIR = "openapi/paths/search"

# In-tree sources (paths relative to the core worktree root).
CUTOVER = "go/cmd/api/CUTOVER_OWNERSHIP.md"
MOUNT_FILE = "go/internal/query/contentread/content_handler.go"
MOUNT_FUNC = "ContentHandler.Mount"
FRAGMENT_FILE = "go/internal/query/openapi/paths/search/content.go"
SEARCH_PIN_FILE = "go/internal/query/content_search_read_admission.go"
POINT_PIN_FILE = "go/internal/query/content_read_admission.go"
SEARCH_PIN_CONST = 'ContentSearchReadAdmissionContractVersion = "v0"'
POINT_PIN_CONST = 'ContentPointReadAdmissionContractVersion = "v0"'

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

# The 7 Content boxes (#10491 "Content (7)"): 3 point reads native via
# satellite eshu-api#95, 4 searches relay-disclosed under the minted
# core pin eshu#10540 with satellite termination pending (unmerged
# satellite W4 lane drive/9669-w4-satellite).
ROUTES = (
    {
        "route": "POST /api/v0/content/files/read",
        "class": "native-served",
        "pin": "ContentPointReadAdmission v0",
        "sat_fragment": SATELLITE_FRAGMENT,
        "evidence": "tracker #10491 Content [x] native (sat #93); "
                    "satellite eshu-api#95 merged; core point pin eshu#10504",
    },
    {
        "route": "POST /api/v0/content/files/lines",
        "class": "native-served",
        "pin": "ContentPointReadAdmission v0",
        "sat_fragment": SATELLITE_FRAGMENT,
        "evidence": "tracker #10491 Content [x] native (sat #93); "
                    "satellite eshu-api#95 merged; core point pin eshu#10504",
    },
    {
        "route": "POST /api/v0/content/entities/read",
        "class": "native-served",
        "pin": "ContentPointReadAdmission v0",
        "sat_fragment": SATELLITE_FRAGMENT,
        "evidence": "tracker #10491 Content [x] native (sat #93); "
                    "satellite eshu-api#95 merged; core point pin eshu#10504",
    },
    {
        "route": "POST /api/v0/content/files/search",
        "class": "relay-disclosed",
        "pin": "ContentSearchReadAdmission v0",
        "sat_fragment": None,
        "evidence": "tracker #10491 Content [ ] unchecked; core search pin "
                    "eshu#10540 minted; satellite W4 termination pending "
                    "(unmerged drive/9669-w4-satellite)",
    },
    {
        "route": "POST /api/v0/content/entities/search",
        "class": "relay-disclosed",
        "pin": "ContentSearchReadAdmission v0",
        "sat_fragment": None,
        "evidence": "tracker #10491 Content [ ] unchecked; core search pin "
                    "eshu#10540 minted; satellite W4 termination pending "
                    "(unmerged drive/9669-w4-satellite)",
    },
    {
        "route": "POST /api/v0/content/dockerfile-installs/search",
        "class": "relay-disclosed",
        "pin": "ContentSearchReadAdmission v0",
        "sat_fragment": None,
        "evidence": "tracker #10491 Content [ ] unchecked; core search pin "
                    "eshu#10540 minted; satellite W4 termination pending "
                    "(unmerged drive/9669-w4-satellite)",
    },
    {
        "route": "POST /api/v0/content/php-config/search",
        "class": "relay-disclosed",
        "pin": "ContentSearchReadAdmission v0",
        "sat_fragment": None,
        "evidence": "tracker #10491 Content [ ] unchecked; core search pin "
                    "eshu#10540 minted; satellite W4 termination pending "
                    "(unmerged drive/9669-w4-satellite)",
    },
)


def short(sha):
    """Shorten a SHA for prose; SHAs stay full in machine-checked fields."""
    return sha[:12]
