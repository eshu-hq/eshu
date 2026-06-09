module github.com/eshu-hq/eshu/examples/collector-extensions/scorecard

go 1.26.0

require (
	github.com/eshu-hq/eshu/sdk/go/collector v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/eshu-hq/eshu/sdk/go/collector => ../../../sdk/go/collector
