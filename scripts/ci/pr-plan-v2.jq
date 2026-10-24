def lower_sha:
  type == "string" and test("^[0-9a-f]{40}$");

def positive_int($maximum):
  type == "number" and floor == . and . > 0 and . <= $maximum;

def selection:
  if (.workflows | type) != "array" or (.jobs | type) != "object" or (.gates | type) != "array" then
    false
  else
    (.jobs | keys | sort) == (.workflows | sort) and
    (.workflows | unique | length) == (.workflows | length) and
    all(.workflows[]; type == "string" and length > 0) and
    all(.jobs[]; type == "array" and length > 0 and all(.[]; type == "string" and length > 0)) and
    all(.gates[]; type == "string" and length > 0)
  end;

type == "object" and
.schema_version == 2 and
.head_sha == $sha and ($sha | lower_sha) and
(.target_run_id | positive_int(1000000000000000)) and .target_run_id == $run_id and
(.target_run_attempt | positive_int(1000)) and .target_run_attempt == $run_attempt and
(.merge_sha | lower_sha) and (.merge_tree | lower_sha) and
(.reason | type == "string" and length > 0) and
selection and
(if .mode == "reuse" then
   (.source_sha | lower_sha) and (.source_full_sha | lower_sha) and (.tree | lower_sha) and
   (.workflows | length == 0) and (.jobs | length == 0) and (.gates | length == 0)
 elif .mode == "smoke" then
   (.source_sha | lower_sha) and (.source_full_sha | lower_sha) and (.tree | lower_sha) and
   (.workflows | length > 0) and (.jobs | length > 0)
 elif .mode == "full" then
   (has("source_sha") | not) and (has("source_full_sha") | not) and (has("tree") | not) and
   (.workflows | length > 0) and (.jobs | length > 0)
 else false end)
