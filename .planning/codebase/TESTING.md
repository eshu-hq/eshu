# Testing Patterns

**Analysis Date:** 2026-04-13

## Test Framework

**Runner:**
- pytest 7.4.0+ (specified in `pyproject.toml`)
- Config: `pyproject.toml` with `[tool.pytest.ini_options]`
- Parallelization: pytest-xdist 3.6.1+ for `-n` workers
- Async support: pytest-asyncio 0.21.0+

**Assertion Library:**
- Python standard library `assert` statements (no third-party assertion library)

**Run Commands:**
```bash
# All tests
PYTHONPATH=src uv run pytest tests/ -v

# Unit tests only
PYTHONPATH=src uv run pytest tests/unit/ -v

# Integration tests
PYTHONPATH=src uv run pytest tests/integration/ -v

# E2E tests with parallelization
PYTHONPATH=src uv run pytest tests/e2e/ -n 4 -v

# Watch mode (via test script)
./tests/run_tests.sh fast    # Unit + Integration
./tests/run_tests.sh unit    # Unit only
./tests/run_tests.sh all     # Everything

# Coverage
PYTHONPATH=src uv run pytest tests/ --cov=src/eshu --cov-report=html
```

## Test File Organization

**Location:**
- Unit tests: `tests/unit/` (mirrors `src/` structure where applicable)
- Integration tests: `tests/integration/` (higher-level flows)
- E2E tests: `tests/e2e/` (full system journeys)
- Fixtures: `tests/fixtures/` (shared test data, sample projects)

**Naming:**
- Test files: `test_<domain>.py`, e.g., `test_postgres.py`, `test_python_sql_mappings.py`
- Test modules co-locate with what they test: `tests/unit/content/test_postgres.py` for `src/eshu/content/postgres.py`
- Test classes group related tests: `class TestRepoContext:`, `class TestBundleRegistry:`

**Structure:**
```
tests/
├── conftest.py                    # Global pytest fixtures
├── fixtures/
│   ├── sample_projects/           # Real source code repos for testing
│   └── ecosystems/                # Fixture corpus (Ansible/Jenkins example)
├── unit/                          # Fast tests, mocked dependencies
│   ├── api/
│   ├── cli/
│   ├── content/
│   ├── core/
│   ├── parsers/
│   └── test_python_docstrings.py  # Convention tests
├── integration/                   # Medium speed, real subsystems
│   ├── cli/
│   ├── deployment/
│   ├── indexing/
│   └── mcp/
└── e2e/                          # Slow tests, full system
```

## Test Structure

**Suite Organization:**

```python
# From tests/unit/content/test_postgres.py
"""Unit tests for the PostgreSQL content provider."""

from __future__ import annotations

from contextlib import contextmanager
from datetime import datetime, timezone
import importlib
from unittest.mock import MagicMock

import pytest

from eshu.content.models import ContentEntityEntry, ContentFileEntry
from eshu.content.postgres import PostgresContentProvider


def test_delete_repository_content_removes_entities_and_files(monkeypatch) -> None:
    """Deleting repository content should purge entity and file rows for one repo."""
    
    provider = PostgresContentProvider("postgresql://example")
    cursor = MagicMock()

    @contextmanager
    def _cursor():
        yield cursor

    monkeypatch.setattr(provider, "_cursor", _cursor)
    
    provider.delete_repository_content("repository:r_test")
    
    # assertions...
```

**Patterns:**
- One assertion concept per test (or grouped related assertions)
- Test names describe behavior: `test_delete_repository_content_removes_entities_and_files`
- Docstrings explain the scenario: "Deleting repository content should purge entity and file rows for one repo."
- Setup in function body (no separate setUp method)
- Teardown via context managers or pytest fixtures with `yield`
- Use `monkeypatch` for env/attribute mocking (injected by pytest)

## Mocking

**Framework:** unittest.mock (standard library)
- `MagicMock` for replacing objects
- `@contextmanager` for fixture-like mocking
- `monkeypatch` pytest fixture for monkey-patching modules, env vars

**Patterns:**

```python
# Replace method with mock
cursor = MagicMock()
monkeypatch.setattr(provider, "_cursor", _context_manager_returning_cursor)

# Verify calls
queries = [call.args[0] for call in cursor.execute.call_args_list]
assert queries == [expected_query_1, expected_query_2]

# Mock environment
monkeypatch.delenv("ESHU_API_KEY", raising=False)
monkeypatch.setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317")
```

**What to Mock:**
- Database connections: Always mock Postgres/Neo4j for unit tests
- External APIs: Mock HTTP clients for API integration tests
- File I/O: Use temporary directories (via `temp_test_dir` fixture) for real files
- OTEL/observability: `observability.reset_observability_for_tests()` clears state

**What NOT to Mock:**
- Business logic functions: Test actual implementations
- Pydantic models: Use real instances for validation testing
- Tree-sitter parser: Use real parsers (lightweight, no I/O)
- Fixtures: Use real test data from `tests/fixtures/`

## Fixtures and Factories

**Test Data:**

```python
# From tests/conftest.py (global fixtures)
@pytest.fixture(scope="session")
def sample_projects_path():
    """Returns the path to the directory containing all sample projects."""
    if not SAMPLE_PROJECTS_DIR.exists():
        pytest.fail(f"Sample projects directory not found at {SAMPLE_PROJECTS_DIR}")
    return SAMPLE_PROJECTS_DIR

@pytest.fixture(scope="session")
def python_sample_project(sample_projects_path):
    """Returns path to the Python sample project."""
    path = sample_projects_path / "sample_project"
    if not path.exists():
        pytest.fail(f"Python sample project not found at {path}")
    return path

@pytest.fixture
def temp_test_dir():
    """Creates a temporary directory for file operations, cleaned up after test."""
    temp_dir = tempfile.mkdtemp(prefix="eshu_test_unit_")
    yield Path(temp_dir)
    shutil.rmtree(temp_dir, ignore_errors=True)

# Autouse fixture for isolation
@pytest.fixture(autouse=True)
def isolate_http_auth_env(monkeypatch: pytest.MonkeyPatch):
    """Keep local HTTP auth env from leaking into tests unintentionally."""
    monkeypatch.delenv("ESHU_API_KEY", raising=False)
    monkeypatch.delenv("ESHU_AUTO_GENERATE_API_KEY", raising=False)
```

**Fixture Scope:**
- `scope="session"`: Heavy setup (sample projects, repos) - reused across all tests
- `scope="module"`: Medium-weight - reused within test file
- Default (function scope): Fresh instance per test

**Location:**
- Global fixtures: `tests/conftest.py`
- Module-specific fixtures: Top of test file or `conftest.py` in subdirectory
- Sample projects: `tests/fixtures/sample_projects/` (real Git repos)
- Fixture corpus: `tests/fixtures/ecosystems/` (Ansible/Jenkins automation example)

## Coverage

**Requirements:** No enforced minimum (not specified in `pyproject.toml`)

**View Coverage:**
```bash
PYTHONPATH=src uv run pytest tests/ --cov=src/eshu --cov-report=html
# Open htmlcov/index.html
```

## Test Markers

**Available markers** (configured in `pyproject.toml`):
```ini
markers = [
    "integration: mark a test as an integration test.",
    "e2e: mark a test as an end-to-end test.",
    "slow: mark test as slow."
]
```

**Usage:**
```python
@pytest.mark.integration
def test_git_facts_end_to_end():
    """Integration test for facts-first indexing."""
    pass

@pytest.mark.slow
def test_large_repository_indexing():
    """E2E test for performance."""
    pass
```

## Test Types

**Unit Tests:**
- Location: `tests/unit/`
- Scope: Single function or class, all dependencies mocked
- Speed: ~100ms per test
- Example: `tests/unit/content/test_postgres.py` - Tests `PostgresContentProvider` methods with mocked cursors
- Approach: Isolate behavior, verify business logic without side effects

**Integration Tests:**
- Location: `tests/integration/`
- Scope: Multiple modules working together, some real subsystems
- Speed: ~500ms - 5s per test
- Example: `tests/integration/indexing/test_git_facts_end_to_end.py` - Tests facts-first flow with in-memory stores
- Approach: Verify components collaborate correctly

**E2E Tests:**
- Location: `tests/e2e/`
- Scope: Full system end-to-end, real database backends (Neo4j, Postgres)
- Speed: ~10s - 60s per test (run with parallelization: `-n 4`)
- Requires: Docker Compose stack or real database connections
- Approach: User journey validation, full system behavior

## Common Patterns

**Async Testing:**
```python
import pytest

@pytest.mark.asyncio
async def test_async_repository_sync():
    """Async operations use pytest-asyncio."""
    result = await sync_repository_async()
    assert result.status == "complete"
```

**Error Testing:**
```python
def test_parse_sqlalchemy_tablename_mapping(python_parser, temp_test_dir) -> None:
    """Test successful parsing of SQLAlchemy ORM mappings."""
    
    source_file = temp_test_dir / "models.py"
    source_file.write_text("""
from sqlalchemy.orm import DeclarativeBase

class User(Base):
    __tablename__ = "users"
""".strip() + "\n", encoding="utf-8")
    
    result = python_parser.parse(source_file)
    
    assert result["orm_table_mappings"] == [{
        "class_name": "User",
        "table_name": "users",
        "framework": "sqlalchemy",
        "line_number": 9,
    }]
```

**Parametrized Tests:**
```python
@pytest.mark.parametrize("module_name", [
    "eshu.api",
    "eshu.cli",
    "eshu.collectors",
])
def test_phase1_package_skeleton_imports(module_name: str) -> None:
    """Verify phase1 packages can be imported."""
    import importlib
    module = importlib.import_module(module_name)
    assert module is not None
```

**In-Memory Doubles:**
```python
# From tests/integration/indexing/test_git_facts_end_to_end.py
class _InMemoryFactStore:
    """Minimal in-memory fact store for cutover integration tests."""
    
    def __init__(self) -> None:
        self.runs: list[FactRunRow] = []
        self.records: list[FactRecordRow] = []
        self.enabled = True
    
    def upsert_fact_run(self, entry: FactRunRow) -> None:
        self.runs = [row for row in self.runs if row.source_run_id != entry.source_run_id]
        self.runs.append(entry)
    
    def upsert_facts(self, entries: list[FactRecordRow]) -> None:
        by_id = {record.fact_id: record for record in self.records}
        for entry in entries:
            by_id[entry.fact_id] = entry
        self.records = list(by_id.values())
```

## Repository Guardrails

Pre-test checks run automatically (enforced in `tests/run_tests.sh`):

```bash
# 1. File length limits
python3 scripts/check_python_file_lengths.py --max-lines 500
# Ensures no file exceeds 500 lines

# 2. Docstring requirements
python3 scripts/check_python_docstrings.py
# Enforces docstrings on all public modules, classes, functions
# Exemptions managed via scripts/python_docstring_exemptions.txt

# 3. Code formatting
uv run black --check src tests
# Ensures Black formatting compliance
```

## Test Execution Strategy

**Full Suite:**
```bash
./tests/run_tests.sh all       # All tests: unit, integration, e2e
./tests/run_tests.sh fast      # Fast tests: unit + integration (no e2e)
./tests/run_tests.sh unit      # Unit tests only
./tests/run_tests.sh integration # Integration tests
./tests/run_tests.sh e2e       # E2E tests (requires stack or database)
```

**CI Pipeline (Docker Compose):**
- E2E tests run inside compose network for reliable Neo4j/Postgres access
- Seeding: `scripts/seed_e2e_graph.py` initializes graph fixtures
- Workers: Parallelized with `-n 4` (configurable via `ESHU_E2E_PYTEST_WORKERS`)

---

*Testing analysis: 2026-04-13*
