<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# G2 served-spec snapshot — content-search family (Phase 1)

Refs eshu-hq/eshu#9669 (gate G2), eshu-hq/eshu#10491 (Content (7) boxes). Status: tooling only — no cutover has occurred, no behavior changes, no in-tree code moves. Classification earns zero removal credit per the arbiter G2/G8/G9 scope ruling (2026-11-13).

## Provenance (generation record, verified offline)

```text
core_sha: 895eff3cd33ed6dca44445bb73aca932f8b53c5f
satellite_sha: 943c1eee3897fa92149bc5cbcac04017bfb67542
generator: gen-g2-served-snapshot.py v1
offline: no live fetch in any form (git show/ls-tree on local objects, rg, sha256sum only)
```

Regenerate (run in the worktree root):

```bash
python3 scripts/gen-g2-served-snapshot.py --satellite-repo PATH
bash scripts/check-g2-served-snapshot.sh
```

The satellite repo is read-only input: the generator runs only `rev-parse --verify`, `show`, and `ls-tree` against the pinned SHA and never fetches, pulls, or clones. At 943c1eee3897 the satellite serves the 3 point reads natively (`openapi/paths/search/content_point_reads.go`) and carries no content-search fragment under `openapi/paths/search/`; the 4 searches stay core-relay until the satellite W4 termination lands.

## Classification schema (full-inventory G2)

Every in-tree route carries exactly one class; class counts must sum to the re-measured inventory at the removal base (278/279/281 are all stale — recount then). Phase 1 pins the content-search family (7 rows); later families append rows under the same columns.

| class | served_by | meaning |
| --- | --- | --- |
| native-served | satellite-native | satellite terminates natively; relay drained |
| tier-C-dispatch | tier-C | dispatched to tier-C compat (10491-G4 re-verify) |
| relay-disclosed | core-relay | still riding the relay catch-all, disclosed |
| absent-disclosed | none-absent | in inventory, served by neither side, disclosed |

Column rules: route (`METHOD path`); class (one of the four above); served_by (derived from class); spec_digest (`sat:sha256:<hex>` of the satellite fragment at the pinned SHA for native rows, `tree:sha256:<hex>` of the in-tree fragment for relay rows); pin (covering admission contract or `none`); mount (serving-side registration cite); evidence (tracker box + PR/pin cites). G2 passes only when relay-disclosed + absent-disclosed = 0 except core-owned survivors named in the #9669 body, and tier-C rows passed 10491-G4.

## Source digests (recomputed by the checker)

```text
intree_fragment: go/internal/query/openapi/paths/search/content.go sha256:d8254d1b9313db556f69b3d9b40925fc4a64094aa6629aa47dad16aa728edcca
sat_fragment: openapi/paths/search/content_point_reads.go@943c1eee3897 sha256:5561c38918fc4258f736805b90c2e3e5bc985f12a62b7140001f343ec82fd112
sat_search_dir_listing: openapi/paths/search/@943c1eee3897 sha256:ad84323bd58ed03ab7524836386aea2aeb09135f70f57c94550fdf3b1ba96cf6
```

## Family inventory: content-search (7 rows)

| route | class | served_by | spec_digest | pin | mount | evidence |
| --- | --- | --- | --- | --- | --- | --- |
| `POST /api/v0/content/dockerfile-installs/search` | relay-disclosed | core-relay | tree:sha256:d8254d1b9313db556f69b3d9b40925fc4a64094aa6629aa47dad16aa728edcca | ContentSearchReadAdmission v0 | ContentHandler.Mount (go/internal/query/contentread/content_handler.go) | tracker #10491 Content [ ] unchecked; core search pin eshu#10540 minted; satellite W4 termination pending (unmerged drive/9669-w4-satellite) |
| `POST /api/v0/content/entities/read` | native-served | satellite-native | sat:sha256:5561c38918fc4258f736805b90c2e3e5bc985f12a62b7140001f343ec82fd112 | ContentPointReadAdmission v0 | sat content_wave.go + ContentHandler.Mount (go/internal/query/contentread/content_handler.go) | tracker #10491 Content [x] native (sat #93); satellite eshu-api#95 merged; core point pin eshu#10504 |
| `POST /api/v0/content/entities/search` | relay-disclosed | core-relay | tree:sha256:d8254d1b9313db556f69b3d9b40925fc4a64094aa6629aa47dad16aa728edcca | ContentSearchReadAdmission v0 | ContentHandler.Mount (go/internal/query/contentread/content_handler.go) | tracker #10491 Content [ ] unchecked; core search pin eshu#10540 minted; satellite W4 termination pending (unmerged drive/9669-w4-satellite) |
| `POST /api/v0/content/files/lines` | native-served | satellite-native | sat:sha256:5561c38918fc4258f736805b90c2e3e5bc985f12a62b7140001f343ec82fd112 | ContentPointReadAdmission v0 | sat content_wave.go + ContentHandler.Mount (go/internal/query/contentread/content_handler.go) | tracker #10491 Content [x] native (sat #93); satellite eshu-api#95 merged; core point pin eshu#10504 |
| `POST /api/v0/content/files/read` | native-served | satellite-native | sat:sha256:5561c38918fc4258f736805b90c2e3e5bc985f12a62b7140001f343ec82fd112 | ContentPointReadAdmission v0 | sat content_wave.go + ContentHandler.Mount (go/internal/query/contentread/content_handler.go) | tracker #10491 Content [x] native (sat #93); satellite eshu-api#95 merged; core point pin eshu#10504 |
| `POST /api/v0/content/files/search` | relay-disclosed | core-relay | tree:sha256:d8254d1b9313db556f69b3d9b40925fc4a64094aa6629aa47dad16aa728edcca | ContentSearchReadAdmission v0 | ContentHandler.Mount (go/internal/query/contentread/content_handler.go) | tracker #10491 Content [ ] unchecked; core search pin eshu#10540 minted; satellite W4 termination pending (unmerged drive/9669-w4-satellite) |
| `POST /api/v0/content/php-config/search` | relay-disclosed | core-relay | tree:sha256:d8254d1b9313db556f69b3d9b40925fc4a64094aa6629aa47dad16aa728edcca | ContentSearchReadAdmission v0 | ContentHandler.Mount (go/internal/query/contentread/content_handler.go) | tracker #10491 Content [ ] unchecked; core search pin eshu#10540 minted; satellite W4 termination pending (unmerged drive/9669-w4-satellite) |

## Verification

`bash scripts/check-g2-served-snapshot.sh` at generation commit:

```text
PASS g2-served-snapshot: rows=7 native=3 tierc=0 relay=4 absent=0
```
