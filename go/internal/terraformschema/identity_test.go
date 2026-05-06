package terraformschema

import "testing"

func TestInferIdentityKeysFindsNameAttribute(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"name":  {Type: "string"},
		"scope": {Type: "string"},
	})
	assertStringSliceEqual(t, keys, []string{"name"})
}

func TestInferIdentityKeysFindsFunctionName(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"function_name": {Type: "string"},
		"runtime":       {Type: "string"},
	})
	assertStringSliceEqual(t, keys, []string{"function_name"})
}

func TestInferIdentityKeysFindsClusterIdentifier(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"cluster_identifier": {Type: "string"},
		"engine":             {Type: "string"},
	})
	assertStringSliceEqual(t, keys, []string{"cluster_identifier"})
}

func TestInferIdentityKeysFindsServiceName(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"service_name":             {Type: "string"},
		"auto_deployments_enabled": {Type: "bool"},
	})
	assertStringSliceEqual(t, keys, []string{"service_name"})
}

func TestInferIdentityKeysFallsBackToSuffixName(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"custom_thing_name": {Type: "string"},
		"enabled":           {Type: "bool"},
	})
	assertStringSliceEqual(t, keys, []string{"custom_thing_name"})
}

func TestInferIdentityKeysSortsFallbackKeys(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"zeta_identifier":  {Type: "string"},
		"alpha_name":       {Type: "string"},
		"middle_name":      {Type: "string"},
		"replica_count":    {Type: "number"},
		"deployment_label": {Type: "string"},
	})
	assertStringSliceEqual(t, keys, []string{"alpha_name", "middle_name", "zeta_identifier"})
}

func TestInferIdentityKeysReturnsEmptyForNoNameAttributes(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"cidr_block":         {Type: "string"},
		"enable_dns_support": {Type: "bool"},
	})
	assertStringSliceEqual(t, keys, nil)
}

func TestInferIdentityKeysIgnoresNonStringName(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"name":  {Type: "bool"},
		"count": {Type: "number"},
	})
	assertStringSliceEqual(t, keys, nil)
}

func TestInferIdentityKeysSkipsComplexTypes(t *testing.T) {
	keys := InferIdentityKeys(map[string]AttributeSchema{
		"protocols":     {Type: []any{"set", "string"}},
		"endpoint_type": {Type: "string"},
	})
	assertStringSliceEqual(t, keys, nil)
}

func assertStringSliceEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(keys) = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys[%d] = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}
