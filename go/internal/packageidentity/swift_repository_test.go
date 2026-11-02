// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import "testing"

// TestSwiftOSVSourceLocationIsTheOSVHostPathName pins the name the
// vulnerability planner sends OSV for a stored swift source_location. OSV's
// SwiftURL names are host/path with no scheme, credentials, port, query,
// fragment, trailing slash or ".git" (30 of the 33 names in the feed), and OSV
// matches them byte for byte, so the path keeps its case and only the host
// folds (#9804).
func TestSwiftOSVSourceLocationIsTheOSVHostPathName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://github.com/pytorch/executorch":                    "github.com/pytorch/executorch",
		"https://github.com/pytorch/executorch.git":                "github.com/pytorch/executorch",
		"http://github.com/apple/swift-nio":                        "github.com/apple/swift-nio",
		"https://user:secret@github.com/apple/swift-nio.git?x=1#f": "github.com/apple/swift-nio",
		"HTTP://github.com/a/b":                                    "github.com/a/b",
		"ssh://git@github.com/grpc/grpc-swift.git":                 "github.com/grpc/grpc-swift",
		"ssh://git@github.com:2222/grpc/grpc-swift.git":            "github.com/grpc/grpc-swift",
		"https://github.com:443/apple/swift-nio":                   "github.com/apple/swift-nio",
		"git://github.com/migueldeicaza/SwiftTerm":                 "github.com/migueldeicaza/SwiftTerm",
		"git@github.com:apple/swift-log.git":                       "github.com/apple/swift-log",
		"git@github.com:apple/swift-log.git?ref=main":              "github.com/apple/swift-log",
		"git@github.com:apple/swift-log.git#main":                  "github.com/apple/swift-log",
		"git@github.com:apple/swift-log":                           "github.com/apple/swift-log",
		"  https://github.com/a/b  ":                               "github.com/a/b",
		"https://github.com/apple/swift-nio/":                      "github.com/apple/swift-nio",
		"https://github.com/apple/swift-nio.git/":                  "github.com/apple/swift-nio",
		"https://github.com/apple/swift-nio.GIT":                   "github.com/apple/swift-nio",
		"HTTPS://GitHub.COM/Apple/Swift-NIO.git":                   "github.com/Apple/Swift-NIO",
		"https://github.com/marmelroy/Zip":                         "github.com/marmelroy/Zip",
		"git@github.com:sparkle-project/Sparkle.git":               "github.com/sparkle-project/Sparkle",
		"https://www.github.com/apple/swift-nio":                   "www.github.com/apple/swift-nio",
		"github.com/apple/swift-nio":                               "", // a feed spelling, not a recorded clone URL
		"https://gitlab.example.com/group/sub/repo.git":            "gitlab.example.com/group/sub/repo",
		"git@github.com":                                           "",
		"git@:a/b":                                                 "",
		"git@github.com:":                                          "",
		"https://github.com/":                                      "",
		"https://github.com/.git":                                  "",
		"https://github.com":                                       "",
		"file:///tmp/local":                                        "",
		"file://host.example/a/b":                                  "", // file never names a queried repository
		"git@my.host name:a/b":                                     "", // a space in a scp host
		"ftp://github.com/a/b":                                     "",
		"https://localhost/a/b":                                    "",
		"https://[::1]/a/b":                                        "",
		"swift-nio":                                                "",
		"apple/swift-nio":                                          "",
		"not a url":                                                "",
		"":                                                         "",
		"   ":                                                      "",
	}
	for raw, want := range cases {
		if got := SwiftOSVSourceLocation(raw); got != want {
			t.Errorf("SwiftOSVSourceLocation(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestSwiftOSVSourceLocationAgreesWithTheRepositoryKey pins the drift guard:
// the planner name and the stored location have one resolver key, and that key
// is the ASCII fold of the planner name, so the planner and the resolver cannot
// name one repository two ways.
func TestSwiftOSVSourceLocationAgreesWithTheRepositoryKey(t *testing.T) {
	t.Parallel()
	for _, stored := range []string{
		"https://github.com/apple/swift-nio.git",
		"http://github.com/apple/swift-nio",
		"ssh://git@github.com:2222/apple/swift-nio.git",
		"git@github.com:apple/swift-nio.git?ref=main",
		"git://github.com/migueldeicaza/SwiftTerm",
		"HTTPS://GitHub.COM/Apple/Swift-NIO.GIT/",
		"https://user:tok@gitlab.example.com/g/s/r.git#x",
	} {
		name := SwiftOSVSourceLocation(stored)
		if name == "" {
			t.Errorf("stored %q has no planner name", stored)
			continue
		}
		if got, want := SwiftRepositoryKey(name), SwiftRepositoryKey(stored); got != want || got == "" {
			t.Errorf("stored %q: key of planner name %q = %q, key of stored = %q", stored, name, got, want)
		}
		if got := LowerASCII(name); SwiftRepositoryKey(name) != got {
			t.Errorf("planner name %q: key %q is not its ASCII fold %q", name, SwiftRepositoryKey(name), got)
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

// TestSwiftRepositoryKeyRejectsWhitespaceInAScpHost pins that the key, which the
// resolver calls on stored locations directly, names no repository for a scp
// host with a space or tab. The planner name check would hide this guard.
func TestSwiftRepositoryKeyRejectsWhitespaceInAScpHost(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"git@my.host name:a/b", "git@my.host\tname:a/b"} {
		if got := SwiftRepositoryKey(raw); got != "" {
			t.Errorf("SwiftRepositoryKey(%q) = %q, want no repository", raw, got)
		}
	}
}
