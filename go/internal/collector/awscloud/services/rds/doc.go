// Package rds emits metadata-only Amazon RDS resource and relationship facts
// for the AWS cloud collector.
//
// The package owns scanner-level fact selection for DB instances, DB clusters,
// and DB subnet groups. It deliberately avoids database connections, secrets,
// snapshots, log contents, schemas, tables, row data, and workload ownership
// inference. AWS SDK pagination and API telemetry live in the awssdk adapter.
package rds
