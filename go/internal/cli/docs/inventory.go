// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
)

var (
	// errInventoryLimitReached stops the walk once VerifyOptions.Limit
	// documents are collected. It is a control signal, not a failure: the
	// caller converts it into Inventory.Truncated.
	errInventoryLimitReached = errors.New("documentation file limit reached")

	// envVarPattern matches a concrete ESHU_ environment variable name. The
	// trailing [A-Z0-9] class is what keeps a documented wildcard family
	// prefix such as `ESHU_WORKFLOW_COORDINATOR_*` from being recorded as a
	// real variable.
	envVarPattern = regexp.MustCompile(`\bESHU_[A-Z0-9_]*[A-Z0-9]\b`)
)

// Inventory is the bounded set of documents one verify run will check.
// Truncated reports that the document limit stopped the walk early.
type Inventory struct {
	Documents []doctruth.DocumentInput
	Truncated bool
}

// InventoryDocuments collects the Markdown documents under opts.Path.
//
// A file path is read directly. A directory is walked recursively, skipping
// .git, node_modules, and vendor, and collecting only .md, .mdx, and .markdown
// files. The walk stops at opts.Limit documents and reports Truncated; each
// document's content is bounded at opts.MaxDocumentBytes while its revision id
// still hashes the whole file. Documents are returned sorted by path so a
// repeated run over an unchanged tree produces the same freshness hint.
func InventoryDocuments(opts VerifyOptions) (Inventory, error) {
	info, err := os.Stat(opts.Path)
	if err != nil {
		return Inventory{}, fmt.Errorf("stat documentation path: %w", err)
	}
	if !info.IsDir() {
		doc, err := readDocumentInput(opts.Path, opts.MaxDocumentBytes)
		if err != nil {
			return Inventory{}, err
		}
		return Inventory{Documents: []doctruth.DocumentInput{doc}}, nil
	}
	documents := []doctruth.DocumentInput{}
	err = filepath.WalkDir(opts.Path, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(documents) >= opts.Limit {
			return errInventoryLimitReached
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !isDocumentationFile(path) {
			return nil
		}
		doc, err := readDocumentInput(path, opts.MaxDocumentBytes)
		if err != nil {
			return err
		}
		documents = append(documents, doc)
		if len(documents) >= opts.Limit {
			return errInventoryLimitReached
		}
		return nil
	})
	truncated := false
	if errors.Is(err, errInventoryLimitReached) {
		truncated = true
	} else if err != nil {
		return Inventory{}, fmt.Errorf("inventory documentation: %w", err)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	return Inventory{Documents: documents, Truncated: truncated}, nil
}

// readDocumentInput reads one documentation file into a verifier input. The
// returned content is bounded by maxBytes; RevisionID always hashes the full
// file, so a change past the bound still invalidates the persisted generation.
func readDocumentInput(path string, maxBytes int) (doctruth.DocumentInput, error) {
	file, err := os.Open(path) // #nosec G304 -- path is a documentation file discovered by the program from the scan target directory, not an HTTP request param
	if err != nil {
		return doctruth.DocumentInput{}, fmt.Errorf("read documentation file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	excerpt, revision, truncated, err := readBoundedDocument(file, maxBytes)
	if err != nil {
		return doctruth.DocumentInput{}, fmt.Errorf("read documentation file %s: %w", path, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return doctruth.DocumentInput{}, fmt.Errorf("resolve documentation path %s: %w", path, err)
	}
	return doctruth.DocumentInput{
		Path:             filepath.Clean(path),
		SourceURI:        fileURI(absolute),
		RevisionID:       revision,
		Content:          string(excerpt),
		ContentTruncated: truncated,
	}, nil
}

// readBoundedDocument returns the first maxBytes of reader, a sha256 revision
// id over the entire stream, and whether the content was cut short. Reading
// past the bound only to hash it is deliberate: the revision id has to change
// when a byte beyond the excerpt changes.
func readBoundedDocument(reader io.Reader, maxBytes int) ([]byte, string, bool, error) {
	hash := sha256.New()
	limited, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes)+1))
	if err != nil {
		return nil, "", false, err //nolint:wrapcheck // readDocumentInput wraps this with the file path; wrapping twice would duplicate that context in operator-visible output.
	}
	if _, err := hash.Write(limited); err != nil {
		return nil, "", false, err //nolint:wrapcheck // same single-wrap contract as above.
	}
	if _, err := io.Copy(hash, reader); err != nil {
		return nil, "", false, err //nolint:wrapcheck // same single-wrap contract as above.
	}
	truncated := len(limited) > maxBytes
	if truncated {
		limited = limited[:maxBytes]
	}
	return limited, "sha256:" + hex.EncodeToString(hash.Sum(nil)), truncated, nil
}

// fileURI renders an absolute path as a file:// URI with the escaping url.URL
// applies, so a path containing a space stays a valid URI.
func fileURI(absolute string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
}

// isDocumentationFile reports whether path has a Markdown extension.
func isDocumentationFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".mdx", ".markdown":
		return true
	default:
		return false
	}
}

// EnvironmentTruth reports the ESHU_ environment variable names documentation
// may claim: a built-in default set, plus every concrete name found in the
// environment reference pages reachable from path. Names are sorted and
// deduplicated. A reference page that cannot be read is skipped rather than
// failing the run, which biases an unreadable page toward contradicted
// findings rather than a hard error.
func EnvironmentTruth(path string) []string {
	out := map[string]struct{}{}
	for _, name := range defaultEnvironmentTruth() {
		out[name] = struct{}{}
	}
	for _, candidate := range environmentReferenceCandidates(path) {
		content, err := os.ReadFile(candidate) // #nosec G304 -- candidate paths are program-enumerated env-reference file locations within the scan target directory, not HTTP request params
		if err != nil {
			continue
		}
		for _, name := range envVarPattern.FindAllString(string(content), -1) {
			out[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// environmentReferenceCandidates enumerates the environment reference pages to
// read for path. For each ancestor directory it considers reference/,
// docs/public/reference/, and docs/docs/reference/, taking both
// environment-variables.md and the environment-*.md split pages, then adds the
// same set relative to the working directory and its parent. Candidates are
// deduplicated in discovery order; they are paths to try, not paths that exist.
func environmentReferenceCandidates(path string) []string {
	base := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		base = filepath.Dir(path)
	}
	candidates := []string{}
	seen := map[string]struct{}{}
	add := func(parts ...string) {
		candidate := filepath.Clean(filepath.Join(parts...))
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	addPattern := func(parts ...string) {
		pattern := filepath.Clean(filepath.Join(parts...))
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return
		}
		for _, match := range matches {
			add(match)
		}
	}
	addReferenceSet := func(parts ...string) {
		dirParts := append([]string{}, parts...)
		add(append(dirParts, "environment-variables.md")...)
		addPattern(append(dirParts, "environment-*.md")...)
	}
	for current := filepath.Clean(base); ; current = filepath.Dir(current) {
		addReferenceSet(current, "reference")
		addReferenceSet(current, "docs", "public", "reference")
		addReferenceSet(current, "docs", "docs", "reference")
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	addReferenceSet("docs", "public", "reference")
	addReferenceSet("..", "docs", "public", "reference")
	addReferenceSet("docs", "docs", "reference")
	addReferenceSet("..", "docs", "docs", "reference")
	return candidates
}

// defaultEnvironmentTruth is the environment variable set every verify run
// accepts even when no reference page is reachable.
func defaultEnvironmentTruth() []string {
	return []string{
		"ESHU_API_KEY",
		"ESHU_CONTENT_STORE_DSN",
		"ESHU_FACT_STORE_DSN",
		"ESHU_GRAPH_BACKEND",
		"ESHU_HOME",
		"ESHU_MCP_ADDR",
		"ESHU_POSTGRES_DSN",
		"ESHU_QUERY_PROFILE",
		"ESHU_REMOTE_TIMEOUT_SECONDS",
		"ESHU_SERVICE_URL",
	}
}
