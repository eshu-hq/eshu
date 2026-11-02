// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"

	parserjson "github.com/eshu-hq/eshu/go/internal/parser/json"
)

// resolverSwiftPin is one remote source-control pin of a Package.resolved file.
// The identity is what SwiftPM writes there: the location's final path segment,
// lower-cased, with no ".git".
type resolverSwiftPin struct {
	location, identity, version string
}

// resolverSwiftPins are the pins behind the resolver fixture's swift rows. They
// reach the store only through [resolverParsedSwiftRows], so the entity name
// and metadata are what the production parser writes, never a hand-written
// spelling.
var resolverSwiftPins = []resolverSwiftPin{
	{"https://user:tok@github.com/apple/swift-nio.git?x=1", "swift-nio", "2.0.0"},
	{"https://github.com/pytorch/executorch", "executorch", "0.1.0"},
	{"git@github.com:vapor/vapor.git?ref=main", "vapor", "4.0.0"},
	{"ssh://git@github.com/grpc/grpc-swift.git", "grpc-swift", "1.0.0"},
	{"https://github.com/marmelroy/zip/", "zip", "2.1.0"},
	{"git://github.com/migueldeicaza/SwiftTerm", "swiftterm", "1.0.0"},
	{"https://github.com/apple/swift-crypto.git", "swift-crypto", "3.0.0"},
	{"https://github.com/emqx/CocoaMQTT", "cocoamqtt", "2.1.0"},
	// These identities keep the suffix, so the entity name cannot stand in for
	// the repository key.
	{"https://github.com/upper/Case.GIT", "case.git", "1.0.0"},
	{"https://github.com/upper/Mixed.Git", "mixed.git", "1.0.0"},
}

// resolverSwiftURLFeedNames is every distinct package name the OSV SwiftURL
// export spells, literally: 30 host/path names and 3 bare ones.
var resolverSwiftURLFeedNames = []string{
	"CocoaMQTT", "swift-crypto", "swift-nio-http2",
	"github.com/apple/container", "github.com/apple/containerization", "github.com/apple/swift-asn1",
	"github.com/apple/swift-corelibs-foundation", "github.com/apple/swift-nio", "github.com/apple/swift-nio-extras",
	"github.com/apple/swift-nio-http2", "github.com/apple/swift-nio-ssl", "github.com/awslabs/aws-sdk-swift",
	"github.com/beatt83/jose-swift", "github.com/facebook/zstd", "github.com/grpc/grpc-swift",
	"github.com/ibireme/yyjson", "github.com/ionic-team/capacitor-swift-pm", "github.com/marmelroy/Zip",
	"github.com/migueldeicaza/SwiftTerm", "github.com/mongodb/mongo-swift-driver", "github.com/pubnub/swift",
	"github.com/pytorch/executorch", "github.com/shareup/wasm-interpreter-apple", "github.com/sparkle-project/Sparkle",
	"github.com/square/wire", "github.com/swift-otel/swift-otel", "github.com/swift-otel/swift-w3c-trace-context",
	"github.com/swift-server/async-http-client", "github.com/swift-server/swift-prometheus", "github.com/vapor/leaf-kit",
	"github.com/vapor/postgres-nio", "github.com/vapor/vapor", "github.com/weichsel/ZIPFoundation",
}

// resolverSwiftFeedPins pins one clone URL per host/path feed name, the two
// repositories behind the bare names no host/path name covers, and a fork that
// shares a bare name's final path segment.
func resolverSwiftFeedPins() []resolverSwiftPin {
	repositories := []string{"github.com/emqx/CocoaMQTT", "github.com/apple/swift-crypto", "github.com/forker/swift-crypto"}
	for _, name := range resolverSwiftURLFeedNames {
		if strings.Contains(name, "/") {
			repositories = append(repositories, name)
		}
	}
	pins := make([]resolverSwiftPin, 0, len(repositories))
	for _, repository := range repositories {
		identity := strings.ToLower(repository[strings.LastIndexByte(repository, '/')+1:])
		pins = append(pins, resolverSwiftPin{"https://" + repository + ".git", identity, "1.0.0"})
	}
	return pins
}

// resolverParsedSwiftRows writes the pins into a Package.resolved file, runs the
// production parser over it and returns its rows as fixture rows. A pin the
// parser drops fails the test, because a fixture row it would never write is
// the false-green this helper exists to prevent.
func resolverParsedSwiftRows(t *testing.T, scope, generation string, pins []resolverSwiftPin) []resolverFixtureRow {
	t.Helper()
	encoded := make([]map[string]any, 0, len(pins))
	for _, pin := range pins {
		encoded = append(encoded, map[string]any{
			"identity": pin.identity, "kind": "remoteSourceControl", "location": pin.location,
			"state": map[string]any{"revision": "0", "version": pin.version},
		})
	}
	document, err := json.Marshal(map[string]any{"version": 2, "pins": encoded})
	if err != nil {
		t.Fatalf("marshal Package.resolved: %v", err)
	}
	path := filepath.Join(t.TempDir(), "Package.resolved")
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatalf("write Package.resolved: %v", err)
	}
	payload, err := parserjson.Parse(path, false, shared.Options{}, parserjson.Config{})
	if err != nil {
		t.Fatalf("parse Package.resolved: %v", err)
	}
	parsed, _ := payload["variables"].([]map[string]any)
	if len(parsed) != len(pins) {
		t.Fatalf("parser wrote %d rows for %d pins: %v", len(parsed), len(pins), parsed)
	}
	rows := make([]resolverFixtureRow, 0, len(parsed))
	for _, row := range parsed {
		metadata, err := json.Marshal(map[string]any{
			"source_location": row["source_location"], "package_identity": row["package_identity"],
			"source_namespace": row["source_namespace"], "section": row["section"],
			"lockfile_format": row["lockfile_format"],
		})
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		pm, _ := row["package_manager"].(string)
		name, _ := row["name"].(string)
		version, _ := row["value"].(string)
		rows = append(rows, resolverFixtureRow{
			scope: scope, generation: generation, pm: pm, name: name, version: version,
			extra: strings.TrimSuffix(strings.TrimPrefix(string(metadata), "{"), "}"),
		})
	}
	return rows
}
