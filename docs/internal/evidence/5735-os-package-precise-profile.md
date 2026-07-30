# OS Package Precise-Profile Admission Evidence (#5735)

Added by `6e212174d5` (#5735) as an in-place addition to
`go/internal/reducer/README.md`'s "Detection profile is recorded" /
"OS package evidence is vendor-gated" bullets, after that README was
split into sibling docs (issue #5786). Rescued here verbatim during
the rebase onto that commit so the content and its evidence markers
are not lost; see
[`gotchas-supply-chain-and-vulnerabilities.md`](../../../go/internal/reducer/gotchas-supply-chain-and-vulnerabilities.md)
for the bullets this extends.

Vendor-backed RPM, Debian/dpkg, and Alpine/APK findings qualify as
precise when an exact affected-version or exact known-fixed boundary
matches; OS-package range reasons remain comprehensive.

No-Regression Evidence (#5735): `go test ./internal/reducer -run
'^(TestSupplyChainImpactExactOSPackageReasonsQualifyForPreciseProfile|TestGoldenCassetteDebianOSPackageChainSynthesizesFinding|TestBuildSupplyChainImpactFindingsUsesCollectorShapedOSVDebianAndAPKRanges|TestBuildSupplyChainImpactFindingsUsesVendorAPKOSPackageEvidence|TestBuildSupplyChainImpactFindingsUsesExactAPKKnownFixedProfile)$'
-count=1` proves exact affected-version and exact known-fixed DPKG/APK
decisions are precise through the production dispatcher. The golden
cassette's exact DPKG affected-version decision is precise, while the
collector-shaped OSV matrix keeps DPKG/APK range decisions comprehensive.
On Go 1.26.5, darwin/arm64, the before/after
`BenchmarkEvaluateOSPackageVersionMatchAndClassify` comparison remained
allocation-free and within 2.1% on both measured paths (exact affected:
415.9 ns to 421.9 ns; exact known-fixed: 273.6 ns to 279.4 ns), below the
reducer's 10% no-material-regression band.
No-Observability-Change: this changes only the in-memory evidence-tier
classifier and exact-boundary reason emitted in the existing finding
payload. It adds no queue, graph write, runtime knob, metric instrument,
metric label, span, or log field.
