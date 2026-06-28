// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/eshu-hq/eshu/go/internal/replay/inputtape"
)

// TestAWSSDKClientRecordReplay proves the input tape works through an AWS SDK
// client seam, not only a raw *http.Client. The AWS SDK signs each request
// (SigV4) and accepts a custom aws.Config.HTTPClient; an *http.Client whose
// Transport is the tape RoundTripper satisfies that interface. We record a real
// STS GetCallerIdentity against a fake endpoint, then replay it with the
// endpoint shut down and assert identical output with no SigV4 credential left
// in the tape.
func TestAWSSDKClientRecordReplay(t *testing.T) {
	t.Parallel()

	const callerXML = `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">` +
		`<GetCallerIdentityResult>` +
		`<Arn>arn:aws:iam::123456789012:user/replay-demo</Arn>` +
		`<UserId>AIDADEMOUSERID</UserId>` +
		`<Account>123456789012</Account>` +
		`</GetCallerIdentityResult>` +
		`<ResponseMetadata><RequestId>req-demo-1</RequestId></ResponseMetadata>` +
		`</GetCallerIdentityResponse>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The SDK must have signed the request, proving the recorder saw a live
		// credential that the tape must not retain.
		if auth := r.Header.Get("Authorization"); auth == "" {
			t.Fatalf("expected a signed (Authorization) request from the AWS SDK")
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(callerXML))
	}))

	// STS GetCallerIdentity is a POST with a fixed form body and no query params;
	// SigV4 puts the signature and X-Amz-Date in headers (Authorization is
	// redacted by default), so the request key is stable across runs with no
	// volatile-param config needed here.
	cfg := inputtape.Config{}

	recorder := inputtape.New(cfg)
	recArn := callSTS(t, server.URL, &http.Client{Transport: recorder})
	if recArn != "arn:aws:iam::123456789012:user/replay-demo" {
		t.Fatalf("record arn = %q", recArn)
	}

	tape := recorder.Tape("awscloud")
	canonical, err := inputtape.MarshalTape(tape)
	if err != nil {
		t.Fatalf("marshal tape: %v", err)
	}
	// The static credential id below must not appear; the SigV4 Authorization
	// header (which embeds it) must be redacted.
	if bytes.Contains(canonical, []byte("AKIAREPLAYDEMOKEY")) {
		t.Fatalf("tape leaked access key id:\n%s", canonical)
	}

	// REPLAY with the endpoint shut down.
	server.Close()

	replayer, err := inputtape.NewReplayer(tape, cfg)
	if err != nil {
		t.Fatalf("new replayer: %v", err)
	}
	replayArn := callSTS(t, server.URL, &http.Client{Transport: replayer})
	if replayArn != recArn {
		t.Fatalf("replay arn = %q, want %q", replayArn, recArn)
	}
}

// callSTS issues one GetCallerIdentity through an STS client whose HTTP client is
// httpClient, returning the resolved ARN. The endpoint is pinned to baseURL and
// a static credential is used so the SDK signs deterministically against the
// fake server.
func callSTS(t *testing.T, baseURL string, httpClient *http.Client) string {
	t.Helper()
	client := sts.New(sts.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(baseURL),
		HTTPClient:   httpClient,
		Credentials: credentials.NewStaticCredentialsProvider(
			"AKIAREPLAYDEMOKEY", "replay-demo-secret-key", "",
		),
	})
	out, err := client.GetCallerIdentity(context.Background(), &sts.GetCallerIdentityInput{})
	if err != nil {
		t.Fatalf("GetCallerIdentity: %v", err)
	}
	if out.Arn == nil {
		t.Fatalf("nil ARN")
	}
	return *out.Arn
}
