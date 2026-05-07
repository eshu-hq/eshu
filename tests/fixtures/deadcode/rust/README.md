# Rust Dead-Code Fixture Intent

Maturity: `derived_candidate_only`.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unused_cleanup_candidate` |
| `direct_reference` | `directly_used_helper` |
| `entrypoint` | `main` |
| `public_api` | `public_status` |
| `framework_root` | `registered_handler` |
| `semantic_dispatch` | `Task::run` |
| `excluded` | `generated_excluded_helper` |
| `ambiguous` | `DYNAMIC_FUNCTION_NAME` |
