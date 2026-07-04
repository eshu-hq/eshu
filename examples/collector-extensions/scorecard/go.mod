module github.com/eshu-hq/eshu/examples/collector-extensions/scorecard

go 1.26.0

require (
	github.com/eshu-hq/eshu/sdk/go/collector v0.0.0
	github.com/eshu-hq/eshu/sdk/go/factschema v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/eshu-hq/eshu/sdk/go/collector => ../../../sdk/go/collector

// An external collector replaces this with a real pinned version:
//   require github.com/eshu-hq/eshu/sdk/go/factschema vX.Y.Z
// where vX.Y.Z is the fixture-pack tag (the factschema module version). In this
// monorepo every example resolves the SDK modules by path, the same as the
// collector require above, so the pack is exercised through its real module
// import path without a published tag.
replace github.com/eshu-hq/eshu/sdk/go/factschema => ../../../sdk/go/factschema
