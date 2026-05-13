package awssdk

import (
	"testing"

	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
)

func TestParseTrustPolicyIncludesWildcardStringPrincipal(t *testing.T) {
	_, principals, err := parseTrustPolicy(`{"Version":"2012-10-17","Statement":{"Effect":"Allow","Principal":"*","Action":"sts:AssumeRole"}}`)
	if err != nil {
		t.Fatalf("parseTrustPolicy() error = %v", err)
	}
	want := iamservice.TrustPrincipal{Type: "AWS", Identifier: "*"}
	if len(principals) != 1 || principals[0] != want {
		t.Fatalf("principals = %#v, want %#v", principals, []iamservice.TrustPrincipal{want})
	}
}
