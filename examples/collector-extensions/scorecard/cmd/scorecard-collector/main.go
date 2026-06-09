package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	scorecard "github.com/eshu-hq/eshu/examples/collector-extensions/scorecard"
	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
)

func main() {
	inputPath := flag.String("input", "testdata/complete.json", "Scorecard JSON input file")
	sourceURI := flag.String("source-uri", "https://api.securityscorecards.dev/projects/github.com/example/widgets", "safe source URI for emitted facts")
	previousDigest := flag.String("previous-digest", "", "previous report digest for unchanged detection")
	flag.Parse()

	file, err := os.Open(*inputPath)
	if err != nil {
		fail(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fail(err)
		}
	}()

	report, err := scorecard.LoadReport(file)
	if err != nil {
		fail(err)
	}
	result, err := scorecard.Collect(demoClaim(), report, scorecard.CollectOptions{
		ObservedAt:     time.Now().UTC(),
		SourceURI:      *sourceURI,
		PreviousDigest: *previousDigest,
	})
	if err != nil {
		fail(err)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fail(err)
	}
}

func demoClaim() sdk.Claim {
	now := time.Now().UTC()
	return sdk.Claim{
		ComponentID:   scorecard.ComponentID,
		InstanceID:    "scorecard-local",
		CollectorKind: scorecard.CollectorKind,
		SourceSystem:  scorecard.SourceSystem,
		Scope: sdk.Scope{
			ID:   "github.com/example/widgets",
			Kind: "repository",
		},
		SourceRunID:  "run-local",
		GenerationID: "generation-local",
		WorkItemID:   "work-local",
		FencingToken: "fence-local",
		Attempt:      1,
		Deadline:     now.Add(5 * time.Minute),
		ConfigHandle: "config://examples/scorecard/local",
	}
}

func fail(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "scorecard-collector: %v\n", err)
	os.Exit(1)
}
