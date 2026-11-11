<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# cve_list scan-gate admission evidence

`supportedTrustedAdvisorySourceClass` admits `cve_list` (#10546) so
`validateAuthorizedScanImpactEvidence` accepts scan targets whose advisory
generation cites a `cve_list` authority, matching the storage
`authorizedImpactExpectedKinds` admission. Any other class still fails closed
before authority matching.

No-Regression Evidence (#10546):
`BenchmarkDerivedScanPartitionTheory/authorized_shared_1` (one target, one
shared two-envelope advisory slice plus one os-package/analysis pair,
linux/arm64, Go 1.26.10, three 100-iteration samples) measured 41983/40657/37800
ns/op, ~34400 B/op, and 288 allocs/op at `origin/main` (`76a7efae`) and
42149/40138/40422 ns/op, ~34500 B/op, and 288 allocs/op after: allocation
behavior is identical and bytes/op match within noise. The change adds one
constant case label to the source-class switch on the already-fail-closed
validation path, with no new backend call, queue stage, lock, or graph write.

No-Observability-Change (#10546): no new metric, span, log key, or queue
stage. Rejections keep the existing `unsupported trusted advisory source
class` error.
