-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- The coordinator advances a finite source cycle with admission in one
-- transaction. Sequence order is a traversal key, not a publication order:
-- a late commit below the cursor is revisited on the next complete cycle.
CREATE TABLE IF NOT EXISTS scanner_image_discovery_cursors (
    scanner_collector_instance_id TEXT NOT NULL,
    authorization_namespace TEXT NOT NULL
      CHECK (authorization_namespace IN ('local','hosted')),
    tenant_id TEXT NOT NULL DEFAULT '',
    workspace_id TEXT NOT NULL DEFAULT '',
    subject_class TEXT NOT NULL DEFAULT '',
    policy_revision_hash TEXT NOT NULL DEFAULT '',
    discovery_policy_revision BIGINT NOT NULL CHECK (discovery_policy_revision > 0),
    version BIGINT NOT NULL CHECK (version >= 0),
    cycle_upper_observation_id BIGINT NOT NULL CHECK (cycle_upper_observation_id >= 0),
    after_observation_id BIGINT NOT NULL CHECK (after_observation_id >= 0),
    after_authorization_domain TEXT NOT NULL DEFAULT '',
    advanced_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scanner_collector_instance_id,authorization_namespace,
                 tenant_id,workspace_id,subject_class,policy_revision_hash,
                 discovery_policy_revision),
    CHECK (after_observation_id <= cycle_upper_observation_id),
    CHECK ((authorization_namespace='local' AND tenant_id='' AND workspace_id=''
            AND subject_class='' AND policy_revision_hash='')
       OR (authorization_namespace='hosted' AND tenant_id<>'' AND workspace_id<>''
            AND subject_class<>'' AND policy_revision_hash<>''))
);
REVOKE ALL ON scanner_image_discovery_cursors FROM PUBLIC;
