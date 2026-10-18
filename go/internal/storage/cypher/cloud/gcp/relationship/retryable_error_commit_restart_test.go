// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationship

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/stretchr/testify/require"
)

// closedPropertyKeyDictionaryCommitMsg is the RAW body from Ifa cell
// restart-backend-between-phase-groups (runs 36349511251, 36292315197,
// 36210727831), not built from the constant under test: a typo in the
// production constant must turn this red.
const closedPropertyKeyDictionaryCommitMsg = "commit failed: persisting property key dictionary: property key dictionary persistence requires an open database"

// TestBackendRestartCommitPropertyKeyDictionaryClosedReachesDurableQueue pins
// the sixth commit-side spelling of a NornicDB restart: the property key
// dictionary persisted at commit after Close has nil'd its database. The
// commit is refused before the storage write and the transaction rolls back,
// so the GCP relationship edge upsert must requeue instead of dead-lettering
// as projection_bug.
func TestBackendRestartCommitPropertyKeyDictionaryClosedReachesDurableQueue(t *testing.T) {
	t.Parallel()

	driverErr := &neo4jdriver.Neo4jError{
		Code: "Neo.ClientError.Transaction.TransactionCommitFailed",
		Msg:  closedPropertyKeyDictionaryCommitMsg,
	}
	inner := &backendRestartTerminalGroupExecutor{err: driverErr}
	writer := NewGCPCloudResourceEdgeWriter(inner, 0)
	writeErr := writer.WriteCloudResourceEdges(
		context.Background(), gcpCloudResourceEdgeRows(1), "scope-1", "gen-1", "reducer/gcp-relationships",
	)
	handlerErr := fmt.Errorf("write canonical gcp relationship edges: %w", writeErr)

	require.True(t, reducer.IsRetryable(handlerErr),
		"a closed property key dictionary at commit must requeue the edge write instead of dead-lettering it")
	var classified interface{ FailureClass() string }
	require.ErrorAs(t, handlerErr, &classified)
	require.Equal(t, sourcecypher.GraphWriteTimeoutFailureClass, classified.FailureClass())
	var gotDriverErr *neo4jdriver.Neo4jError
	require.ErrorAs(t, handlerErr, &gotDriverErr)
	require.Same(t, driverErr, gotDriverErr)
}
