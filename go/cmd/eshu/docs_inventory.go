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
	docsEnvVarPattern            = regexp.MustCompile(`\bESHU_[A-Z0-9_]+\b`)
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
	file, err := os.Open(path)
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
		content, err := os.ReadFile(candidate)
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
	return []string{
		filepath.Join(base, "docs", "docs", "reference", "environment-variables.md"),
		filepath.Join(base, "reference", "environment-variables.md"),
		filepath.Join("docs", "docs", "reference", "environment-variables.md"),
		filepath.Join("..", "docs", "docs", "reference", "environment-variables.md"),
	}
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
