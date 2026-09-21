// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceKeyspaces identifies the regional Amazon Keyspaces (for Apache
	// Cassandra) metadata-only scan slice. The scanner reads keyspace and table
	// control-plane metadata through ListKeyspaces, GetKeyspace, ListTables, and
	// GetTable only. It never executes CQL, never runs ExecuteStatement,
	// BatchStatement, Select, or any data-plane read against table rows or cells,
	// and never invokes keyspace/table mutation APIs (CreateKeyspace,
	// DeleteKeyspace, UpdateKeyspace, CreateTable, DeleteTable, UpdateTable,
	// RestoreTable, CreateType, DeleteType, TagResource, UntagResource).
	ServiceKeyspaces = "keyspaces"
)

const (
	// ResourceTypeKeyspacesKeyspace identifies an Amazon Keyspaces keyspace
	// metadata resource. The scanner emits keyspace identity, ARN, and
	// replication strategy metadata only; it carries no table row or cell data.
	ResourceTypeKeyspacesKeyspace = "aws_keyspaces_keyspace"
	// ResourceTypeKeyspacesTable identifies an Amazon Keyspaces table metadata
	// resource. The scanner emits table identity, ARN, status, encryption
	// metadata, point-in-time-recovery status, capacity mode, and structural
	// schema column names and types only. Table row data, cell values, and CQL
	// query results are never read or persisted.
	ResourceTypeKeyspacesTable = "aws_keyspaces_table"
)

const (
	// RelationshipKeyspacesTableInKeyspace records that an Amazon Keyspaces table
	// is stored in its parent keyspace. The target is the keyspace resource keyed
	// by its ARN.
	RelationshipKeyspacesTableInKeyspace = "keyspaces_table_in_keyspace"
	// RelationshipKeyspacesTableUsesKMSKey records an Amazon Keyspaces table's
	// reported customer-managed server-side encryption KMS key dependency. The
	// target is the KMS key keyed by its ARN.
	RelationshipKeyspacesTableUsesKMSKey = "keyspaces_table_uses_kms_key"
)
