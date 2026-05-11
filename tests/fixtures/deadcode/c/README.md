# C Dead-Code Fixture Intent

Maturity: `derived`.

The public API case is declared in `fixture.h` and included by `fixture.c` so
the parser can prove the root without treating every non-static function as
public.

Expected symbols:

| Case | Symbol |
| --- | --- |
| `unused` | `unused_cleanup_candidate` |
| `direct_reference` | `directly_used_helper` |
| `entrypoint` | `main` |
| `public_api` | `eshu_c_public_api` |
| `framework_root` | `registered_signal_handler` |
| `semantic_dispatch` | `dispatch_target` |
| `excluded` | `generated_excluded_helper` |
| `ambiguous` | `dynamic_lookup_name` |
