// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// npmrcSeeds are the credential values the seeded .npmrc below carries. Each
// sits in a position the registry-URL parser must never read or must strip.
var npmrcSeeds = []string{
	"npmrcSeedAuthToken0123456789",
	"npmrcSeedAuthBase64Value0123456789",
	"npmrcSeedPassword0123456789",
	"npmrcSeedUsername0123456789",
	"npmrc.seed.email0123456789@seed-mail.example",
	"npmrcSeedUserinfoUser0123456789",
	"npmrcSeedUserinfoPass0123456789",
	"npmrcSeedQueryToken0123456789",
	"npmrcSeedFragment0123456789",
	"npmrcSeedPathToken0123456789",
	"npmrcSeedSplitUser0123456789",
	"npmrcSeedSplitPass0123456789",
	"npmrcSeedFuryToken0123456789",
	"npmrcSeedViewToken0123456789",
}

const npmrcSeededBody = "" +
	"# build registry\n" +
	"registry=https://npmrcSeedUserinfoUser0123456789:npmrcSeedUserinfoPass0123456789@registry.example.com/repository/npm/?token=npmrcSeedQueryToken0123456789#npmrcSeedFragment0123456789\n" +
	"@acme:registry=https://npm.acme.example/repository/repo/\n" +
	"registry=https://npmrcSeedSplitUser0123456789:#npmrcSeedSplitPass0123456789@registry.example.com/repository/npm/\n" +
	"registry=https://npmrcSeedSplitUser0123456789:/npmrcSeedSplitPass0123456789@registry.example.com/repository/npm/\n" +
	"@fury:registry=https://npm-proxy.fury.io/npmrcSeedFuryToken0123456789/acme/\n" +
	"@view:registry=https://pkgs.dev.azure.com/org/_packaging/feed@Release/npm/registry/\n" +
	"@vtok:registry=https://pkgs.dev.azure.com/org/_packaging/feed@npmrcSeedViewToken0123456789/npm/registry/\n" +
	"@tok:registry=https://npm.tok.example/t/npmrcSeedPathToken0123456789/npm/\n" +
	"//registry.example.com/:_authToken=npmrcSeedAuthToken0123456789\n" +
	"//registry.example.com/:_auth=npmrcSeedAuthBase64Value0123456789\n" +
	"//registry.example.com/:_password=npmrcSeedPassword0123456789\n" +
	"//registry.example.com/:username=npmrcSeedUsername0123456789\n" +
	"//registry.example.com/:email=npmrc.seed.email0123456789@seed-mail.example\n" +
	"always-auth=true\n"

// npmrcRegistryEntities returns the content_entity payloads the registry-URL
// parser produced for one path, ordered as emitted.
func npmrcRegistryEntities(t *testing.T, envelopes []facts.Envelope, path string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != "content_entity" || envelope.Payload["relative_path"] != path {
			continue
		}
		meta, _ := envelope.Payload["entity_metadata"].(map[string]any)
		if meta["config_kind"] != "registry_url" {
			continue
		}
		row := map[string]any{"name": envelope.Payload["entity_name"], "source_cache": envelope.Payload["source_cache"]}
		for key, value := range meta {
			row[key] = value
		}
		out = append(out, row)
	}
	return out
}

// TestNpmrcSeededCredentialReachesNoFactUnderTheProductionBinding is the
// #8978 acceptance test. The default registry binds .npmrc to the metadata
// policy and the npmrc parser. A full-stream scan of every emitted fact must
// find none of the seeded credentials, the content row must be metadata-only
// with the source digest, and the registry-URL facts must exist beside it.
func TestNpmrcSeededCredentialReachesNoFactUnderTheProductionBinding(t *testing.T) {
	t.Parallel()

	for _, path := range []string{".npmrc", "apps/web/.npmrc"} {
		snapshot, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), path, npmrcSeededBody)

		for _, seed := range npmrcSeeds {
			assertNoSeedInAnyFact(t, envelopes, seed)
			assertNoSeedInContentFacts(t, envelopes, seed)
		}
		for _, key := range []string{"_authToken", "_password", "_auth=", "always-auth"} {
			if kinds := factKindsWithSeed(t, envelopes, key); len(kinds) != 0 {
				t.Errorf("%s: credential key %q reached fact kinds %v", path, key, kinds)
			}
		}

		content := contentFact(t, envelopes, path)
		if content.Payload["content_body"] != "" {
			t.Errorf("%s: content_body = %q, want an empty metadata row", path, content.Payload["content_body"])
		}
		if want := digestForBody(npmrcSeededBody); content.Payload["content_digest"] != want {
			t.Errorf("%s: content_digest = %v, want the source digest %s", path, content.Payload["content_digest"], want)
		}
		if len(snapshot.ContentFileMetas) == 0 || snapshot.ContentFileMetas[0].Digest != digestForBody(npmrcSeededBody) {
			t.Errorf("%s: snapshot meta digest must be the source digest: %+v", path, snapshot.ContentFileMetas)
		}

		urls := npmrcRegistryEntities(t, envelopes, path)
		if len(urls) != 6 {
			t.Fatalf("%s: registry-URL entities = %v, want the registry lines that survive (the split-userinfo lines emit nothing)", path, urls)
		}
		want := map[string]string{
			"registry":       "https://registry.example.com/repository/npm/",
			"@acme:registry": "https://npm.acme.example/repository/repo/",
			// The token-shaped path is withheld; the host still answers.
			"@tok:registry":  "https://npm.tok.example",
			"@fury:registry": "https://npm-proxy.fury.io",
			// The Azure feed view keeps its path with the separator escaped; a token
			// in the view position is withheld.
			"@view:registry": "https://pkgs.dev.azure.com/org/_packaging/feed%40Release/npm/registry/",
			"@vtok:registry": "https://pkgs.dev.azure.com",
		}
		for _, row := range urls {
			name, _ := row["name"].(string)
			if row["value"] != want[name] {
				t.Errorf("%s: %s value = %v, want %q", path, name, row["value"], want[name])
			}
			if cache, _ := row["source_cache"].(string); cache != "" {
				t.Errorf("%s: %s source_cache = %q, want empty under metadata", path, name, cache)
			}
		}

		var fileFact bool
		for _, envelope := range envelopes {
			if envelope.FactKind != "file" || envelope.Payload["relative_path"] != path {
				continue
			}
			fileFact = true
			data, _ := envelope.Payload["parsed_file_data"].(map[string]any)
			if source, has := data["source"]; has && source != "" {
				t.Errorf("%s: parsed_file_data.source = %v, want absent or empty", path, source)
			}
		}
		if !fileFact {
			t.Errorf("%s: no file fact", path)
		}
	}
}

// TestNpmrcScanFindsTheSeedsWhenTheBodyIsStored is the liveness control for
// the scan above: with the same parser but the store policy, the credential
// lines reach the content fact, so the metadata run's clean scan is real.
func TestNpmrcScanFindsTheSeedsWhenTheBodyIsStored(t *testing.T) {
	t.Parallel()

	definitions := parser.DefaultRegistry().Definitions()
	for i := range definitions {
		if definitions[i].ParserKey == "npmrc" {
			definitions[i].ContentStore = parser.ContentStore{}
		}
	}
	registry, err := parser.NewRegistry(definitions)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, envelopes := snapshotWithPolicy(t, registry, ".npmrc", npmrcSeededBody)
	body, _ := contentFact(t, envelopes, ".npmrc").Payload["content_body"].(string)
	for _, seed := range npmrcSeeds[:5] {
		if !strings.Contains(body, seed) {
			t.Errorf("control: stored content_body does not hold %q; the scan would prove nothing", seed)
		}
	}
	// The parser still strips the registry line even when the body is stored.
	for _, seed := range npmrcSeeds[5:] {
		for _, row := range npmrcRegistryEntities(t, envelopes, ".npmrc") {
			if strings.Contains(asString(row["value"]), seed) {
				t.Errorf("control: parser echoed %q in a registry row", seed)
			}
		}
	}
}
