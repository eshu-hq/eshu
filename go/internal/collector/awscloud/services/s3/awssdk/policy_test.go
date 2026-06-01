package awssdk

import "testing"

func TestDeriveBucketPolicyFlags(t *testing.T) {
	cases := []struct {
		name             string
		ownerAccountID   string
		document         string
		wantPublic       *bool
		wantCrossAccount *bool
	}{
		{
			name:           "wildcard principal is public",
			ownerAccountID: "123456789012",
			document: `{"Version":"2012-10-17","Statement":[
				{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(true),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "aws wildcard principal is public",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(true),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "cross-account principal",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::999988887777:root"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(true),
		},
		{
			name:           "same-account principal is neither public nor cross-account",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/app"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "deny wildcard is not a public grant",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(false),
		},
		{
			name:           "bare account id principal cross-account",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"AWS":["999988887777","123456789012"]},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(true),
		},
		{
			name:           "service principal is neither public nor cross-account",
			ownerAccountID: "123456789012",
			document: `{"Statement":[
				{"Effect":"Allow","Principal":{"Service":"cloudtrail.amazonaws.com"},"Action":"s3:PutObject","Resource":"arn:aws:s3:::b/*"}]}`,
			wantPublic:       boolPtr(false),
			wantCrossAccount: boolPtr(false),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			public, crossAccount, err := deriveBucketPolicyFlags(tc.document, tc.ownerAccountID)
			if err != nil {
				t.Fatalf("deriveBucketPolicyFlags() error = %v, want nil", err)
			}
			assertBoolPtr(t, "public", public, tc.wantPublic)
			assertBoolPtr(t, "crossAccount", crossAccount, tc.wantCrossAccount)
		})
	}
}

func TestDeriveBucketPolicyFlagsHandlesURLEncodedDocument(t *testing.T) {
	encoded := "%7B%22Statement%22%3A%5B%7B%22Effect%22%3A%22Allow%22%2C%22Principal%22%3A%22*%22%2C%22Action%22%3A%22s3%3AGetObject%22%7D%5D%7D"
	public, crossAccount, err := deriveBucketPolicyFlags(encoded, "123456789012")
	if err != nil {
		t.Fatalf("deriveBucketPolicyFlags() error = %v, want nil", err)
	}
	assertBoolPtr(t, "public", public, boolPtr(true))
	assertBoolPtr(t, "crossAccount", crossAccount, boolPtr(false))
}

func TestDeriveBucketPolicyFlagsRejectsMalformedDocument(t *testing.T) {
	_, _, err := deriveBucketPolicyFlags("{not json", "123456789012")
	if err == nil {
		t.Fatalf("deriveBucketPolicyFlags() error = nil, want parse error for malformed document")
	}
}

func assertBoolPtr(t *testing.T, label string, got, want *bool) {
	t.Helper()
	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Fatalf("%s = %v, want %v", label, ptrStr(got), ptrStr(want))
	case *got != *want:
		t.Fatalf("%s = %v, want %v", label, *got, *want)
	}
}

func ptrStr(value *bool) string {
	if value == nil {
		return "nil"
	}
	if *value {
		return "true"
	}
	return "false"
}

func boolPtr(value bool) *bool {
	return &value
}
