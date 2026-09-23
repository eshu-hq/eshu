// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"errors"
	"fmt"
	"os"
	"reflect"
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
// ReadExemptions excuse recording-side always-empty reads by statement
// text: each needs the exact recorded text and a reason.
type BuilderManifest struct {
	Version        int                        `yaml:"version"`
	Builders       []StatementBuilderCoverage `yaml:"builders"`
	ReadExemptions []ReadExemption            `yaml:"read_exemptions,omitempty"`
}

// ReadExemption excuses one always-empty recorded read fingerprint.
type ReadExemption struct {
	Statement string `yaml:"statement"`
	Reason    string `yaml:"reason"`
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
	for _, exemption := range manifest.ReadExemptions {
		if strings.TrimSpace(exemption.Statement) == "" {
			violations = append(violations, "read exemption requires a statement")
			continue
		}
		if strings.TrimSpace(exemption.Reason) == "" {
			violations = append(violations, fmt.Sprintf(
				"read exemption requires a reason: %.40q",
				exemption.Statement,
			))
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
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
