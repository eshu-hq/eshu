package terraformstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserResourceKeysIncludeTerraformMode(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_ami","name":"ubuntu","instances":[{"attributes":{"id":"ami-1"}}]},
		{"mode":"data","type":"aws_ami","name":"ubuntu","instances":[{"attributes":{"id":"ami-2"}}]}
	]}`

	result := parseFixtureFacts(t, state)
	keys := stableKeysByKind(result, facts.TerraformStateResourceFactKind)
	if len(keys) != 2 {
		t.Fatalf("resource stable keys count = %d, want 2: %#v", len(keys), keys)
	}
	if keys[0] == keys[1] {
		t.Fatalf("managed and data resource stable keys collided: %#v", keys)
	}
	if !containsStableKey(keys, "terraform_state_resource:resource:aws_ami.ubuntu") {
		t.Fatalf("managed resource key missing from %#v", keys)
	}
	if !containsStableKey(keys, "terraform_state_resource:resource:data.aws_ami.ubuntu") {
		t.Fatalf("data resource key missing from %#v", keys)
	}
}

func TestParserSensitiveCompositeOutputEmitsShapeNotNilMarker(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","outputs":{
		"credentials":{"sensitive":true,"value":{"username":"admin","password":"secret"}}
	}}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), parseFixtureOptions(t))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	output := factByKind(t, result.Facts, facts.TerraformStateOutputFactKind)
	if _, ok := output.Payload["value"]; ok {
		t.Fatalf("output value = %#v, want omitted for sensitive composite", output.Payload["value"])
	}
	if got, want := output.Payload["value_shape"], "composite"; got != want {
		t.Fatalf("output value_shape = %#v, want %q", got, want)
	}
	requireFactKinds(t, result.Facts, facts.TerraformStateWarningFactKind)
	assertNoRawSecret(t, result.Facts, "secret")
}

func containsStableKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}
