// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// BuilderManifest is the checked-in inventory of every production
// sourcecypher.Statement builder (issue #6783): one entry per enclosing
// symbol with its match rule (template for static builders, ordered
// fragments for dynamic ones), pinned by source digest. Builders whose
// text is statically unknowable (forwarding adapters, derived probe text,
// opaque composition) carry an Exempt reason instead of a match rule; the
// exemption excuses execution proof, never drift — template, fragments,
// and digest still pin the symbol.
//
// ReadExemptions excuse recording-side always-empty reads by builder
// identity: each names the go-relative path:symbol of the direct
// Run/RunSingle caller that produced the read, plus a stable anchor
// fragment the recorded text must contain, plus a reason. Identity (not
// full statement text) is the key so projection edits that keep the
// anchor leave the exemption working; a renamed, moved, or deleted
// callsite fails as stale instead of rotting silently (see the
// coverage gate's stale-exemption finding).
type BuilderManifest struct {
	Version        int                        `yaml:"version"`
	Builders       []StatementBuilderCoverage `yaml:"builders"`
	ReadExemptions []ReadExemption            `yaml:"read_exemptions,omitempty"`
}

// MinExemptionAnchorLength floors exemption anchors the same way the
// coverage gate floors fragment-set anchors: glue-only fragments match
// siblings and would excuse the wrong read. It must stay equal to the
// coverage gate's anchor floor; the backendconformance test suite pins
// the equality.
const MinExemptionAnchorLength = 16

// ReadExemption excuses one always-empty recorded read by the builder
// identity that produced it and a stable anchor its text must contain.
type ReadExemption struct {
	Callsite string `yaml:"callsite"`
	Anchor   string `yaml:"anchor"`
	Reason   string `yaml:"reason"`
}

// GoDir returns the go module subdirectory containing this source tree,
// so validators and attribution can resolve go-relative paths without
// depending on the caller's working directory.
func GoDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve queryplan package file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file))), nil
}

// LoadBuilderManifest reads and parses a builder manifest file.
func LoadBuilderManifest(path string) (BuilderManifest, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is a testdata manifest chosen by the caller
	if err != nil {
		return BuilderManifest{}, fmt.Errorf("read builder manifest: %w", err)
	}
	var manifest BuilderManifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return BuilderManifest{}, fmt.Errorf("parse builder manifest: %w", err)
	}
	for i := range manifest.ReadExemptions {
		manifest.ReadExemptions[i].Callsite = CanonicalCallsite(manifest.ReadExemptions[i].Callsite)
	}
	return manifest, nil
}

// ValidateBuilderManifest verifies the checked-in manifest against fresh
// discovery: every discovered builder must be registered with the exact
// count, digest, template, and fragments, and every registration must
// still exist. Drift in any pinned field fails; only the execution proof
// (covered by the gate, not this validator) honors exemptions.
func ValidateBuilderManifest(manifest BuilderManifest, discovered []StatementBuilderCoverage) error {
	expected := flattenBuilders(manifest.Builders)
	actual := flattenBuilders(discovered)
	var violations []string
	for key, builder := range actual {
		want, ok := expected[key]
		if !ok {
			violations = append(violations, fmt.Sprintf(
				"unregistered statement builder %s (count %d)",
				key,
				builder.Count,
			))
			continue
		}
		if builder.Count != want.Count {
			violations = append(violations, fmt.Sprintf(
				"%s: discovered build count %d, manifest requires %d",
				key,
				builder.Count,
				want.Count,
			))
		}
		if builder.SourceDigest != want.SourceDigest {
			violations = append(violations, fmt.Sprintf(
				"%s: source_sha256 does not match production symbol (manifest %s, production %s)",
				key,
				want.SourceDigest,
				builder.SourceDigest,
			))
		}
		if !reflect.DeepEqual(builder.Variants, want.Variants) {
			violations = append(violations, fmt.Sprintf(
				"%s: statement variants do not match production symbol",
				key,
			))
		}
	}
	for key := range expected {
		if _, ok := actual[key]; !ok {
			violations = append(violations, fmt.Sprintf("stale statement builder registration %s", key))
		}
	}
	for key, builder := range expected {
		if builder.Exempt != "" && strings.TrimSpace(builder.Exempt) == "" {
			violations = append(violations, fmt.Sprintf(
				"%s: exemption requires a reason",
				key,
			))
		}
	}
	goDir, goDirErr := GoDir()
	if goDirErr != nil {
		violations = append(violations, fmt.Sprintf("resolve go source directory: %v", goDirErr))
	}
	for _, exemption := range manifest.ReadExemptions {
		if strings.TrimSpace(exemption.Callsite) == "" {
			violations = append(violations, "read exemption requires a callsite")
			continue
		}
		path, _, ok := splitCallsite(exemption.Callsite)
		if !ok {
			violations = append(violations, fmt.Sprintf(
				"read exemption callsite must be go-relative path:symbol: %.60q",
				exemption.Callsite,
			))
			continue
		}
		if goDirErr == nil {
			if info, err := os.Stat(filepath.Join(goDir, filepath.FromSlash(path))); err != nil || info.IsDir() {
				violations = append(violations, fmt.Sprintf(
					"read exemption callsite file does not exist: %.60q",
					exemption.Callsite,
				))
				continue
			}
		}
		if len(strings.Join(strings.Fields(exemption.Anchor), " ")) < MinExemptionAnchorLength {
			violations = append(violations, fmt.Sprintf(
				"read exemption anchor must be at least %d characters: %.60q",
				MinExemptionAnchorLength,
				exemption.Callsite,
			))
			continue
		}
		if strings.TrimSpace(exemption.Reason) == "" {
			violations = append(violations, fmt.Sprintf(
				"read exemption requires a reason: %.60q",
				exemption.Callsite,
			))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

// CanonicalCallsiteSymbol normalizes a builder-identity symbol to its
// canonical form: the leading dot-separated segment parenthesized. The
// Go runtime renders a value-receiver method bare (Type.Method) but a
// pointer-receiver method parenthesized ((*Type).Method), so the same
// builder would record two different identities across a receiver change;
// the canonical form ((Type).Method, (*Type).Method) is stable across
// both. Capture and manifest load apply it identically, so authors may
// write either form. Plain functions (no dot) pass through unchanged;
// closures parenthesize the function name ((Foo).Method.func1), which is
// stable but cosmetic — the identity still locates the builder. Dots
// inside generic brackets do not split the receiver.
func CanonicalCallsiteSymbol(symbol string) string {
	if strings.HasPrefix(symbol, "(") {
		return symbol
	}
	depth := 0
	for i := 0; i < len(symbol); i++ {
		switch symbol[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case '.':
			if depth == 0 {
				return "(" + symbol[:i] + ")" + symbol[i:]
			}
		}
	}
	return symbol
}

// CanonicalCallsite normalizes a full builder identity to its canonical
// form, leaving malformed identities untouched to fail closed downstream.
func CanonicalCallsite(callsite string) string {
	path, symbol, ok := splitCallsite(callsite)
	if !ok {
		return callsite
	}
	return path + ":" + CanonicalCallsiteSymbol(symbol)
}

// splitCallsite divides a builder identity into its go-relative file path
// and symbol. Both parts must be non-empty and the path must end in .go;
// the symbol is opaque otherwise (methods, closures, and generic
// instantiations all keep their runtime form).
func splitCallsite(callsite string) (path, symbol string, ok bool) {
	path, symbol, ok = strings.Cut(callsite, ":")
	if !ok || path == "" || symbol == "" || !strings.HasSuffix(path, ".go") {
		return "", "", false
	}
	return path, symbol, true
}

func flattenBuilders(coverage []StatementBuilderCoverage) map[string]StatementBuilder {
	out := make(map[string]StatementBuilder)
	for _, file := range coverage {
		for _, builder := range file.Builders {
			out[file.File+":"+builder.Symbol] = builder
		}
	}
	return out
}
