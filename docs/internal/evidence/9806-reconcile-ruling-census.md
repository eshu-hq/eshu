# #9806 Ruling Reconciliation: Live-Record Census Rerun

Method: throwaway probe `TestZZReconcileCensus` (run, diffed, then deleted;
never part of the deliverable) that classifies five committed OSV OS fixture
records through the production chain below the dormant typed decode —
converter tagger (`OSVRecordEnvelopesReport`), release selection boundary
(`selectOSAdvisoryRelease`), matcher (`evaluateSupplyChainVersionMatch`) —
over a listed/unlisted host matrix with at-fix and above-all observed
versions. The probe decodes the converter payload maps directly, so both trees
see identical tagged facts.

- Base: `origin/main` at `c367d6c996` (#9927 behavior).
- Head: `drive/9806-reconcile-ruling` at the reconciliation commit.
- Corpus: `debian-cve-2023-38039`, `debian-cve-2003-1605`,
  `alpine-cve-2023-27537`, `ubuntu-cve-2016-5419`, `ubuntu-cve-2022-27775`
  (2 Debian, 1 Alpine, 2 Ubuntu) × listed lines + 2 unlisted lines each ×
  each listed fix and one above-all observed version. 101 cells.

## Flip counts (base -> head)

| Base verdict | Head verdict | Cells |
| --- | --- | --- |
| `not_affected_known_fixed` / `dpkg_known_fixed_unlisted_release` | `possibly_affected` / `os_release_not_in_advisory` | 14 |
| `possibly_affected` / `os_release_unlisted` | `possibly_affected` / `os_release_not_in_advisory` | 6 |

## Stability (identical on both trees)

- `affected_exact` / `apk_affected_range`: 3 cells.
- `not_affected_known_fixed` / `apk_exact_known_fixed`: 2 cells.
- `not_affected_known_fixed` / `apk_known_fixed`: 25 cells.
- `not_affected_known_fixed` / `dpkg_exact_known_fixed`: 4 cells.
- `not_affected_known_fixed` / `dpkg_known_fixed`: 18 cells.
- `possibly_affected` / `os_release_ambiguous`: 29 cells.

No cell moved except the 20 retired-verdict flips above: zero regressions in
matched, exact, ambiguous, and range-derived known-fixed reads.

## Scope limits

- The typed decode still drops `releases` until the SDK pin, so live facts stay
  untagged and read the old rules; this census measures the tagged world the
  ruling governs (the world the pin fixtures will land in).
- Contained-version rows are pinned by the committed reconcile tests, not by
  this census (the observed matrix uses at-fix and above-all versions only).
- ESM pocket behavior is pinned by the converter vector table and the reducer
  pocket test; no committed fixture carries a pocket tag yet.
