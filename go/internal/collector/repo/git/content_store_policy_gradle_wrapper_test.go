// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// wrapperSeeds are credential-shaped values planted in the seeded wrapper
// file below. They are built by concatenation so no secret-looking literal sits
// in the source file (GitHub push protection).
var wrapperSeeds = []string{
	"gradleWrapper" + "SeedUser0123456789",
	"gradleWrapper" + "SeedPass0123456789",
	"gradleWrapper" + "SeedQuery0123456789",
	"gradleWrapper" + "SeedFragment0123456789",
	"gradleWrapper" + "SeedOther0123456789",
}

var wrapperSeededBody = "distributionBase=GRADLE_USER_HOME\n" +
	"distributionPath=wrapper/dists\n" +
	"distributionUrl=https\\://" + wrapperSeeds[0] + ":" + wrapperSeeds[1] +
	"@repo.example.com/gradle/distributions/gradle-8.5-bin.zip?token=" + wrapperSeeds[2] + "#" + wrapperSeeds[3] + "\n" +
	"repoPassword=" + wrapperSeeds[4] + "\n"

// wrapperDistributionEntities returns the content_entity payloads the wrapper
// parser produced for one path.
func wrapperDistributionEntities(envelopes []facts.Envelope, path string) []map[string]any {
	var out []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind != "content_entity" || envelope.Payload["relative_path"] != path {
			continue
		}
		meta, _ := envelope.Payload["entity_metadata"].(map[string]any)
		if meta["config_kind"] != "gradle_distribution" {
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

// TestGradleWrapperSeededCredentialReachesNoFactUnderTheProductionBinding is
// the #8822 acceptance test. The default registry binds
// gradle-wrapper.properties to the metadata policy and the wrapper parser. A
// full-stream scan of every emitted fact must find none of the seeded values,
// the content row must be metadata-only with the source digest, and the
// distribution fact must exist beside it.
func TestGradleWrapperSeededCredentialReachesNoFactUnderTheProductionBinding(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"gradle/wrapper/gradle-wrapper.properties", "apps/api/gradle/wrapper/gradle-wrapper.properties"} {
		snapshot, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), path, wrapperSeededBody)

		for _, seed := range wrapperSeeds {
			assertNoSeedInAnyFact(t, envelopes, seed)
			assertNoSeedInContentFacts(t, envelopes, seed)
		}
		for _, key := range []string{"repoPassword", "repo.example.com"} {
			if kinds := factKindsWithSeed(t, envelopes, key); len(kinds) != 0 {
				t.Errorf("%s: %q reached fact kinds %v", path, key, kinds)
			}
		}

		content := contentFact(t, envelopes, path)
		if content.Payload["content_body"] != "" {
			t.Errorf("%s: content_body = %q, want an empty metadata row", path, content.Payload["content_body"])
		}
		if want := digestForBody(wrapperSeededBody); content.Payload["content_digest"] != want {
			t.Errorf("%s: content_digest = %v, want the source digest %s", path, content.Payload["content_digest"], want)
		}
		if len(snapshot.ContentFileMetas) == 0 || snapshot.ContentFileMetas[0].Digest != digestForBody(wrapperSeededBody) {
			t.Errorf("%s: snapshot meta digest must be the source digest: %+v", path, snapshot.ContentFileMetas)
		}

		rows := wrapperDistributionEntities(envelopes, path)
		if len(rows) != 1 {
			t.Fatalf("%s: distribution entities = %v, want exactly one", path, rows)
		}
		if rows[0]["value"] != "8.5" || rows[0]["distribution_type"] != "bin" {
			t.Errorf("%s: distribution row = %v, want gradle 8.5 bin", path, rows[0])
		}
		if cache, _ := rows[0]["source_cache"].(string); cache != "" {
			t.Errorf("%s: source_cache = %q, want empty under metadata", path, cache)
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

// TestGradleWrapperScanFindsTheSeedsWhenTheBodyIsStored is the liveness
// control for the scan above: with the same parser but the store policy, the
// credential lines reach the content fact, so the metadata run's clean scan is
// real. The parser still emits only the version row.
func TestGradleWrapperScanFindsTheSeedsWhenTheBodyIsStored(t *testing.T) {
	t.Parallel()

	definitions := parser.DefaultRegistry().Definitions()
	for i := range definitions {
		if definitions[i].ParserKey == "gradle_wrapper" {
			definitions[i].ContentStore = parser.ContentStore{}
		}
	}
	registry, err := parser.NewRegistry(definitions)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	const path = "gradle/wrapper/gradle-wrapper.properties"
	_, envelopes := snapshotWithPolicy(t, registry, path, wrapperSeededBody)
	body, _ := contentFact(t, envelopes, path).Payload["content_body"].(string)
	for _, seed := range []string{wrapperSeeds[0], wrapperSeeds[1], wrapperSeeds[4]} {
		if !strings.Contains(body, seed) {
			t.Errorf("control: stored content_body does not hold %q; the scan would prove nothing", seed)
		}
	}
	for _, row := range wrapperDistributionEntities(envelopes, path) {
		for _, seed := range wrapperSeeds {
			if strings.Contains(asString(row["value"]), seed) {
				t.Errorf("control: parser echoed %q in a distribution row", seed)
			}
		}
	}
}
