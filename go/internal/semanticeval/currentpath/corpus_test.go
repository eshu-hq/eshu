package currentpath

import (
	"os"
	"strings"
	"testing"
)

const repoIDPlaceholder = "{repo_id}"

func TestCheckedInEshuPhase0SuiteContract(t *testing.T) {
	suiteData, err := os.Open("testdata/eshu_phase0_suite.json")
	if err != nil {
		t.Fatalf("open suite: %v", err)
	}
	defer func() {
		_ = suiteData.Close()
	}()

	suite, err := LoadSuiteJSON(suiteData)
	if err != nil {
		t.Fatalf("LoadSuiteJSON() error = %v, want nil", err)
	}
	if got, want := len(suite.Cases), 10; got != want {
		t.Fatalf("len(cases) = %d, want %d", got, want)
	}
	for _, evalCase := range suite.Cases {
		if evalCase.Scope["repo_id"] != repoIDPlaceholder {
			t.Fatalf("case %q repo_id = %q, want placeholder", evalCase.ID, evalCase.Scope["repo_id"])
		}
		for _, expected := range evalCase.Expected {
			if !strings.Contains(expected.Handle, repoIDPlaceholder) {
				t.Fatalf("case %q expected handle %q missing repo placeholder", evalCase.ID, expected.Handle)
			}
		}
	}
}
