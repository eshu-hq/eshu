-- Drop the narrow container-image-identity epoch index ahead of recreating it
-- with the Dockerfile base-image arm (#5460). Migration 077 creates the
-- replacement under the same name.
--
-- This is a file of its own, holding exactly ONE statement, because the
-- migration runner Execs each file as a single simple-query string and Postgres
-- treats a multi-statement string as an implicit transaction block -- and
-- DROP/CREATE INDEX CONCURRENTLY cannot run inside one. Migration 069 only
-- works for the same reason: it is a lone statement.
DROP INDEX CONCURRENTLY IF EXISTS fact_records_identity_epoch_idx;
