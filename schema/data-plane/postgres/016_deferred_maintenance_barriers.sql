CREATE TABLE IF NOT EXISTS deferred_maintenance_barriers (
    barrier_name TEXT NOT NULL,
    epoch BIGINT NOT NULL,
    shard_count INTEGER NOT NULL CHECK (shard_count > 0),
    leader_shard_index INTEGER NULL,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (barrier_name, epoch)
);
CREATE TABLE IF NOT EXISTS deferred_maintenance_barrier_arrivals (
    barrier_name TEXT NOT NULL,
    epoch BIGINT NOT NULL,
    shard_index INTEGER NOT NULL CHECK (shard_index >= 0),
    arrived_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (barrier_name, epoch, shard_index),
    FOREIGN KEY (barrier_name, epoch)
        REFERENCES deferred_maintenance_barriers (barrier_name, epoch)
        ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS deferred_maintenance_barrier_arrivals_epoch_idx
    ON deferred_maintenance_barrier_arrivals (barrier_name, epoch, arrived_at DESC);
CREATE INDEX IF NOT EXISTS deferred_maintenance_barriers_updated_idx
    ON deferred_maintenance_barriers (updated_at DESC);
