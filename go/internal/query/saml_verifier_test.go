package query

import (
	"encoding/base64"
	"testing"
)

func TestRequestIDFromSAMLResponseExtractsInResponseTo(t *testing.T) {
	t.Parallel()

	responseXML := `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-1" InResponseTo="request-1"></Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))

	got, err := requestIDFromSAMLResponse(encoded)
	if err != nil {
		t.Fatalf("requestIDFromSAMLResponse() error = %v", err)
	}
	if got != "request-1" {
		t.Fatalf("request id = %q, want request-1", got)
	}
}

func TestResponseEnvelopeIDsFromSAMLResponseExtractsRequestAndResponseIDs(t *testing.T) {
	t.Parallel()

	responseXML := `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-1" InResponseTo="request-1"></Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))

	requestID, responseID, err := responseEnvelopeIDsFromSAMLResponse(encoded)
	if err != nil {
		t.Fatalf("responseEnvelopeIDsFromSAMLResponse() error = %v", err)
	}
	if requestID != "request-1" || responseID != "response-1" {
		t.Fatalf("ids = %q/%q, want request-1/response-1", requestID, responseID)
	}
}

func TestRequestIDFromSAMLResponseRejectsMissingInResponseTo(t *testing.T) {
	t.Parallel()

	responseXML := `<Response xmlns="urn:oasis:names:tc:SAML:2.0:protocol" ID="response-1"></Response>`
	encoded := base64.StdEncoding.EncodeToString([]byte(responseXML))

	if got, err := requestIDFromSAMLResponse(encoded); err == nil {
		t.Fatalf("requestIDFromSAMLResponse() = %q, nil error; want error", got)
	}
}
