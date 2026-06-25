// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

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
	errDocsInventoryLimitReached = errors.New("documentation file limit reached")
	docsEnvVarPattern            = regexp.MustCompile(`\bESHU_[A-Z0-9_]*[A-Z0-9]\b`)
)

func inventoryDocs(opts docsVerifyOptions) (docsInventory, error) {
	info, err := os.Stat(opts.Path)
	if err != nil {
		return docsInventory{}, fmt.Errorf("stat documentation path: %w", err)
	}
	if !info.IsDir() {
		doc, err := readDocumentInput(opts.Path, opts.MaxDocumentBytes)
		if err != nil {
			return docsInventory{}, err
		}
		return docsInventory{Documents: []doctruth.DocumentInput{doc}}, nil
	}
	documents := []doctruth.DocumentInput{}
	err = filepath.WalkDir(opts.Path, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(documents) >= opts.Limit {
			return errDocsInventoryLimitReached
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
			return errDocsInventoryLimitReached
		}
		return nil
	})
	truncated := false
	if errors.Is(err, errDocsInventoryLimitReached) {
		truncated = true
	} else if err != nil {
		return docsInventory{}, fmt.Errorf("inventory documentation: %w", err)
	}
	sort.Slice(documents, func(i, j int) bool { return documents[i].Path < documents[j].Path })
	return docsInventory{Documents: documents, Truncated: truncated}, nil
}

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

func readBoundedDocument(reader io.Reader, maxBytes int) ([]byte, string, bool, error) {
	hash := sha256.New()
	limited, err := io.ReadAll(io.LimitReader(reader, int64(maxBytes)+1))
	if err != nil {
		return nil, "", false, err
	}
	if _, err := hash.Write(limited); err != nil {
		return nil, "", false, err
	}
	if _, err := io.Copy(hash, reader); err != nil {
		return nil, "", false, err
	}
	truncated := len(limited) > maxBytes
	if truncated {
		limited = limited[:maxBytes]
	}
	return limited, "sha256:" + hex.EncodeToString(hash.Sum(nil)), truncated, nil
}

func fileURI(absolute string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
}

func isDocumentationFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".mdx", ".markdown":
		return true
	default:
		return false
	}
}

func docsVerifyEnvironmentTruth(path string) []string {
	out := map[string]struct{}{}
	for _, name := range docsVerifyDefaultEnvironmentTruth() {
		out[name] = struct{}{}
	}
	for _, candidate := range environmentReferenceCandidates(path) {
		content, err := os.ReadFile(candidate) // #nosec G304 -- candidate paths are program-enumerated env-reference file locations within the scan target directory, not HTTP request params
		if err != nil {
			continue
		}
		for _, name := range docsEnvVarPattern.FindAllString(string(content), -1) {
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

func docsVerifyDefaultEnvironmentTruth() []string {
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
