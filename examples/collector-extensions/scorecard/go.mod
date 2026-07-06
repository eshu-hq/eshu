module github.com/eshu-hq/eshu/examples/collector-extensions/scorecard

go 1.26.0

require (
	github.com/eshu-hq/eshu/sdk/go/collector v0.0.0
	github.com/eshu-hq/eshu/sdk/go/factschema v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

// Both replace directives are a monorepo-only stand-in for a real version
// pin. An external collector deletes both replace lines and instead requires
// released versions directly, e.g.:
//   require github.com/eshu-hq/eshu/sdk/go/collector vX.Y.Z
//   require github.com/eshu-hq/eshu/sdk/go/factschema vX.Y.Z
// where the factschema vX.Y.Z is also the fixture-pack version. See
// README.md ("Pinning story") and docs/public/extend/sdk-compatibility.md
// for the versions to pin together.
replace github.com/eshu-hq/eshu/sdk/go/collector => ../../../sdk/go/collector

replace github.com/eshu-hq/eshu/sdk/go/factschema => ../../../sdk/go/factschema
