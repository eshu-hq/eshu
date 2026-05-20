package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/query"
)

type docsVerifyOptions struct {
	Path             string
	Limit            int
	MaxDocumentBytes int
	FailOn           []string
	JSON             bool
}

type docsVerifyEnvelope struct {
	Data  docsVerifyData   `json:"data"`
	Truth map[string]any   `json:"truth"`
	Error *docsVerifyError `json:"error"`
}

type docsVerifyData struct {
	Findings        []doctruth.VerificationFinding        `json:"findings"`
	EvidencePackets []doctruth.VerificationEvidencePacket `json:"evidence_packets"`
	Summary         doctruth.VerificationSummary          `json:"summary"`
	Truncated       bool                                  `json:"truncated"`
}

type docsInventory struct {
	Documents []doctruth.DocumentInput
	Truncated bool
}

type docsVerifyError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

var (
	errDocsInventoryLimitReached = errors.New("documentation file limit reached")
	docsEnvVarPattern            = regexp.MustCompile(`\bESHU_[A-Z0-9_]+\b`)
)

func init() {
	rootCmd.AddCommand(newDocsCommand())
}

func newDocsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Verify and inspect documentation truth",
	}
	cmd.AddCommand(newDocsVerifyCommand())
	return cmd
}

func newDocsVerifyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "verify [path]",
		Short:         "Verify documentation claims against Eshu truth sources",
		Args:          cobra.MaximumNArgs(1),
		RunE:          runDocsVerify,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().Int("limit", 50, "Maximum documentation files to scan")
	cmd.Flags().Int("max-bytes", 256*1024, "Maximum bytes to read from each documentation file")
	cmd.Flags().String("fail-on", "", "Comma-separated finding statuses that should fail the command")
	cmd.Flags().String("scope", "", "Reserved verification scope selector")
	cmd.Flags().String("repo", "", "Reserved repository selector")
	cmd.Flags().Bool("json", false, "Write documentation verification as JSON")
	return cmd
}

func runDocsVerify(cmd *cobra.Command, args []string) error {
	opts, err := docsVerifyOptionsFromCommand(cmd, args)
	if err != nil {
		return err
	}
	inventory, err := inventoryDocs(opts)
	if err != nil {
		return err
	}
	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands:             commandTruthFromCobra(rootCmd),
		HTTPEndpoints:        endpointTruthFromOpenAPI(query.OpenAPISpec()),
		EnvironmentVariables: docsVerifyEnvironmentTruth(opts.Path),
		MaxDocuments:         opts.Limit,
		MaxDocumentBytes:     opts.MaxDocumentBytes,
	})
	result, err := verifier.Verify(cmd.Context(), inventory.Documents)
	if err != nil {
		return err
	}
	result.Truncated = result.Truncated || inventory.Truncated
	exitErr := docsVerifyFailure(opts, result)
	envelope := docsVerifyEnvelopeForResult(result, exitErr)
	if opts.JSON {
		if err := writeDocsVerifyJSON(cmd.OutOrStdout(), envelope); err != nil {
			return err
		}
		return exitErr
	}
	if err := renderDocsVerifyText(cmd.OutOrStdout(), result); err != nil {
		return err
	}
	return exitErr
}

func docsVerifyOptionsFromCommand(cmd *cobra.Command, args []string) (docsVerifyOptions, error) {
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	maxBytes, err := cmd.Flags().GetInt("max-bytes")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	failOn, err := cmd.Flags().GetString("fail-on")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return docsVerifyOptions{}, err
	}
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	if limit <= 0 {
		return docsVerifyOptions{}, commandExitError{message: "--limit must be greater than 0", code: 2}
	}
	if maxBytes <= 0 {
		return docsVerifyOptions{}, commandExitError{message: "--max-bytes must be greater than 0", code: 2}
	}
	return docsVerifyOptions{
		Path:             path,
		Limit:            limit,
		MaxDocumentBytes: maxBytes,
		FailOn:           splitCSV(failOn),
		JSON:             jsonOutput,
	}, nil
}

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

func commandTruthFromCobra(root *cobra.Command) []doctruth.CommandTruth {
	out := []doctruth.CommandTruth{}
	var walk func(*cobra.Command, []string)
	walk = func(cmd *cobra.Command, prefix []string) {
		for _, child := range cmd.Commands() {
			if child.Hidden {
				continue
			}
			name := strings.Fields(child.Use)
			if len(name) == 0 {
				continue
			}
			path := append(append([]string{}, prefix...), name[0])
			out = append(out, doctruth.CommandTruth{Path: path})
			walk(child, path)
		}
	}
	walk(root, nil)
	return out
}

func endpointTruthFromOpenAPI(spec string) []doctruth.HTTPEndpointTruth {
	var raw struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(spec), &raw); err != nil {
		return nil
	}
	out := []doctruth.HTTPEndpointTruth{}
	for path, methods := range raw.Paths {
		for method := range methods {
			method = strings.ToUpper(method)
			switch method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				out = append(out, doctruth.HTTPEndpointTruth{Method: method, Path: path})
			}
		}
	}
	return out
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

func docsVerifyFailure(opts docsVerifyOptions, result doctruth.VerificationResult) error {
	failOn := map[string]struct{}{}
	for _, status := range opts.FailOn {
		failOn[status] = struct{}{}
	}
	for _, finding := range result.Findings {
		if _, ok := failOn[finding.Status]; ok {
			return commandExitError{
				message: "documentation verification has " + finding.Status + " findings",
				code:    1,
			}
		}
	}
	return nil
}

func docsVerifyEnvelopeForResult(result doctruth.VerificationResult, err error) docsVerifyEnvelope {
	envelope := docsVerifyEnvelope{
		Data: docsVerifyData{
			Findings:        result.Findings,
			EvidencePackets: result.EvidencePackets,
			Summary:         result.Summary,
			Truncated:       result.Truncated,
		},
		Truth: map[string]any{
			"capability": "documentation.verify",
			"basis":      "active documentation claim verification",
			"freshness":  map[string]any{"state": "fresh"},
		},
	}
	if err != nil {
		envelope.Error = &docsVerifyError{Code: "documentation_verification_failed", Message: err.Error()}
	}
	return envelope
}

func renderDocsVerifyText(w io.Writer, result doctruth.VerificationResult) error {
	summary := result.Summary
	if _, err := fmt.Fprintf(
		w,
		"Docs verify: documents=%d claims=%d valid=%d contradicted=%d missing_evidence=%d unsupported=%d truncated=%t\n",
		summary.DocumentsScanned,
		summary.ClaimsChecked,
		summary.Valid,
		summary.Contradicted,
		summary.MissingEvidence,
		summary.UnsupportedClaimType,
		result.Truncated,
	); err != nil {
		return err
	}
	for _, finding := range result.Findings {
		if finding.Status == doctruth.VerificationStatusValid {
			continue
		}
		if _, err := fmt.Fprintf(w, "- %s %s %s\n", finding.Status, finding.ClaimType, finding.NormalizedClaim); err != nil {
			return err
		}
	}
	return nil
}

func writeDocsVerifyJSON(w io.Writer, envelope docsVerifyEnvelope) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope)
}

func splitCSV(value string) []string {
	parts := []string{}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}
