// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "testing"

// TestSwiftOSVSourceLocationIsThePlannerRule pins the name the vulnerability
// planner queries for a stored swift source_location.
func TestSwiftOSVSourceLocationIsThePlannerRule(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://github.com/pytorch/executorch":                    "https://github.com/pytorch/executorch",
		"https://github.com/pytorch/executorch.git":                "https://github.com/pytorch/executorch.git",
		"https://user:secret@github.com/apple/swift-nio.git?x=1#f": "https://github.com/apple/swift-nio.git",
		"HTTP://github.com/a/b":                                    "http://github.com/a/b",
		"ssh://git@github.com/grpc/grpc-swift.git":                 "ssh://github.com/grpc/grpc-swift.git",
		"git://github.com/migueldeicaza/SwiftTerm":                 "git://github.com/migueldeicaza/SwiftTerm",
		"git@github.com:apple/swift-log.git":                       "git@github.com:apple/swift-log.git",
		"git@github.com:apple/swift-log.git?ref=main":              "git@github.com:apple/swift-log.git",
		"git@github.com:apple/swift-log.git#main":                  "git@github.com:apple/swift-log.git",
		"  https://github.com/a/b  ":                               "https://github.com/a/b",
		"git@github.com":                                           "",
		"git@:a/b":                                                 "",
		"git@github.com:":                                          "",
		"https://github.com/":                                      "",
		"file:///tmp/local":                                        "",
		"ftp://github.com/a/b":                                     "",
		"not a url":                                                "",
		"":                                                         "",
	}
	for raw, want := range cases {
		if got := SwiftOSVSourceLocation(raw); got != want {
			t.Errorf("SwiftOSVSourceLocation(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestSwiftRepositoryKeyJoinsFeedAndStoredSpellings is the parity table: every
// row of one group is a spelling of the same repository, on the feed side or
// the stored side, and all of them must share one key. Feed values are literal
// names from the OSV SwiftURL export.
func TestSwiftRepositoryKeyJoinsFeedAndStoredSpellings(t *testing.T) {
	t.Parallel()
	for _, group := range []struct {
		key       string
		spellings []string
	}{
		{"github.com/apple/swift-nio", []string{
			"github.com/apple/swift-nio",
			"https://github.com/apple/swift-nio",
			"https://github.com/apple/swift-nio.git",
			"https://github.com/apple/swift-nio.git/",
			"https://github.com/apple/swift-nio/",
			"http://github.com/apple/swift-nio",
			"https://user:token@github.com/apple/swift-nio.git?x=1#frag",
			"https://github.com:443/apple/swift-nio",
			"ssh://git@github.com/apple/swift-nio.git",
			"git://github.com/apple/swift-nio",
			"git@github.com:apple/swift-nio.git",
			"git@github.com:apple/swift-nio.git?ref=main",
			"GitHub.com/Apple/Swift-NIO",
			"  github.com/apple/swift-nio  ",
			"https://github.com/apple/swift-nio.GIT",
			"https://github.com/apple/swift-nio.Git/",
			"https://github.com/Apple/Swift-NIO.GIT",
			"git@github.com:apple/swift-nio.GIT",
			"github.com/apple/swift-nio.Git",
		}},
		{"github.com/marmelroy/zip", []string{"github.com/marmelroy/Zip", "https://github.com/marmelroy/zip/"}},
		{"github.com/sparkle-project/sparkle", []string{"github.com/sparkle-project/Sparkle", "git@github.com:sparkle-project/Sparkle.git"}},
	} {
		for _, spelling := range group.spellings {
			if got := SwiftRepositoryKey(spelling); got != group.key {
				t.Errorf("SwiftRepositoryKey(%q) = %q, want %q", spelling, got, group.key)
			}
		}
	}
}

func TestSwiftRepositoryKeyRefusesWhatNamesNoRepository(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"", "swift-crypto", "CocoaMQTT", "swift-nio-http2", // the three bare SwiftURL feed names
		"apple/swift-nio", // a path with no host
		"file:///tmp/local", "ftp://github.com/a/b", "https://github.com/", "https://github.com",
		"git@github.com", "git@github.com:", "user@github.com:a/b", "not a/url.git", "github.com/",
	} {
		if got := SwiftRepositoryKey(raw); got != "" {
			t.Errorf("SwiftRepositoryKey(%q) = %q, want no key", raw, got)
		}
	}
}

// TestSwiftRepositoryKeyAcceptsEveryPlannerName pins the composition the
// resolver relies on: whatever the planner queries keeps a repository key.
func TestSwiftRepositoryKeyAcceptsEveryPlannerName(t *testing.T) {
	t.Parallel()
	for _, stored := range []string{
		"https://user:tok@github.com/apple/swift-nio.git?x=1",
		"ssh://git@github.com/grpc/grpc-swift.git",
		"git@github.com:vapor/vapor.git?ref=main",
		"git://github.com/migueldeicaza/SwiftTerm",
		"http://code.example/a/b",
		"https://localhost/a/b",
	} {
		planned := SwiftOSVSourceLocation(stored)
		if planned == "" || SwiftRepositoryKey(planned) == "" {
			t.Errorf("stored %q: planned %q has key %q", stored, planned, SwiftRepositoryKey(planned))
		}
		if SwiftRepositoryKey(planned) != SwiftRepositoryKey(stored) {
			t.Errorf("stored %q and planned %q have different keys", stored, planned)
		}
	}
}
