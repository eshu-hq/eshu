package terraformstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserEmitsSourceWarnings(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SourceWarnings = []terraformstate.SourceWarning{{
		WarningKind: "state_in_vcs",
		Reason:      "terraform state file was discovered in git and explicitly approved for ingestion",
		Source:      "git_local_file",
	}}

	result, err := terraformstate.Parse(
		context.Background(),
		strings.NewReader(`{"serial":17,"lineage":"lineage-123"}`),
		options,
	)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	warning := factByKind(t, result.Facts, facts.TerraformStateWarningFactKind)
	if got, want := warning.Payload["warning_kind"], "state_in_vcs"; got != want {
		t.Fatalf("warning_kind = %#v, want %#v", got, want)
	}
	if strings.Contains(warning.SourceRef.SourceURI, "terraform.tfstate") {
		t.Fatalf("SourceRef.SourceURI leaked raw state locator: %#v", warning.SourceRef)
	}
}
