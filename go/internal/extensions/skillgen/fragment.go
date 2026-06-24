// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Fragment is one loaded source-of-truth file from skill-fragments/.
//
// The frontmatter is the machine-parseable surface; the body is the
// human- and skill-readable surface that the host adapters copy into
// the generated output.
type Fragment struct {
	// ID is the stable fragment id; matches the filename without .md.
	ID string
	// Version is the fragment schema version; bumped on breaking contract changes.
	Version string
	// Requires is a list of fragment ids this fragment depends on.
	Requires []string
	// ByteCitation is the canonical source anchor in the form path#start-end.
	ByteCitation string
	// Description is the one-line purpose used for the host skill's description
	// and for capability-aware rendering.
	Description string
	// Body is the Markdown body of the fragment, with the frontmatter block
	// stripped. The body starts with a leading newline if the fragment file
	// had one after the closing frontmatter delimiter.
	Body string
	// SourcePath is the absolute path of the fragment file on disk.
	SourcePath string
}

// frontmatter is the YAML frontmatter schema. Field tags are lowercase
// because that is the contract S1 ships; renames require a fragment
// version bump and a regeneration of the expected/ baseline.
type frontmatter struct {
	ID           string   `yaml:"id"`
	Version      string   `yaml:"version"`
	Requires     []string `yaml:"requires"`
	ByteCitation string   `yaml:"byte_citation"`
	Description  string   `yaml:"description"`
}

// ErrFragmentMissingByteCitation is returned by LoadFragments when a fragment
// file is missing the required byte_citation field.
var ErrFragmentMissingByteCitation = errors.New("fragment missing byte_citation")

// LoadFragments reads every .md file in fragmentsDir, parses its YAML
// frontmatter, and returns the fragments sorted by ID for deterministic
// rendering.
//
// Non-Markdown files are silently skipped. Fragments with invalid
// frontmatter, missing required fields, or duplicate IDs return an
// error and the result is nil.
func LoadFragments(fragmentsDir string) ([]Fragment, error) {
	entries, err := os.ReadDir(fragmentsDir)
	if err != nil {
		return nil, fmt.Errorf("read fragments dir %s: %w", fragmentsDir, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		paths = append(paths, filepath.Join(fragmentsDir, entry.Name()))
	}
	sort.Strings(paths)
	fragments := make([]Fragment, 0, len(paths))
	seen := make(map[string]string)
	for _, path := range paths {
		fragment, err := loadFragment(path)
		if err != nil {
			return nil, err
		}
		if other, ok := seen[fragment.ID]; ok {
			return nil, fmt.Errorf("duplicate fragment id %q in %s and %s", fragment.ID, other, path)
		}
		seen[fragment.ID] = path
		fragments = append(fragments, fragment)
	}
	return fragments, nil
}

// loadFragment reads one fragment file and parses its frontmatter.
func loadFragment(path string) (Fragment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fragment{}, fmt.Errorf("read fragment %s: %w", path, err)
	}
	fm, body, err := splitFrontmatter(string(data))
	if err != nil {
		return Fragment{}, fmt.Errorf("fragment %s: %w", path, err)
	}
	if strings.TrimSpace(fm.ByteCitation) == "" {
		return Fragment{}, fmt.Errorf("fragment %s: %w", path, ErrFragmentMissingByteCitation)
	}
	if strings.TrimSpace(fm.ID) == "" {
		return Fragment{}, fmt.Errorf("fragment %s: id is required", path)
	}
	if strings.TrimSpace(fm.Version) == "" {
		return Fragment{}, fmt.Errorf("fragment %s: version is required", path)
	}
	if strings.TrimSpace(fm.Description) == "" {
		return Fragment{}, fmt.Errorf("fragment %s: description is required", path)
	}
	return Fragment{
		ID:           strings.TrimSpace(fm.ID),
		Version:      strings.TrimSpace(fm.Version),
		Requires:     fm.Requires,
		ByteCitation: strings.TrimSpace(fm.ByteCitation),
		Description:  strings.TrimSpace(fm.Description),
		Body:         body,
		SourcePath:   path,
	}, nil
}

// splitFrontmatter splits a Markdown file into its YAML frontmatter and body.
// The first line MUST start with "---" and the closing "---" must be on its
// own line. The body returned is the trimmed suffix after the closing delimiter.
func splitFrontmatter(content string) (frontmatter, string, error) {
	if !strings.HasPrefix(content, "---") {
		return frontmatter{}, "", errors.New("missing opening frontmatter delimiter")
	}
	rest := content[3:]
	// Allow a single trailing newline after the opening "---".
	rest = strings.TrimPrefix(rest, "\n")
	// The rest must start with YAML content; find the closing "---" on its
	// own line.
	const closingMarker = "\n---"
	closeIdx := strings.Index(rest, closingMarker)
	if closeIdx < 0 {
		return frontmatter{}, "", errors.New("missing closing frontmatter delimiter")
	}
	yamlBlock := rest[:closeIdx]
	after := rest[closeIdx+len(closingMarker):]
	// Trim the leading blank line(s) after the closing "---" so the body
	// starts at the first non-whitespace character of the Markdown content.
	after = strings.TrimLeft(after, "\n")
	after = strings.TrimLeft(after, " \t")
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return frontmatter{}, "", fmt.Errorf("parse frontmatter yaml: %w", err)
	}
	return fm, after, nil
}
