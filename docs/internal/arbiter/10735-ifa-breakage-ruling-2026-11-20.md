# Arbiter ruling: #10735 newest-copy product path vs Ifá contract gate — option (3) SCOPE TRANSFER to #10734's Odù lane

Date: 2026-11-20 (UTC). Issues: eshu-hq/eshu#10735 (branch drive/10735-newest-copy-product-path, commit f2770f4c51 over origin/main 408666d045) vs Ifá contract-layer gate; receiving lane eshu-hq/eshu#10734 (OPEN). Arbiter: arbiter-eshu (read-only; arbiter edited nothing).

Severity: promotion-blocking coordination issue (contract gate red; no code defect). Confidence: high.

## What was inspected (all read-only; no edits made)

Full #10735 body + all 3 comments via gh; #10734 body + state (OPEN) via gh; /tmp/attest-10735/verdict.md incl. row-6 rebase waiver; branch diff origin/main..HEAD; go/internal/reducer/supplychain/core/newest_copy.go, impact_classify.go, typed_decode.go, product_newest_copy_10735_test.go, retraction_product_test.go; go/internal/ifa/vuln_collector_odu_test.go, vuln_collector_determinism_test.go, vuln_collector_odu.go; cassette testdata/cassettes/vulnerabilityintelligence/vuln-collector-nvd.json; docs/internal/design/10734-vulnerability-collector-ifa-odu.md; specs/ifa-coverage-manifest.v1.yaml. Both failing tests re-run on the branch (fail exactly as reported) and on a throwaway origin/main worktree (pass); worktree removed afterwards.

## Root cause (mechanism, undisputed)

The cassette's window2 scope holds ONLY CVE-2026-90001 Rejected @ 2026-11-10T07:00:00.000 with zero affected_product facts, while window1 holds the Analyzed copy @ 2026-11-03T08:30:00.000 plus the widget product. The branch elects window2 (impact_classify.go:52-57, newest_copy.go:282-309), admits only the window2 scope, and drops the window1 product at impact_classify.go:62-63. Result: 0 findings. The "partition" failures are baseline-count mismatches only (determinism_test.go:237; newest-copy subjects 0 want 1, every-window 4 want 5) with no chunk-order digest divergence — the election is order-independent, so partition-independence (I3) is NOT broken.

## Why not (1) implementation defect

The code faithfully executes the binding 2026-11-17 ruling (winner per (source, advisory id) across scopes; keep iff own scope+generation holds an admitted copy; tie-union; the two guards). The ruling keeps R-B10, whose normative content ("a CPE present only in an older NVD copy forms no finding," #10734 body) requires exactly this drop, and the branch deliberately pins the retracted-winner case in "retracted newer admits its own scope only" (product_newest_copy_10735_test.go:224-248). A package-style "winner product-less keeps losers' products" carve-out would contradict the ruling's letter. The all-native-sources deviation is immaterial: both copies are NVD, and narrowing it would not restore the finding.

## Why not (2) stale test expectation by #10735

The substance is real (the assertions encode pre-ruling behavior), but the license bar is unmet. No ruling clause explicitly licenses #10735 to override an Ifá conformance assertion — the 11-17 ruling names only core/storage tests and never mentions Ifá/contract/conformance, while the binding allocation states "The Odù proof of these cases is eshu#10734." #10735's own claim comment scopes it to reducer core + storage and touches no ifa files. #10734's design doc explicitly promises this shape yields a retracted finding (10734 doc table line 252; lines 378-383), so the promise #10735 would break is #10734's owned proof.

## Handoff terms

(a) #10735 keeps reducer behavior + core proof unchanged, makes no ifa edits, and does not merge on a red contract gate.
(b) #10734 reconciles its Odù proof — either expect no finding for the W1+W2 shape (widget CPE is older-copy-only → dropped per kept R-B10), or add a synthetic re-issue window repeating the product so a retracted-finding Odù survives post-ruling. Either way it must update TestVulnCollectorNewestCopyRetracts (vuln_collector_odu_test.go:427-464), determinismSubjects baselines (determinism_test.go:58-82; note the anti-vacuity guard at :104-110 forbids wantFindings 0, so newest-copy subjects must be dropped/replaced, every-window 5→4), the coverage-manifest rows (specs/ifa-coverage-manifest.v1.yaml:72-80), and its design note.
(c) Landing constraint (decisive): the reconciled Ifá expectations fail on current main and pass only on the branch — the mirror of today's tests — so #10734's update cannot land on main first; it must land stacked on / together with #10735 so the contract gate proves the combined behavior in one promotion.
