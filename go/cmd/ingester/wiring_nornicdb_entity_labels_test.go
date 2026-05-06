package main

import (
	"strings"
	"testing"
	"time"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

func TestNornicDBEntityLabelBatchSizes(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelBatchSizes(func(string) string { return "" }, 100)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelBatchSizes() error = %v, want nil", err)
	}
	if got["Function"] != defaultNornicDBFunctionEntityBatchSize {
		t.Fatalf("Function batch size = %d, want %d", got["Function"], defaultNornicDBFunctionEntityBatchSize)
	}
	if got["Struct"] != defaultNornicDBStructEntityBatchSize {
		t.Fatalf("Struct batch size = %d, want %d", got["Struct"], defaultNornicDBStructEntityBatchSize)
	}
	if got["Variable"] != defaultNornicDBVariableEntityBatchSize {
		t.Fatalf("Variable batch size = %d, want %d", got["Variable"], defaultNornicDBVariableEntityBatchSize)
	}
	if got["K8sResource"] != 1 {
		t.Fatalf("K8sResource batch size = %d, want 1", got["K8sResource"])
	}
}

func TestNornicDBEntityLabelBatchSizesClampToEntityBatchSize(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelBatchSizes(func(string) string { return "" }, 40)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelBatchSizes() error = %v, want nil", err)
	}
	if got["Function"] != defaultNornicDBFunctionEntityBatchSize {
		t.Fatalf("Function batch size = %d, want %d", got["Function"], defaultNornicDBFunctionEntityBatchSize)
	}
	if got["Struct"] != 40 {
		t.Fatalf("Struct batch size = %d, want 40", got["Struct"])
	}
	if got["Variable"] != 40 {
		t.Fatalf("Variable batch size = %d, want 40", got["Variable"])
	}
	if got["K8sResource"] != defaultNornicDBK8sResourceEntityBatchSize {
		t.Fatalf("K8sResource batch size = %d, want %d", got["K8sResource"], defaultNornicDBK8sResourceEntityBatchSize)
	}
}

func TestNornicDBEntityLabelBatchSizesDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelBatchSizes(func(string) string { return "" }, 100)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelBatchSizes() error = %v, want nil", err)
	}
	if got["Function"] != defaultNornicDBFunctionEntityBatchSize {
		t.Fatalf("Function batch size = %d, want %d", got["Function"], defaultNornicDBFunctionEntityBatchSize)
	}
	if got["Struct"] != defaultNornicDBStructEntityBatchSize {
		t.Fatalf("Struct batch size = %d, want %d", got["Struct"], defaultNornicDBStructEntityBatchSize)
	}
	if got["Variable"] != defaultNornicDBVariableEntityBatchSize {
		t.Fatalf("Variable batch size = %d, want %d", got["Variable"], defaultNornicDBVariableEntityBatchSize)
	}
	if got["K8sResource"] != defaultNornicDBK8sResourceEntityBatchSize {
		t.Fatalf("K8sResource batch size = %d, want %d", got["K8sResource"], defaultNornicDBK8sResourceEntityBatchSize)
	}
}

func TestNornicDBEntityLabelBatchSizesFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelBatchSizes(func(key string) string {
		if key == nornicDBEntityLabelBatchSizesEnv {
			return "Function=30,Struct=40,Variable=35,Class=75"
		}
		return ""
	}, 100)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelBatchSizes() error = %v, want nil", err)
	}
	if got["Function"] != 30 {
		t.Fatalf("Function batch size = %d, want 30", got["Function"])
	}
	if got["Struct"] != 40 {
		t.Fatalf("Struct batch size = %d, want 40", got["Struct"])
	}
	if got["Variable"] != 35 {
		t.Fatalf("Variable batch size = %d, want 35", got["Variable"])
	}
	if got["Class"] != 75 {
		t.Fatalf("Class batch size = %d, want 75", got["Class"])
	}
}

func TestNornicDBEntityLabelBatchSizesCapsEnvByEntityBatchSize(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelBatchSizes(func(key string) string {
		if key == nornicDBEntityLabelBatchSizesEnv {
			return "Function=30,Struct=80,Variable=80"
		}
		return ""
	}, 50)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelBatchSizes() error = %v, want nil", err)
	}
	if got["Function"] != 30 {
		t.Fatalf("Function batch size = %d, want 30", got["Function"])
	}
	if got["Struct"] != 50 {
		t.Fatalf("Struct batch size = %d, want 50", got["Struct"])
	}
	if got["Variable"] != 50 {
		t.Fatalf("Variable batch size = %d, want 50", got["Variable"])
	}
}

func TestNornicDBEntityLabelBatchSizesRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBEntityLabelBatchSizes(func(key string) string {
		if key == nornicDBEntityLabelBatchSizesEnv {
			return "Function=nope"
		}
		return ""
	}, 100)
	if err == nil {
		t.Fatal("nornicDBEntityLabelBatchSizes() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBEntityLabelBatchSizesEnv) {
		t.Fatalf("nornicDBEntityLabelBatchSizes() error = %q, want env name", err)
	}
}

func TestNornicDBEntityLabelPhaseGroupStatementsDefault(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelPhaseGroupStatements(func(string) string { return "" }, defaultNornicDBEntityPhaseStatements)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelPhaseGroupStatements() error = %v, want nil", err)
	}
	if got["Function"] != defaultNornicDBFunctionEntityPhaseStatements {
		t.Fatalf("Function phase statements = %d, want %d", got["Function"], defaultNornicDBFunctionEntityPhaseStatements)
	}
	if got["Struct"] != defaultNornicDBStructEntityPhaseStatements {
		t.Fatalf("Struct phase statements = %d, want %d", got["Struct"], defaultNornicDBStructEntityPhaseStatements)
	}
	if got["Variable"] != defaultNornicDBVariableEntityPhaseStatements {
		t.Fatalf("Variable phase statements = %d, want %d", got["Variable"], defaultNornicDBVariableEntityPhaseStatements)
	}
	if got["K8sResource"] != defaultNornicDBK8sResourceEntityPhaseStatements {
		t.Fatalf("K8sResource phase statements = %d, want %d", got["K8sResource"], defaultNornicDBK8sResourceEntityPhaseStatements)
	}
}

func TestNornicDBEntityLabelPhaseGroupStatementsFromEnv(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelPhaseGroupStatements(func(key string) string {
		if key == nornicDBEntityLabelPhaseGroupStatementsEnv {
			return "Function=8,Struct=12,Variable=4,Class=20"
		}
		return ""
	}, defaultNornicDBEntityPhaseStatements)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelPhaseGroupStatements() error = %v, want nil", err)
	}
	if got["Function"] != 8 {
		t.Fatalf("Function phase statements = %d, want 8", got["Function"])
	}
	if got["Struct"] != 12 {
		t.Fatalf("Struct phase statements = %d, want 12", got["Struct"])
	}
	if got["Variable"] != 4 {
		t.Fatalf("Variable phase statements = %d, want 4", got["Variable"])
	}
	if got["Class"] != 20 {
		t.Fatalf("Class phase statements = %d, want 20", got["Class"])
	}
}

func TestNornicDBEntityLabelPhaseGroupStatementsCapsEnvByEntityPhaseStatements(t *testing.T) {
	t.Parallel()

	got, err := nornicDBEntityLabelPhaseGroupStatements(func(key string) string {
		if key == nornicDBEntityLabelPhaseGroupStatementsEnv {
			return "Function=30,Struct=20,Variable=18"
		}
		return ""
	}, 15)
	if err != nil {
		t.Fatalf("nornicDBEntityLabelPhaseGroupStatements() error = %v, want nil", err)
	}
	if got["Function"] != 15 {
		t.Fatalf("Function phase statements = %d, want 15", got["Function"])
	}
	if got["Struct"] != 15 {
		t.Fatalf("Struct phase statements = %d, want 15", got["Struct"])
	}
	if got["Variable"] != 15 {
		t.Fatalf("Variable phase statements = %d, want 15", got["Variable"])
	}
}

func TestNornicDBEntityLabelPhaseGroupStatementsRejectsInvalidEnv(t *testing.T) {
	t.Parallel()

	_, err := nornicDBEntityLabelPhaseGroupStatements(func(key string) string {
		if key == nornicDBEntityLabelPhaseGroupStatementsEnv {
			return "Function=nope"
		}
		return ""
	}, defaultNornicDBEntityPhaseStatements)
	if err == nil {
		t.Fatal("nornicDBEntityLabelPhaseGroupStatements() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), nornicDBEntityLabelPhaseGroupStatementsEnv) {
		t.Fatalf("nornicDBEntityLabelPhaseGroupStatements() error = %q, want env name", err)
	}
}

func TestIngesterContentBeforeCanonicalOnlyLocalAuthoritative(t *testing.T) {
	t.Parallel()

	if !ingesterContentBeforeCanonical(func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "local_authoritative"
		}
		return ""
	}) {
		t.Fatal("ingesterContentBeforeCanonical(local_authoritative) = false, want true")
	}
	if ingesterContentBeforeCanonical(func(key string) string {
		if key == "ESHU_QUERY_PROFILE" {
			return "production"
		}
		return ""
	}) {
		t.Fatal("ingesterContentBeforeCanonical(production) = true, want false")
	}
}

func TestCanonicalTransactionTimeoutOnlyAppliesToNornicDB(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ESHU_CANONICAL_WRITE_TIMEOUT" {
			return "3s"
		}
		return ""
	}
	if got := canonicalTransactionTimeout(runtimecfg.GraphBackendNeo4j, getenv); got != 0 {
		t.Fatalf("canonicalTransactionTimeout(neo4j) = %s, want 0", got)
	}
	if got := canonicalTransactionTimeout(runtimecfg.GraphBackendNornicDB, getenv); got != 3*time.Second {
		t.Fatalf("canonicalTransactionTimeout(nornicdb) = %s, want 3s", got)
	}
}

func TestIngesterNeo4jExecutorTransactionConfigurersSetTimeout(t *testing.T) {
	t.Parallel()

	executor := ingesterNeo4jExecutor{TxTimeout: 4 * time.Second}
	configurers := executor.transactionConfigurers()
	if len(configurers) != 1 {
		t.Fatalf("transactionConfigurers count = %d, want 1", len(configurers))
	}
	var config neo4jdriver.TransactionConfig
	configurers[0](&config)
	if got := config.Timeout; got != 4*time.Second {
		t.Fatalf("transaction timeout = %s, want 4s", got)
	}
}
