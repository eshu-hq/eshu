package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	lambdasvc "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lambda"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestAWSLambdaCommitDoesNotPersistPlaintextEnvironmentValues(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 14, 12, 30, 0, 0, time.UTC)
	databaseURL := "postgres://user:password@example.internal/app"
	logLevel := "public-looking-but-still-runtime-config"
	key, err := redact.NewKey([]byte("aws-no-plaintext-proof-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}

	boundary := awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceLambda,
		ScopeID:             "aws:123456789012:us-east-1:lambda",
		GenerationID:        "aws:123456789012:us-east-1:lambda:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          observedAt,
	}
	scanner := lambdasvc.Scanner{
		Client: lambdaNoPlaintextClient{
			functions: []lambdasvc.Function{{
				ARN:         "arn:aws:lambda:us-east-1:123456789012:function:api",
				Name:        "api",
				PackageType: "Zip",
				Runtime:     "nodejs20.x",
				Environment: map[string]string{
					"DATABASE_URL": databaseURL,
					"LOG_LEVEL":    logLevel,
				},
			}},
		},
		RedactionKey: key,
	}

	envelopes, err := scanner.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: nil,
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return observedAt }
	store.SkipRelationshipBackfill = true

	scopeValue := scope.IngestionScope{
		ScopeID:       boundary.ScopeID,
		SourceSystem:  string(scope.CollectorAWS),
		ScopeKind:     scope.KindRegion,
		CollectorKind: scope.CollectorAWS,
		PartitionKey:  "aws:123456789012:us-east-1:lambda",
		Metadata: map[string]string{
			"account_id":   boundary.AccountID,
			"region":       boundary.Region,
			"service_kind": boundary.ServiceKind,
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID:  boundary.GenerationID,
		ScopeID:       boundary.ScopeID,
		ObservedAt:    observedAt,
		IngestedAt:    observedAt,
		Status:        scope.GenerationStatusPending,
		TriggerKind:   scope.TriggerKindSnapshot,
		FreshnessHint: boundary.GenerationID,
	}
	if err := store.CommitScopeGeneration(
		context.Background(),
		scopeValue,
		generation,
		testFactChannel(envelopes),
	); err != nil {
		t.Fatalf("CommitScopeGeneration() error = %v, want nil", err)
	}
	if !db.tx.committed {
		t.Fatal("transaction committed = false, want true")
	}

	assertExecArgsDoNotContain(t, db.tx.execs, []string{
		databaseURL,
		logLevel,
	})
	assertExecArgsContain(t, db.tx.execs, awscloud.RedactionPolicyVersion)
}

type lambdaNoPlaintextClient struct {
	functions []lambdasvc.Function
}

func (c lambdaNoPlaintextClient) ListFunctions(context.Context) ([]lambdasvc.Function, error) {
	return c.functions, nil
}

func (c lambdaNoPlaintextClient) ListAliases(
	context.Context,
	lambdasvc.Function,
) ([]lambdasvc.Alias, error) {
	return nil, nil
}

func (c lambdaNoPlaintextClient) ListEventSourceMappings(
	context.Context,
	lambdasvc.Function,
) ([]lambdasvc.EventSourceMapping, error) {
	return nil, nil
}

func assertExecArgsContain(t *testing.T, execs []fakeExecCall, needle string) {
	t.Helper()
	for _, exec := range execs {
		for _, arg := range exec.args {
			text := persistedArgText(arg)
			if strings.Contains(text, needle) {
				return
			}
		}
	}
	t.Fatalf("exec args did not contain %q", needle)
}
