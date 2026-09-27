// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/ifa/materializededges"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// The workload_cloud_relationship determinism cell (#6228) drives an Odù of
// aws_resource facts, whose anchors name a workload but carry no
// workload-domain facts. The writer MATCHes
// `(workload:Workload {id})<-[:INSTANCE_OF]-(instance:WorkloadInstance)`, so
// without those two nodes the handler fails instances_not_ready forever:
// the workload pipeline that creates them in production has no fixture
// input in the cell. This subcommand creates exactly the positive anchor's
// endpoints -- nothing else -- following the materialize-platform-prerequisite
// precedent: idempotent MERGEs (safe under handler retries and repeated
// N-cell drives) plus a graph postcondition verify, so a seed that did not
// land fails here instead of surfacing later as zero assert-edges edges.
//
// It is NOT invented truth: the seeded identity is the Odù's own positive
// anchor (workload:orders-api in prod), keyed by the single source of truth
// the guard mapper shares (materializededges
// .WorkloadCloudRelationshipInstanceID). The negative and ambiguous anchors
// get no nodes, so the extractor's drop-never-invent restraint still has
// something to prove live.

const workloadEndpointsSeedCypher = `MERGE (w:Workload {id: $workload_id})
MERGE (i:WorkloadInstance {id: $instance_id})
SET i.environment = $environment
MERGE (i)-[:INSTANCE_OF]->(w)`

const (
	workloadEndpointsWorkloadCountCypher = `MATCH (w:Workload {id: $id}) RETURN count(w) AS count`
	workloadEndpointsInstanceCountCypher = `MATCH (i:WorkloadInstance {id: $id}) RETURN count(i) AS count`
	workloadEndpointsEdgeCountCypher     = `MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance {id: $instance_id}) RETURN count(i) AS count`
)

type workloadEndpointsOptions struct {
	workloadID  string
	environment string
}

type workloadEndpointsBackend interface {
	ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error
	CountWorkload(ctx context.Context, workloadID string) (int, error)
	CountInstance(ctx context.Context, instanceID string) (int, error)
	CountInstanceOf(ctx context.Context, workloadID, instanceID string) (int, error)
}

type workloadEndpointsBackendError struct {
	stage string
	cause error
}

func (e *workloadEndpointsBackendError) Error() string {
	return fmt.Sprintf("%s: %v", e.stage, e.cause)
}

func (e *workloadEndpointsBackendError) Unwrap() error {
	return e.cause
}

func newWorkloadEndpointsBackendError(stage string, cause error) error {
	return &workloadEndpointsBackendError{stage: stage, cause: cause}
}

func safeWorkloadEndpointsCommandError(err error) error {
	var backendErr *workloadEndpointsBackendError
	if errors.As(err, &backendErr) {
		return fmt.Errorf("ifa materialize-workload-endpoints: %s: check graph backend configuration and reachability (connection details redacted)", backendErr.stage)
	}
	return fmt.Errorf("ifa materialize-workload-endpoints: %w", err)
}

var openWorkloadEndpointsBackend = func(ctx context.Context) (workloadEndpointsBackend, func(), error) {
	driver, cfg, err := runtimecfg.OpenNeo4jDriver(ctx, os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	closeFn := func() { _ = driver.Close(context.Background()) }
	return &boltWorkloadEndpointsBackend{driver: driver, db: cfg.DatabaseName}, closeFn, nil
}

func parseMaterializeWorkloadEndpointsFlags(args []string, stderr io.Writer) (workloadEndpointsOptions, error) {
	fs := flag.NewFlagSet("ifa materialize-workload-endpoints", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var options workloadEndpointsOptions
	fs.StringVar(&options.workloadID, "workload-id", "", "Workload node id (workload:<name>) whose instance to seed")
	fs.StringVar(&options.environment, "environment", "", "instance environment (matched by the writer)")
	if err := fs.Parse(args); err != nil {
		return workloadEndpointsOptions{}, err //nolint:wrapcheck // flag errors are self-describing.
	}
	if fs.NArg() != 0 {
		return workloadEndpointsOptions{}, fmt.Errorf("ifa materialize-workload-endpoints: positional arguments are not accepted")
	}
	options.workloadID = strings.TrimSpace(options.workloadID)
	options.environment = strings.TrimSpace(options.environment)
	if options.workloadID == "" {
		return workloadEndpointsOptions{}, fmt.Errorf("ifa materialize-workload-endpoints: -workload-id is required")
	}
	if options.environment == "" {
		return workloadEndpointsOptions{}, fmt.Errorf("ifa materialize-workload-endpoints: -environment is required")
	}
	return options, nil
}

func runMaterializeWorkloadEndpointsCommand(
	ctx context.Context,
	args []string,
	stdout,
	stderr io.Writer,
) error {
	options, err := parseMaterializeWorkloadEndpointsFlags(args, stderr)
	if err != nil {
		return err
	}

	backend, closeFn, err := openWorkloadEndpointsBackend(ctx)
	if err != nil {
		return safeWorkloadEndpointsCommandError(newWorkloadEndpointsBackendError("open graph backend", err))
	}
	defer closeFn()

	instanceID, err := materializeWorkloadEndpoints(ctx, options, backend)
	if err != nil {
		return safeWorkloadEndpointsCommandError(err)
	}
	if _, err := fmt.Fprintf(stdout, "instance_id=%s verified=1\n", instanceID); err != nil {
		return fmt.Errorf("ifa materialize-workload-endpoints: write verified endpoint result: check output destination")
	}
	return nil
}

// materializeWorkloadEndpoints MERGEs the positive anchor's Workload and
// WorkloadInstance nodes plus their INSTANCE_OF edge, then verifies the
// graph postcondition (exactly one of each). A verify miss fails: the seed
// reporting success while the nodes are absent would surface later as zero
// assert-edges edges with no pointer back here.
func materializeWorkloadEndpoints(
	ctx context.Context,
	options workloadEndpointsOptions,
	backend workloadEndpointsBackend,
) (string, error) {
	instanceID := materializededges.WorkloadCloudRelationshipInstanceID(options.workloadID, options.environment)
	if instanceID == "" || options.workloadID == "" || options.environment == "" {
		return "", fmt.Errorf("derive instance id: insufficient identity input")
	}

	if err := backend.ExecuteCypher(ctx, workloadEndpointsSeedCypher, map[string]any{
		"workload_id": options.workloadID,
		"instance_id": instanceID,
		"environment": options.environment,
	}); err != nil {
		return "", newWorkloadEndpointsBackendError("seed workload endpoints", err)
	}

	workloadCount, err := backend.CountWorkload(ctx, options.workloadID)
	if err != nil {
		return "", newWorkloadEndpointsBackendError("verify Workload", err)
	}
	instanceCount, err := backend.CountInstance(ctx, instanceID)
	if err != nil {
		return "", newWorkloadEndpointsBackendError("verify WorkloadInstance", err)
	}
	edgeCount, err := backend.CountInstanceOf(ctx, options.workloadID, instanceID)
	if err != nil {
		return "", newWorkloadEndpointsBackendError("verify INSTANCE_OF", err)
	}
	if workloadCount != 1 || instanceCount != 1 || edgeCount != 1 {
		return "", fmt.Errorf("verified %d Workload / %d WorkloadInstance / %d INSTANCE_OF for %q, want 1/1/1", workloadCount, instanceCount, edgeCount, instanceID)
	}
	return instanceID, nil
}

type boltWorkloadEndpointsBackend struct {
	driver neo4j.DriverWithContext
	db     string
}

func (b *boltWorkloadEndpointsBackend) ExecuteCypher(
	ctx context.Context,
	cypher string,
	params map[string]any,
) error {
	result, err := neo4j.ExecuteQuery(
		ctx,
		b.driver,
		cypher,
		params,
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(b.db),
	)
	if err != nil {
		return fmt.Errorf("execute workload endpoints seed: %w", err)
	}
	if result == nil {
		return fmt.Errorf("execute workload endpoints seed: nil result")
	}
	return nil
}

func (b *boltWorkloadEndpointsBackend) CountWorkload(ctx context.Context, workloadID string) (int, error) {
	return b.countByID(ctx, workloadEndpointsWorkloadCountCypher, workloadID)
}

func (b *boltWorkloadEndpointsBackend) CountInstance(ctx context.Context, instanceID string) (int, error) {
	return b.countByID(ctx, workloadEndpointsInstanceCountCypher, instanceID)
}

func (b *boltWorkloadEndpointsBackend) CountInstanceOf(ctx context.Context, workloadID, instanceID string) (int, error) {
	result, err := neo4j.ExecuteQuery(
		ctx,
		b.driver,
		workloadEndpointsEdgeCountCypher,
		map[string]any{"workload_id": workloadID, "instance_id": instanceID},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(b.db),
	)
	if err != nil {
		return 0, fmt.Errorf("execute endpoint edge count: %w", err)
	}
	if result == nil {
		return 0, fmt.Errorf("endpoint edge count returned a nil result")
	}
	if len(result.Records) != 1 {
		return 0, fmt.Errorf("endpoint edge count returned %d rows, want 1", len(result.Records))
	}
	count, _, err := neo4j.GetRecordValue[int64](result.Records[0], "count")
	if err != nil {
		return 0, fmt.Errorf("read endpoint edge count: %w", err)
	}
	return int(count), nil
}

func (b *boltWorkloadEndpointsBackend) countByID(ctx context.Context, cypher, id string) (int, error) {
	result, err := neo4j.ExecuteQuery(
		ctx,
		b.driver,
		cypher,
		map[string]any{"id": id},
		neo4j.EagerResultTransformer,
		neo4j.ExecuteQueryWithDatabase(b.db),
	)
	if err != nil {
		return 0, fmt.Errorf("execute endpoint count: %w", err)
	}
	if result == nil {
		return 0, fmt.Errorf("endpoint count returned a nil result")
	}
	if len(result.Records) != 1 {
		return 0, fmt.Errorf("endpoint count returned %d rows, want 1", len(result.Records))
	}
	count, _, err := neo4j.GetRecordValue[int64](result.Records[0], "count")
	if err != nil {
		return 0, fmt.Errorf("read endpoint count: %w", err)
	}
	return int(count), nil
}

var _ workloadEndpointsBackend = (*boltWorkloadEndpointsBackend)(nil)
