-- Adds a real, non-secret display label to generated API tokens (issue
-- #3708). Prior to this column, only display_handle_hash (SHA-256 of the
-- operator-supplied label) was persisted, so no list surface could ever show
-- a human-readable name for a token. display_label is plaintext and
-- display-only; it is never used as a credential or lookup key.
ALTER TABLE identity_token_metadata
    ADD COLUMN IF NOT EXISTS display_label TEXT NULL;
