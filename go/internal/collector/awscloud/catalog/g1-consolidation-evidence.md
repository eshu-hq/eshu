# G1 Service-Constant Catalog Consolidation Evidence (#6696)

G1 opener: this note records the lane G1 move of ten
`constants_<service>.go` root files (appmesh, appstream, workspaces,
elasticbeanstalk, dms, imagebuilder, verifiedaccess, transfer, cognito,
amp — 137 string constants) into the `catalog` leaf as
`catalog/g1_services.go` (service mesh, streaming, desktops, and app
hosting) and `catalog/g1_platform.go` (migration, image build, access,
file transfer, identity, and monitoring), verbatim apart from the package
clause. The split is a size measure only: one file would pass the 500-line
Go cap at roughly 760 lines, so the category ships as two files (327 and
419 lines), both under the cap.

No-Regression Evidence (g1 slice): the consolidation moves declarations
only — every value is pinned byte-identical by
`TestG1ConstantsMatchAWSCloudValues` (137/137) in
`catalog/g1_services_test.go`, and `go test` across the collector tree
plus `go build ./...` and `go vet ./...` are green after the repoint
(86 referencing files change only the qualifier from `awscloud.` to
`catalog.`, plus the leaf import, dropping the root import where it goes
unused). Baseline: the pre-move values read from the base tree for each
of the ten root files. Backend/version: none — pure compile-time
constant relocations with no query, graph write, queue, lease, worker,
batching, or Compose/Helm change, so there are no row counts to regress.
Terminal evidence: the pin test passes at the final head and the
redaction-derivation and relguard disk walks already generalized to every
catalog category file pass with both new leaves present.

No-Observability-Change (g1 slice): the leaf declares no instrument and
runs no stage; scanner emission stays covered by
`eshu_dp_aws_resources_emitted_total` and
`eshu_dp_aws_scan_duration_seconds` (AWS cloud row in
`docs/public/observability/telemetry-coverage.md`). Safe because the Go
compiler resolves every moved reference, the value-pinning test fails on
any drift, and the relguard/redaction disk-walk derivations already cover
the catalog directory.
