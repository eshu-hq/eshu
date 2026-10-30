// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// manifestDependencyQueryExecMode pins the pgx exec mode of every page of
// ListActivePackageManifestDependencyFacts, for both query texts (#9586).
//
// Under the pool default, QueryExecModeCacheStatement, each SQL text becomes a
// named prepared statement per connection. PostgreSQL then switches it to the
// generic plan from its sixth execution, because the generic cost estimate is
// no higher than the custom one. The generic plan keeps the custom plan's
// shape and buffers, but filters the folded entity_name = ANY($2) against a
// Param array with a linear compare on every scanned row. A custom plan sees
// a Const array, which PostgreSQL hashes once it has nine or more elements
// (convert_saop_to_hashed_saop in src/backend/optimizer/util/clauses.c). The
// sbt branch's folded entity_name <> ALL($2) has the same trap. Every later
// read on that long-lived reducer connection paid the generic cost from page one.
//
// QueryExecModeCacheDescribe runs Parse, Bind and Execute on the unnamed
// statement, which PostgreSQL plans with the bound values on every execution.
// It caches only the parameter and result description per connection, so
// parameter and result formats match the pool default; the scan path is unchanged.
// It needs the pgx description cache, which is on by default (capacity 512;
// a DSN that sets description_cache_capacity=0 makes this read fail loudly
// with "cannot use QueryExecModeCacheDescribe with disabled description
// cache", never return wrong rows). The cost is one replan per page.
//
// Performance Evidence: see docs/internal/evidence/9586-manifest-read-custom-plan.md.
const manifestDependencyQueryExecMode = pgx.QueryExecModeCacheDescribe

// Read label values of the eshu_dp_package_manifest_dependency_read_*
// histograms: which query text the read ran. unclassified marks a query text
// missing from manifestDependencyReadLabels, which
// TestManifestDependencyQueryTextsAllHaveReadLabels refuses.
const (
	manifestDependencyReadPlain        = "plain"
	manifestDependencyReadMavenFamily  = "maven_family"
	manifestDependencyReadUnclassified = "unclassified"
)

// manifestDependencyReadLabels names the read label of every query text
// activePackageManifestDependencyQuery can return. A new text needs a row
// here; without one its reads record read=unclassified, not plain.
var manifestDependencyReadLabels = map[string]string{
	listActivePackageManifestDependencyFactsQuery:        manifestDependencyReadPlain,
	listActivePackageManifestDependencyFactsWithSBTQuery: manifestDependencyReadMavenFamily,
}

// recordManifestDependencyRead records one completed manifest dependency
// read: its wall time since started and its page count, labeled by which query
// text it ran. Every page but the last returns exactly listFactsByKindPageSize
// rows, so the page count follows from the row total.
func (s FactStore) recordManifestDependencyRead(ctx context.Context, query string, started time.Time, rows int) {
	if s.instruments == nil {
		return
	}
	read, ok := manifestDependencyReadLabels[query]
	if !ok {
		read = manifestDependencyReadUnclassified
	}
	attrs := metric.WithAttributes(telemetry.AttrRead(read))
	if s.instruments.PackageManifestDependencyReadDuration != nil {
		s.instruments.PackageManifestDependencyReadDuration.Record(ctx, time.Since(started).Seconds(), attrs)
	}
	if s.instruments.PackageManifestDependencyReadPages != nil {
		s.instruments.PackageManifestDependencyReadPages.Record(ctx, int64(rows/listFactsByKindPageSize+1), attrs)
	}
}
