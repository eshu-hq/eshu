// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/scope"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
	factschema "github.com/eshu-hq/eshu/sdk/go/factschema"
	"github.com/eshu-hq/eshu/sdk/go/factschema/fixturepack"
)

func TestOCINumberReachesClaimedTypedConsumer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  string
		patch func(map[string]any)
		check func(*testing.T, factschema.Envelope)
	}{
		{
			name: "scalar", kind: factschema.FactKindWorkItemProjectMetadata,
			patch: func(p map[string]any) { p["issue_count"] = json.Number("9007199254740993") },
			check: func(t *testing.T, env factschema.Envelope) {
				t.Helper()
				if n, ok := env.Payload["issue_count"].(json.Number); !ok || n != "9007199254740993" {
					t.Fatalf("preserved issue count = %T %v", env.Payload["issue_count"], env.Payload["issue_count"])
				}
				v, err := factschema.DecodeWorkItemProjectMetadata(env)
				if err != nil || v.IssueCount == nil || *v.IssueCount != 9007199254740993 {
					t.Fatalf("typed issue count = %v, %v", v.IssueCount, err)
				}
			},
		},
		{
			name: "nested", kind: factschema.FactKindKubernetesLivePodTemplate,
			patch: func(p map[string]any) {
				p["containers"] = []any{map[string]any{"ports": []any{json.Number("8e1")}}}
			},
			check: func(t *testing.T, env factschema.Envelope) {
				t.Helper()
				containers, ok := env.Payload["containers"].([]any)
				if !ok || len(containers) != 1 {
					t.Fatalf("containers = %T %v", env.Payload["containers"], env.Payload["containers"])
				}
				ports, ok := containers[0].(map[string]any)["ports"].([]any)
				if !ok || len(ports) != 1 {
					t.Fatalf("ports = %v", containers[0])
				}
				if n, ok := ports[0].(json.Number); !ok || n != "8e1" {
					t.Fatalf("preserved port = %T %v", ports[0], ports[0])
				}
				v, err := factschema.DecodeKubernetesLivePodTemplate(env)
				if err != nil || len(v.Containers) != 1 || len(v.Containers[0].Ports) != 1 || v.Containers[0].Ports[0] != 80 {
					t.Fatalf("typed containers = %+v, %v", v.Containers, err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := testWorkItem()
			item.LeaseExpiresAt = time.Now().Add(time.Minute)
			fact := testSDKFact(item)
			fact.Kind = tc.kind
			payload, ok := fixturepack.ValidPayload(tc.kind)
			if !ok {
				t.Fatal("missing typed fixture")
			}
			tc.patch(payload)
			fact.Payload = payload
			manifest := testManifest()
			manifest.Spec.EmittedFacts[0].Kind = tc.kind
			manifest.Spec.EmittedFacts[0].PayloadSchemaRef = tc.kind
			grants := []component.ProducerGrant{{
				ProducerID: manifest.Metadata.ID, Version: manifest.Metadata.Version,
				Kind: tc.kind, SchemaVersions: []string{"1.0.0"}, Scope: "repo",
				ExpiresAt: time.Now().Add(time.Hour).UTC(),
			}}
			runner := OCIRunner{
				ImageRef: ociTestDigest,
				exec: func(_ context.Context, _ string, _ []string, stdin []byte, stdout, _ io.Writer) error {
					var request Request
					if err := json.Unmarshal(stdin, &request); err != nil {
						return err
					}
					result := completeResult(item, fact)
					result.Claim = request.Claim
					return json.NewEncoder(stdout).Encode(result)
				},
			}
			source, err := NewSource(Config{
				Manifest: manifest, CollectorInstanceID: item.CollectorInstanceID,
				ScopeKind: scope.KindRepository, ConfigHandle: "cfg-scorecard",
				Config: map[string]any{"fixture": "scorecard"}, Runner: runner,
				Clock: testObservedAt, Grants: grants,
			})
			if err != nil {
				t.Fatal(err)
			}
			collected, ok, err := source.NextClaimed(context.Background(), item)
			if err != nil || !ok {
				t.Fatalf("NextClaimed ok=%t error=%v", ok, err)
			}
			envelopes := collectFacts(t, collected)
			if len(envelopes) != 1 {
				t.Fatalf("envelopes = %d", len(envelopes))
			}
			tc.check(t, factenvelope.FactSchemaFromInternal(envelopes[0].Clone()))
		})
	}
}

func TestSharedSDKResultDecoderRejectsMalformedInput(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"empty", "", "decode extension result:"},
		{"malformed", "{", "decode extension result:"},
		{"unknown", `{"unexpected":1}`, "unknown field"},
		{"second result", `{} {}`, "trailing JSON value"},
		{"trailing junk", `{} junk`, "decode extension result trailer:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeSDKResult([]byte(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decodeSDKResult error = %v, want %q", err, tc.want)
			}
		})
	}
	result, err := decodeSDKResult(bytes.TrimSpace([]byte(`{"protocol_version":"` + sdkcollector.ProtocolVersionV1Alpha1 + `","state":"complete"}`)))
	if err != nil || result.State != sdkcollector.ResultComplete {
		t.Fatalf("valid result = %+v, %v", result, err)
	}
}
