package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/semanticeval"
	"github.com/eshu-hq/eshu/go/internal/semanticeval/currentpath"
)

const (
	defaultBaseURL = "http://localhost:8080"
	repoIDToken    = "{repo_id}"
)

func main() {
	if handled, err := printVersionFlag(os.Args[1:], os.Stdout); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		log.Fatal(err)
	}
}

func printVersionFlag(args []string, stdout io.Writer) (bool, error) {
	return buildinfo.PrintVersionFlag(args, stdout, "eshu-semantic-eval-currentpath")
}

func run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("semantic-eval-currentpath", flag.ContinueOnError)
	flags.SetOutput(stderr)

	suitePath := flags.String("suite", "", "path to current-path semantic eval suite JSON")
	baseURL := flags.String("base-url", apiBaseURLFromEnv(), "Eshu API base URL")
	repoID := flags.String("repo-id", "", "canonical repo id used to replace {repo_id} in the suite")
	runOutput := flags.String("run-output", "", "optional path for observed run JSON")
	reportOutput := flags.String("report-output", "", "optional path for score report JSON; stdout is used when omitted")
	k := flags.Int("k", 10, "top-K cutoff for retrieval metrics")
	timeout := flags.Duration("timeout", 0, "optional per-case request timeout override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*suitePath) == "" {
		return fmt.Errorf("suite path is required")
	}

	suiteFile, err := os.Open(*suitePath)
	if err != nil {
		return fmt.Errorf("open suite: %w", err)
	}
	defer func() {
		_ = suiteFile.Close()
	}()
	suite, err := currentpath.LoadSuiteJSON(suiteFile)
	if err != nil {
		return fmt.Errorf("load suite: %w", err)
	}
	trimmedRepoID := strings.TrimSpace(*repoID)
	if trimmedRepoID == "" && suiteContainsRepoIDPlaceholder(suite) {
		return fmt.Errorf("--repo-id is required when the suite contains %q placeholders", repoIDToken)
	}
	if trimmedRepoID != "" {
		suite = substituteRepoID(suite, trimmedRepoID)
	}

	runResult, err := currentpath.Runner{
		BaseURL: *baseURL,
		Timeout: *timeout,
	}.Run(ctx, suite)
	if err != nil {
		return fmt.Errorf("run current path eval: %w", err)
	}
	report, err := semanticeval.Score(suite.EvalSuite(), runResult, semanticeval.Options{K: *k})
	if err != nil {
		return fmt.Errorf("score current path eval: %w", err)
	}

	if *runOutput != "" {
		if err := writeJSONFile(*runOutput, runResult); err != nil {
			return fmt.Errorf("write run output: %w", err)
		}
	}
	if *reportOutput != "" {
		if err := writeJSONFile(*reportOutput, report); err != nil {
			return fmt.Errorf("write report output: %w", err)
		}
		return nil
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	_, err = fmt.Fprintln(stdout, string(payload))
	return err
}

func apiBaseURLFromEnv() string {
	if value := strings.TrimSpace(os.Getenv("ESHU_API_URL")); value != "" {
		return value
	}
	return defaultBaseURL
}

func suiteContainsRepoIDPlaceholder(suite currentpath.Suite) bool {
	for _, evalCase := range suite.Cases {
		if mapContainsRepoIDPlaceholder(evalCase.Scope) {
			return true
		}
		for _, expected := range evalCase.Expected {
			if strings.Contains(expected.Handle, repoIDToken) {
				return true
			}
		}
		for _, handle := range evalCase.MustNotInclude {
			if strings.Contains(handle, repoIDToken) {
				return true
			}
		}
		if requestContainsRepoIDPlaceholder(evalCase.CurrentPath) {
			return true
		}
	}
	return false
}

func mapContainsRepoIDPlaceholder(values map[string]string) bool {
	for _, value := range values {
		if strings.Contains(value, repoIDToken) {
			return true
		}
	}
	return false
}

func requestContainsRepoIDPlaceholder(request currentpath.Request) bool {
	if strings.Contains(request.RepoID, repoIDToken) ||
		strings.Contains(request.Query, repoIDToken) ||
		strings.Contains(request.Language, repoIDToken) ||
		strings.Contains(request.Intent, repoIDToken) ||
		strings.Contains(request.SearchType, repoIDToken) {
		return true
	}
	for _, term := range request.Terms {
		if strings.Contains(term, repoIDToken) {
			return true
		}
	}
	return false
}

func substituteRepoID(suite currentpath.Suite, repoID string) currentpath.Suite {
	for caseIndex := range suite.Cases {
		evalCase := &suite.Cases[caseIndex]
		if evalCase.Scope != nil {
			for key, value := range evalCase.Scope {
				evalCase.Scope[key] = strings.ReplaceAll(value, repoIDToken, repoID)
			}
		}
		for expectedIndex := range evalCase.Expected {
			expected := &evalCase.Expected[expectedIndex]
			expected.Handle = strings.ReplaceAll(expected.Handle, repoIDToken, repoID)
		}
		for forbiddenIndex := range evalCase.MustNotInclude {
			evalCase.MustNotInclude[forbiddenIndex] = strings.ReplaceAll(evalCase.MustNotInclude[forbiddenIndex], repoIDToken, repoID)
		}
		request := &evalCase.CurrentPath
		request.RepoID = strings.ReplaceAll(request.RepoID, repoIDToken, repoID)
		request.Query = strings.ReplaceAll(request.Query, repoIDToken, repoID)
	}
	return suite
}

func writeJSONFile(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o644)
}
