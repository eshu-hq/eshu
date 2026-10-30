// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"strings"
	"testing"
)

// lookalikeLetters are non-ASCII letters whose Unicode lowercase mapping is an
// ASCII letter: U+0130 (capital I with dot above) lowers to "i" and U+212A
// (Kelvin sign) lowers to "k" under Go's strings.ToLower. Registries treat a
// name that carries one as a different package (#9584).
var lookalikeLetters = []struct {
	name    string
	letter  string
	asciiUp string
}{
	{name: "dotted capital I", letter: "\u0130", asciiUp: "I"},
	{name: "Kelvin sign", letter: "\u212A", asciiUp: "K"},
}

// lookalikeTemplates gives one real-looking ASCII name per ecosystem form the
// key functions accept. Each template holds exactly one placeholder letter
// marked "#" that is replaced by the ASCII capital or by the lookalike.
var lookalikeTemplates = []struct {
	ecosystem string
	template  string
}{
	{"nuget", "Ser#log"},
	{"nuget", "Microsoft.#ernel"},
	{"npm", "#ndex-of"},
	{"npm", "@scope/#t"},
	{"pypi", "Dj#ngo_Ext..Plugin"},
	{"gomod", "github.com/#zure/sdk"},
	{"maven", "org.example:#mpl"},
	{"maven", "org.example/#mpl"},
	{"composer", "vendor/#mpl"},
	{"rubygems", "Ra#ls"},
	{"cargo", "Ser#e"},
	{"swift", "github.com/apple/#nit"},
	{"hex", "Acme:#lug"},
	{"hex", "Pl#g"},
	{"pub", "Fl#tter"},
	{"os", "L#bc6"},
	{"generic", "Th#ng"},
}

func lookalikePair(template, asciiUp, letter string) (string, string) {
	return strings.Replace(template, "#", asciiUp, 1), strings.Replace(template, "#", letter, 1)
}

func keySet(keys []ConsumptionKey) map[ConsumptionKey]struct{} {
	set := make(map[ConsumptionKey]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

func sharedKey(left, right []ConsumptionKey) (ConsumptionKey, bool) {
	set := keySet(left)
	for _, key := range right {
		if _, ok := set[key]; ok {
			return key, true
		}
	}
	return ConsumptionKey{}, false
}

// TestConsumptionKeysKeepLookalikesApart is the #9584 regression: a manifest
// name with a non-ASCII lookalike letter must share no key with the ASCII
// name, in every ecosystem and through every key entry point.
func TestConsumptionKeysKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	for _, letter := range lookalikeLetters {
		for _, entry := range lookalikeTemplates {
			asciiName, lookalike := lookalikePair(entry.template, letter.asciiUp, letter.letter)
			t.Run(letter.name+"/"+entry.ecosystem+"/"+asciiName, func(t *testing.T) {
				t.Parallel()

				asciiKeys := ConsumptionKeys(entry.ecosystem, asciiName)
				if entry.ecosystem != "generic" && len(asciiKeys) == 0 {
					t.Fatalf("ConsumptionKeys(%q, %q) returned no keys; the template is not a valid name", entry.ecosystem, asciiName)
				}
				if key, ok := sharedKey(asciiKeys, ConsumptionKeys(entry.ecosystem, lookalike)); ok {
					t.Fatalf("manifest name %q joins %q via key %+v", lookalike, asciiName, key)
				}
				manifest := ManifestConsumptionKeys(ManifestKeyInput{PackageManager: entry.ecosystem, Names: []string{lookalike}})
				if key, ok := sharedKey(asciiKeys, manifest); ok {
					t.Fatalf("ManifestConsumptionKeys(%q) joins %q via key %+v", lookalike, asciiName, key)
				}
				ecosystem := NormalizeEcosystem(Ecosystem(entry.ecosystem))
				asciiNames := ManifestEntityNameCandidates(ecosystem, asciiName)
				for _, candidate := range ManifestEntityNameCandidates(ecosystem, lookalike) {
					for _, asciiCandidate := range asciiNames {
						if candidate == asciiCandidate {
							t.Fatalf("ManifestEntityNameCandidates(%q) shares %q with %q", lookalike, candidate, asciiName)
						}
					}
				}
			})
		}
	}
}

// TestRegistryIdentityKeysKeepLookalikesApart covers the advisory and
// registry side: an advisory or registry package spelled with a lookalike must
// not produce the ASCII package's key or package ID either, so the two sides
// fold identically.
func TestRegistryIdentityKeysKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	for _, letter := range lookalikeLetters {
		asciiName, lookalike := lookalikePair("Ser#log", letter.asciiUp, letter.letter)
		asciiKeys := ConsumptionKeys("nuget", asciiName)

		lookalikeIdentity, err := Normalize(RawIdentity{Ecosystem: EcosystemNuGet, Registry: DefaultRegistry(EcosystemNuGet), RawName: lookalike})
		if err != nil {
			t.Fatalf("Normalize(%q) error = %v", lookalike, err)
		}
		asciiIdentity, err := Normalize(RawIdentity{Ecosystem: EcosystemNuGet, Registry: DefaultRegistry(EcosystemNuGet), RawName: asciiName})
		if err != nil {
			t.Fatalf("Normalize(%q) error = %v", asciiName, err)
		}
		if lookalikeIdentity.PackageID == asciiIdentity.PackageID {
			t.Fatalf("%s: lookalike %q and %q share package ID %q", letter.name, lookalike, asciiName, asciiIdentity.PackageID)
		}
		registry := ConsumptionKeysForRegistryIdentity("nuget", lookalike, lookalikeIdentity.NormalizedName, "")
		if key, ok := sharedKey(asciiKeys, registry); ok {
			t.Fatalf("%s: registry identity %q joins %q via key %+v", letter.name, lookalike, asciiName, key)
		}
		if key, ok := sharedKey(asciiKeys, ConsumptionKeysFromPURL("pkg:nuget/"+lookalike+"@1.0.0")); ok {
			t.Fatalf("%s: PURL for %q joins %q via key %+v", letter.name, lookalike, asciiName, key)
		}
		if key, ok := sharedKey(asciiKeys, ConsumptionKeysFromPackageID(lookalikeIdentity.PackageID)); ok {
			t.Fatalf("%s: package ID for %q joins %q via key %+v", letter.name, lookalike, asciiName, key)
		}
	}
}

// TestConsumptionKeysPreserveNonASCIILettersAndFoldASCII pins the rule: only
// A-Z fold, every other rune keeps its source form, and the same lookalike
// spelled twice still joins itself.
func TestConsumptionKeysPreserveNonASCIILettersAndFoldASCII(t *testing.T) {
	t.Parallel()

	got := ConsumptionKeys("nuget", "Ser\u0130log")
	want := []ConsumptionKey{{Ecosystem: EcosystemNuGet, PackageName: "ser\u0130log"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("ConsumptionKeys(nuget, Ser\\u0130log) = %#v, want %#v", got, want)
	}
	if key, ok := sharedKey(got, ConsumptionKeys("nuget", "SER\u0130LOG")); !ok {
		t.Fatalf("an ASCII case variant of the same lookalike must still join itself, got no shared key")
	} else if key != want[0] {
		t.Fatalf("shared key = %+v, want %+v", key, want[0])
	}
	if got := ConsumptionKeys("nuget", "Stra\u00DFe"); len(got) != 1 || got[0].PackageName != "stra\u00DFe" {
		t.Fatalf("ConsumptionKeys(nuget, Stra\\u00dfe) = %#v, want the non-ASCII letter kept", got)
	}
}

func TestLowerASCIIFoldsOnlyAToZ(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"":                   "",
		"already-lower":      "already-lower",
		"MiXeD.Case_09":      "mixed.case_09",
		"Ser\u0130log":       "ser\u0130log",
		"\u212Aelvin":        "\u212Aelvin",
		"\u00C5ngstr\u00F6m": "\u00C5ngstr\u00F6m",
		"@Scope/Name":        "@scope/name",
	} {
		if got := LowerASCII(input); got != want {
			t.Fatalf("LowerASCII(%q) = %q, want %q", input, got, want)
		}
	}
	for b := 0; b < 128; b++ {
		input := string(rune(b))
		if got, want := LowerASCII(input), strings.ToLower(input); got != want {
			t.Fatalf("LowerASCII(%q) = %q, strings.ToLower = %q; the two must agree on ASCII", input, got, want)
		}
	}
}

func TestContainsNonASCII(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]bool{"": false, "serilog": false, "Ser\u0130log": true, "caf\u00E9": true} {
		if got := ContainsNonASCII(input); got != want {
			t.Fatalf("ContainsNonASCII(%q) = %v, want %v", input, got, want)
		}
	}
}

// TestManifestConsumptionKeysRejectReviewedLookalikes pins the four
// collisions the #9568 review confirmed through ManifestConsumptionKeys.
func TestManifestConsumptionKeysRejectReviewedLookalikes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		packageManager, lookalike, asciiKey string
	}{
		{"nuget", "Ser\u0130log", "serilog"},
		{"npm", "\u212Aafka", "kafka"},
		{"pypi", "Flas\u212A", "flask"},
		{"maven", "org.apache.\u212Aafka:kafka-clients", "org.apache.kafka:kafka-clients"},
	} {
		keys := ManifestConsumptionKeys(ManifestKeyInput{PackageManager: test.packageManager, Names: []string{test.lookalike}})
		if len(keys) == 0 {
			t.Fatalf("ManifestConsumptionKeys(%s, %q) returned no keys, want the lookalike's own keys", test.packageManager, test.lookalike)
		}
		for _, key := range keys {
			if key.PackageName == test.asciiKey {
				t.Fatalf("ManifestConsumptionKeys(%s, %q) produced the ASCII package key %q", test.packageManager, test.lookalike, test.asciiKey)
			}
		}
	}
}

// TestIdentityHelpersKeepLookalikesApart pins every remaining identity fold in the
// package to ASCII-only case folding (#9584): registry hosts in URL and bare
// form, ecosystem aliases, Swift names, Hex organizations, the PURL type
// fallback, and the sbt package-manager check.
func TestIdentityHelpersKeepLookalikesApart(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"https://REG\u0130STRY.example/v3/": "reg\u0130stry.example/v3",
		"https://\u212AEYS.example":         "\u212Aeys.example",
		"REG\u0130STRY.example/Path/":       "reg\u0130stry.example/Path",
		"\u212AEYS.example":                 "\u212Aeys.example",
	} {
		if got := NormalizeRegistry(input); got != want {
			t.Fatalf("NormalizeRegistry(%q) = %q, want %q", input, got, want)
		}
	}
	if got := NormalizeEcosystem(Ecosystem("P\u0130P")); got != "" {
		t.Fatalf("NormalizeEcosystem(P\u0130P) = %q, want no ecosystem", got)
	}

	identityPairs := []struct {
		name             string
		ascii, lookalike RawIdentity
	}{
		{
			name:      "swift name",
			ascii:     RawIdentity{Ecosystem: EcosystemSwift, RawName: "Swift-NIO", Namespace: "github.com/apple"},
			lookalike: RawIdentity{Ecosystem: EcosystemSwift, RawName: "Swift-N\u0130O", Namespace: "github.com/apple"},
		},
		{
			name:      "swift namespace path",
			ascii:     RawIdentity{Ecosystem: EcosystemSwift, RawName: "swift-nio", Namespace: "github.com/KAPPLE"},
			lookalike: RawIdentity{Ecosystem: EcosystemSwift, RawName: "swift-nio", Namespace: "github.com/\u212AAPPLE"},
		},
		{
			name:      "composer vendor namespace",
			ascii:     RawIdentity{Ecosystem: EcosystemComposer, RawName: "console", Namespace: "SymfonI"},
			lookalike: RawIdentity{Ecosystem: EcosystemComposer, RawName: "console", Namespace: "Symfon\u0130"},
		},
		{
			name:      "hex organization",
			ascii:     RawIdentity{Ecosystem: EcosystemHex, RawName: "plug", Namespace: "Kcme"},
			lookalike: RawIdentity{Ecosystem: EcosystemHex, RawName: "plug", Namespace: "\u212Acme"},
		},
	}
	for _, pair := range identityPairs {
		pair.ascii.Registry = DefaultRegistry(pair.ascii.Ecosystem)
		pair.lookalike.Registry = DefaultRegistry(pair.lookalike.Ecosystem)
		ascii, err := Normalize(pair.ascii)
		if err != nil {
			t.Fatalf("%s: Normalize(ascii) error = %v", pair.name, err)
		}
		lookalike, err := Normalize(pair.lookalike)
		if err != nil {
			t.Fatalf("%s: Normalize(lookalike) error = %v", pair.name, err)
		}
		if ascii.PackageID == lookalike.PackageID {
			t.Fatalf("%s: lookalike shares package ID %q", pair.name, ascii.PackageID)
		}
	}

	osIdentity, err := Normalize(RawIdentity{Ecosystem: EcosystemOS, Registry: "alpine", RawName: "musl", PackageManager: " AP\u212A "})
	if err != nil {
		t.Fatalf("Normalize(os, package manager AP\u212A) error = %v", err)
	}
	if osIdentity.PackageManager != "ap\u212A" {
		t.Fatalf("package manager = %q, want %q kept apart from apk", osIdentity.PackageManager, "ap\u212A")
	}

	pypiKeys := ConsumptionKeys("pypi", "requests")
	if key, ok := sharedKey(pypiKeys, ConsumptionKeysFromPURL("pkg:P\u0130P/requests@1.0.0")); ok {
		t.Fatalf("a PURL with type P\u0130P joins pypi requests via key %+v", key)
	}

	if !isSBTPackageManager(" SBT ") || isSBTPackageManager("\u017Fbt") {
		t.Fatalf("isSBTPackageManager must fold ASCII case only: SBT=%t, long-s sbt=%t", isSBTPackageManager(" SBT "), isSBTPackageManager("\u017Fbt"))
	}
}
