# Coding Conventions

**Analysis Date:** 2026-04-13

## Naming Patterns

**Files:**
- Lowercase with underscores: `python_support.py`, `structured_logging.py`, `graph_builder.py`
- Module files pair with support files: `python.py` + `python_support.py` (parser implementations)
- Routers in API: `services.py`, `code.py`, `repositories.py` (domain-based naming)
- Test files: `test_python_docstrings.py`, `test_postgres.py` (prefix with `test_`)

**Functions:**
- snake_case: `get_app_home()`, `normalize_entity_type()`, `create_snapshot_fact_emitter()`
- Private functions prefixed with underscore: `_canonical_prefix()`, `_is_canonical_id_for_type()`, `_cursor()`
- Factory functions named `get_` or `create_`: `get_database_manager()`, `create_app()`, `create_parser()`

**Variables:**
- snake_case for all variables: `canonical_id`, `source_run_id`, `temp_test_dir`
- Constants in SCREAMING_SNAKE_CASE: `TEST_ROOT`, `SAMPLE_PROJECTS_DIR`, `FIXTURES_DIR`, `_CANONICAL_ID_RE`
- Enum members lowercase: `repository`, `content_entity`, `workload`, `service`
- Private module-level variables prefixed with underscore: `_LOGGING_STATE`, `_ENTITY_TYPE_ALIASES`, `_RESERVED_KEYS`

**Types:**
- Classes PascalCase: `EntityType`, `WorkloadKind`, `PostgresContentProvider`, `PythonTreeSitterParser`
- Dataclasses use `@dataclass(slots=True)`: `_LoggingState`, `ContentFileEntry`, `ContentEntityEntry`
- Enum classes PascalCase inheriting from `Enum` or `str, Enum`: `EntityType(str, Enum)`, `WorkloadKind(str, Enum)`
- Type hints use modern syntax: `dict[str, str]`, `list[FactRecordRow]`, `str | None` (union with `|` not `Union`)

## Code Style

**Formatting:**
- Black for code formatting (configured in `pyproject.toml` with `target-version = ["py310"]`)
- Line length: implicit (Black default ~88 chars)
- Exclude patterns in Black config: docs/site, egg-info, test fixtures

**Linting:**
- Ruff is the primary linter (cache at `.ruff_cache/`)
- Python 3.10+ minimum requirement
- Pre-commit checks via scripts:
  - `python3 scripts/check_python_file_lengths.py --max-lines 500` - Max 500 lines per file
  - `python3 scripts/check_python_docstrings.py` - Enforce docstrings on modules, classes, functions

**Import Organization:**

Order:
1. `from __future__ import annotations` (always first if present)
2. Standard library imports: `import os`, `from pathlib import Path`, `from typing import Mapping`
3. Third-party imports: `from pydantic import BaseModel`, `from fastapi import APIRouter`, `import pytest`
4. Relative imports: `from ..domain.entities import EntityType`, `from .dependencies import QueryServices`

All files in `src/` use `from __future__ import annotations` for forward references (250+ files consistently apply this).

**Path Aliases:**
- All relative imports use dot notation: `from ...domain.entities`, `from ..dependencies`
- No absolute path imports within the package; use relative imports

## Error Handling

**Patterns:**
- Exceptions are caught with type-specific handlers: `except ServiceAliasError as exc:`
- Custom exceptions used for domain errors: `ServiceAliasError`, `FactWorkItemError`
- Error messages passed to response builders: `problem_response(request, title="...", detail=str(exc))`
- Try-except blocks appear in API routers for service layer errors (`api/routers/services.py`)
- Database exceptions caught and logged: error tracking via `core/database_*.py` implementations

**Exception Hierarchy:**
- Define custom exceptions in domain modules: `ServiceAliasError` in `query/context.py`
- Use builtin exceptions for type errors and value errors
- Never silently swallow exceptions; log or re-raise with context

## Logging

**Framework:** Python standard library `logging` module (not third-party)

**Patterns:**
- Structured logging via JSON formatted records: `observability/structured_logging.py`
- Each log entry includes: `timestamp`, `severity_text`, `severity_number`, `message`, `component`, `service_name`, `trace_id`, `span_id`, `request_id`, `correlation_id`
- Debug logging available via `utils/debug_log.py` with `info_logger` helper
- OTEL integration: trace context automatically injected into logs
- Log levels configured via env var: `LOG_LEVEL` controls verbosity (maps to DEBUG, INFO, WARNING, ERROR, CRITICAL)

**When to Log:**
- Entry/exit of major functions: rare, only for long-running operations
- Error conditions with context: always
- State transitions in runtime loops: always
- Database operations: via OTEL spans (not direct logging)
- Parse completion: summarized, not per-file

**Example:**
```python
import logging
logger = logging.getLogger(__name__)
logger.info("Repository indexed", extra={"repo_id": repo_id, "file_count": count})
```

## Comments

**When to Comment:**
- Explain WHY, not WHAT (code should be self-documenting for WHAT)
- Non-obvious algorithmic choices or workarounds
- Links to external specs or decision docs: e.g., "See docs/architecture.md section X"
- Boundary conditions or assumptions that aren't obvious from types

**JSDoc/TSDoc:**
- All public modules require module-level docstrings
- All public classes require class docstrings
- All public functions require docstrings
- Format: Google style with Args, Returns, Raises sections

**Example:**
```python
"""HTTP routes exposing the service alias over canonical workloads."""

def get_service_context(
    workload_id: str,
    request: Request,
    environment: str | None = None,
    services: QueryServices = Depends(get_query_services),
):
    """Return service-context data for a workload-id alias.

    Args:
        workload_id: Canonical workload identifier.
        request: Incoming FastAPI request used for validation errors.
        environment: Optional environment scope.
        services: Query service container.

    Returns:
        Service-context data or a problem response.
    """
```

## Function Design

**Size:** Maximum 500 lines per file (enforced via `scripts/check_python_file_lengths.py`)
- Keep functions under ~30 lines when possible
- Extract helper functions to separate modules if needed
- Long modules split by responsibility: `coordinator_facts.py`, `coordinator_finalize.py`, `coordinator_pipeline.py` (separate concerns)

**Parameters:**
- Use explicit parameters, not *args
- Type all parameters: `workload_id: str`, `environment: str | None`
- Dependency injection via FastAPI `Depends()`: `services: QueryServices = Depends(get_query_services)`
- Docstring Args section describes each parameter

**Return Values:**
- Always type return values: `-> WorkloadContextResponse`
- Union types with `|`: `str | None` instead of `Optional[str]`
- Return dict with error field for fallible operations: `{"error": "message"}` or `{"data": result}`
- Document return type in docstring Returns section

## Module Design

**Exports:**
- Public modules use `__all__` to control exports: `src/eshu/api/__init__.py`
- Example: `__all__ = ["API_V0_PREFIX", "QueryServices", "create_app", ...]`

**Barrel Files:**
- Package `__init__.py` re-exports key types and constructors
- `src/eshu/core/__init__.py` exports `get_database_manager()` factory
- `src/eshu/api/__init__.py` exports `create_app`, `QueryServices`, `get_query_services`

**Module Organization:**
- Keep related functionality in single files: `api/app.py` (FastAPI setup), `api/dependencies.py` (DI container)
- Separate concerns: `graph/persistence/` has: `session.py`, `mutations.py`, `calls.py`, `entities.py`, `files.py` (one concern each)
- Support modules pair with implementation: `parsers/languages/python.py` + `parsers/languages/python_support.py`

## Type Annotations

**Style:**
- Strict mode: use `from __future__ import annotations` in all modules
- Modern union syntax: `str | None` not `Optional[str]`, `int | str` not `Union[int, str]`
- Never use `Any` without explicit justification
- Generic types: `list[str]`, `dict[str, int]`, `Mapping[str, Any]`
- Pydantic models for data contracts: `from pydantic import BaseModel, ConfigDict`

**Example:**
```python
from pydantic import BaseModel, ConfigDict

class ContentFileEntry(BaseModel):
    """File content entry."""
    
    repo_id: str
    relative_path: str
    content: str
    language: str
    artifact_type: str | None = None
```

## Dataclass Usage

**Pattern:**
- Use `@dataclass(slots=True)` for performance with immutable state
- Example: `src/eshu/observability/structured_logging.py`
```python
@dataclass(slots=True)
class _LoggingState:
    """Runtime logging metadata shared by all emitted records."""
    component: str
    runtime_role: str
    service_name: str
    resource: dict[str, str]
    stream: IO[str] | None = None
```

---

*Convention analysis: 2026-04-13*
