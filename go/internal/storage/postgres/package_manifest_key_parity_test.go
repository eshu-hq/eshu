// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

// The sidecar writer and the reducer's in-memory consumption builder must turn
// the same manifest row into the same key set, for ASCII names and for
// non-ASCII lookalikes alike (#9584). The reducer's key join is the accuracy
// gate behind the sidecar prefilter; if the two folded differently, a row the
// sidecar never keyed could not be found, or a key the gate rejects would be
// read.
func TestManifestConsumptionKeysMatchReducerBuilderForMixedCorpus(t *testing.T) {
	t.Parallel()

	corpus := []struct{ name, packageManager, namespace string }{
		{"Serilog", "nuget", ""},
		{"Ser\u0130log", "nuget", ""},
		{"Serilog.Sinks.\u212Aafka", "dotnet", ""},
		{"React-DOM", "npm", ""},
		{"@Babel/Core", "npm", ""},
		{"Core", "npm", "@Babel"},
		{"\u212Aoa", "npm", ""},
		{"Friendly_Bard...Plugin", "python", ""},
		{"Dj\u0130ngo", "pypi", ""},
		{"requests", "pyp\u0130", ""},
		{"github.com/BurntSushi/toml", "go", ""},
		{"github.com/Burnt\u0130ushi/toml", "gomod", ""},
		{"org.apache.maven:maven-core", "maven", ""},
		{"com.Google.Guava:Guava", "gradle", ""},
		{"org.example:\u0130mpl", "maven", ""},
		{"Symfony/Console", "composer", ""},
		{"Rails", "rubygems", ""},
		{"Ra\u0130ls", "gem", ""},
		{"Serde_JSON", "cargo", ""},
		{"Phoenix", "hex", ""},
		{"Flutter_Bloc", "pub", ""},
		{"Str\u00E4\u00DFe", "cargo", ""},
	}
	envelopes := make([]facts.Envelope, 0, len(corpus))
	for index, entry := range corpus {
		metadata := map[string]any{"config_kind": "dependency", "package_manager": entry.packageManager}
		if entry.namespace != "" {
			metadata["namespace"] = entry.namespace
		}
		envelope := manifestConsumptionTestEnvelope("fact-"+string(rune('a'+index)), false, map[string]any{
			"entity_type": "Variable", "repo_id": "repository:app", "entity_name": entry.name, "entity_metadata": metadata,
		})
		envelope.FactKind = "content_entity"
		envelopes = append(envelopes, envelope)
	}

	sidecar := map[string][]string{}
	for _, row := range packageManifestConsumptionKeyRows(envelopes) {
		sidecar[row.FactID] = append(sidecar[row.FactID], row.Ecosystem+"\x00"+row.PackageName)
	}
	reducer := map[string][]string{}
	for _, dependency := range correlation.ExtractPackageManifestDependencies(envelopes) {
		names := []string{dependency.DependencyName}
		if dependency.PackageNamespace != "" {
			names = append(names, strings.TrimSpace(dependency.PackageNamespace)+"/"+dependency.DependencyName)
		}
		if keys := dependency.ConsumptionKeys(names...); len(keys) > 0 {
			reducer[dependency.FactID] = keys
		}
	}
	for factID := range sidecar {
		sort.Strings(sidecar[factID])
	}
	if !reflect.DeepEqual(sidecar, reducer) {
		t.Fatalf("sidecar keys and reducer builder keys differ\nsidecar: %q\nreducer: %q", sidecar, reducer)
	}
	for factID, keys := range sidecar {
		for _, key := range keys {
			if key == "nuget\x00serilog" && factID != "fact-a" {
				t.Fatalf("fact %s (a lookalike) was keyed as the ASCII Serilog key", factID)
			}
		}
	}
}
