# Python Dead-Code Fixture

This fixture covers Python `code_quality.dead_code` root modeling while keeping
Python maturity at `derived`. The cases are intentionally mixed across plain
calls, package/public surfaces, decorators, CLI commands, Lambda handlers,
dataclass models, properties, member calls, dynamic imports, generated files,
and tests.

## Expected Symbols

| Case | Symbol | File | Expected handling |
| --- | --- | --- | --- |
| `unused` | `unused_helper` | `app.py` | Returned as a candidate when no graph edge reaches it. |
| `direct_reference` | `direct_reference_target`, `EventModel.from_dict`, `LogPartition.from_event`, `LogProcessor.create_partition` | `app.py` | Reached by direct, inherited classmethod, and member calls. |
| `entrypoint` | `main` | `__main__.py` | Package execution entrypoint. |
| `public_api` | `PublicService` | `app.py` | Public module surface from `__all__`; maturity remains derived until policy is exact. |
| `framework_root` | `fastapi_health`, `flask_status`, `celery_sync`, `click_sync`, `typer_serve`, `lambda_handler`, `EventModel`, `EventModel.__post_init__`, `EventModel.object_url` | `app.py`, `cli.py`, `template.yaml` | Decorator, Lambda config, dataclass, and property roots modeled by parser metadata. |
| `semantic_dispatch` | `semantic_dispatch_target` | `dynamic_loader.py` | Reached by dynamic import/dispatch and therefore not exact. |
| `excluded` | `generated_client`, `test_helper` | `generated/client_pb2.py`, `tests/test_app.py` | Generated/test-owned code excluded by default. |
| `ambiguous` | `ambiguous_dynamic_target` | `dynamic_loader.py` | Dynamic import name is data-dependent and must keep truth non-exact. |
