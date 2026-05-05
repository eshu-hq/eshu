package cypher_test

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type compileExecutor struct{}

func (compileExecutor) Execute(context.Context, cypher.Statement) error {
	return nil
}

func (compileExecutor) ExecuteGroup(context.Context, []cypher.Statement) error {
	return nil
}

type compilePhaseExecutor struct{}

func (compilePhaseExecutor) ExecutePhaseGroup(context.Context, []cypher.Statement) error {
	return nil
}

func TestBackendNeutralContractsCompileWithoutNeo4jPackage(t *testing.T) {
	var executor cypher.Executor = compileExecutor{}
	var groupExecutor cypher.GroupExecutor = compileExecutor{}
	var phaseExecutor cypher.PhaseGroupExecutor = compilePhaseExecutor{}

	statement := cypher.Statement{
		Operation: cypher.OperationCanonicalUpsert,
		Cypher:    "RETURN 1",
		Parameters: map[string]any{
			cypher.StatementMetadataPhaseKey:       cypher.CanonicalPhaseFiles,
			cypher.StatementMetadataEntityLabelKey: "File",
		},
	}

	if err := executor.Execute(context.Background(), statement); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if err := groupExecutor.ExecuteGroup(context.Background(), []cypher.Statement{statement}); err != nil {
		t.Fatalf("ExecuteGroup() error = %v", err)
	}
	if err := phaseExecutor.ExecutePhaseGroup(context.Background(), []cypher.Statement{statement}); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v", err)
	}
}
