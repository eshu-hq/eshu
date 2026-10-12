-- 7759 WAL arms: btree-only copies, GINs excluded (same cost in both arms).
CREATE TABLE ce_wal_with (LIKE content_entities INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
CREATE TABLE ce_wal_without (LIKE content_entities INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
-- 004 btree set on both
CREATE INDEX ce_wal_with_repo_idx ON ce_wal_with (repo_id);
CREATE INDEX ce_wal_with_type_idx ON ce_wal_with (entity_type);
CREATE INDEX ce_wal_with_path_idx ON ce_wal_with (relative_path);
CREATE INDEX ce_wal_with_artifact_idx ON ce_wal_with (artifact_type);
CREATE INDEX ce_wal_with_template_idx ON ce_wal_with (template_dialect);
CREATE INDEX ce_wal_with_iac_idx ON ce_wal_with (iac_relevant);
CREATE INDEX ce_wal_with_repo_entity_idx ON ce_wal_with (repo_id, entity_id);
CREATE INDEX ce_wal_with_k8s_partial_idx ON ce_wal_with (repo_id, relative_path, start_line, entity_id) INCLUDE (entity_name) WHERE entity_type = 'K8sResource';
CREATE INDEX ce_wal_with_lang_type_idx ON ce_wal_with (language, entity_type);
CREATE INDEX ce_wal_with_lang_type_path_idx ON ce_wal_with (language, entity_type, relative_path, start_line, entity_name);
CREATE INDEX ce_wal_with_repo_path_start_idx ON ce_wal_with (repo_id, relative_path, start_line, entity_id);
CREATE INDEX ce_wal_with_repo_lang_type_idx ON ce_wal_with (repo_id, language, entity_type);
CREATE INDEX ce_wal_without_type_idx ON ce_wal_without (entity_type);
CREATE INDEX ce_wal_without_path_idx ON ce_wal_without (relative_path);
CREATE INDEX ce_wal_without_artifact_idx ON ce_wal_without (artifact_type);
CREATE INDEX ce_wal_without_template_idx ON ce_wal_without (template_dialect);
CREATE INDEX ce_wal_without_iac_idx ON ce_wal_without (iac_relevant);
CREATE INDEX ce_wal_without_repo_entity_idx ON ce_wal_without (repo_id, entity_id);
CREATE INDEX ce_wal_without_k8s_partial_idx ON ce_wal_without (repo_id, relative_path, start_line, entity_id) INCLUDE (entity_name) WHERE entity_type = 'K8sResource';
CREATE INDEX ce_wal_without_lang_type_idx ON ce_wal_without (language, entity_type);
CREATE INDEX ce_wal_without_lang_type_path_idx ON ce_wal_without (language, entity_type, relative_path, start_line, entity_name);
CREATE INDEX ce_wal_without_repo_path_start_idx ON ce_wal_without (repo_id, relative_path, start_line, entity_id);
CREATE INDEX ce_wal_without_repo_lang_type_idx ON ce_wal_without (repo_id, language, entity_type);
