#!/usr/bin/env python3
"""Scan live test files for readiness enrollment markers (#7859 P1).

The marker block (after the `package` clause of an enrolled live test):
    //eshu:readiness live-postgres-readiness
    //eshu:readiness:reason <verbatim ledger reason prose>
    //eshu:readiness:env <space-separated ESHU_* DSN/DISPOSABLE vars>
    //eshu:readiness:env-optional <vars the workflow sets but the runner
        does not require> (zero or one line; the completed-index proofs)
    //eshu:readiness:helper <repo-relative helper file> (consumer only)

The helper-only file carries a reason-only marker (it defines no Test):
    //eshu:readiness-helper live-postgres-readiness
    //eshu:readiness-helper:reason <verbatim ledger reason prose>
"""

import hashlib
import pathlib
import re

SUITE = "live-postgres-readiness"
ENROLL = "//eshu:readiness "
REASON = "//eshu:readiness:reason "
ENV = "//eshu:readiness:env "
ENV_OPTIONAL = "//eshu:readiness:env-optional "
HELPER_FIELD = "//eshu:readiness:helper "
KNOWN_FIELDS = (ENROLL, REASON, ENV, ENV_OPTIONAL, HELPER_FIELD)
HELPER_ENROLL = "//eshu:readiness-helper "
HELPER_REASON = "//eshu:readiness-helper:reason "
VAR_PATTERN = re.compile(r"ESHU_[A-Z0-9_]+\Z")
TEST_DECLARATION = re.compile(r"^func (Test\w+)\(", flags=re.MULTILINE)
BUILD_TAG = re.compile(r"^//go:build\s+(.+)$", flags=re.MULTILINE)
ENV_LITERAL = re.compile(r'"(ESHU_[A-Z0-9_]*(?:_DSN|_DISPOSABLE))"')

# ESHU_*_DSN/_DISPOSABLE literals an enrolled file may carry without
# declaring them: tuning knobs and reader/snapshot handles the suite never
# required, plus the constant Neo4j proof-backend handles. Any other
# undeclared literal fails closed so a new proof DSN cannot slip in.
DENYLIST = frozenset({
    "ESHU_ACTIVATION_COMPOSED_ITERATIONS",
    "ESHU_CONFIG_ARTIFACT_SEED_TIMING_ROWS",
    "ESHU_POSTGRES_MAX_IDLE_CONNS",
    "ESHU_POSTGRES_MAX_OPEN_CONNS",
    "ESHU_POSTGRES_READ_DSN",
    "ESHU_POSTGRES_READ_MAX_IDLE_CONNS",
    "ESHU_POSTGRES_READ_MAX_OPEN_CONNS",
    "ESHU_POSTGRES_READ_MEMBERS",
    "ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS",
    "ESHU_POSTGRES_TEST_DSN",
})
# Constant proof-backend handles: required by the runner's Neo4j loop and
# the workflow's graph extras, not by any one marker.
CONSTANT_MANAGED = frozenset({
    "ESHU_NEO4J_URI",
    "ESHU_NEO4J_USERNAME",
    "ESHU_NEO4J_PASSWORD",
})
# Runner-required acknowledgments with no enrolled reader: the runner
# fails unless they are set, but no enrolled test references them (the
# deadlock pair predates the scheduled move; the fairness disposable has
# no code reader at all). The generator force-includes them so the
# requirement stays visible and byte-identical; minting a new one is a
# conscious generator edit, never a silent marker line.
ACK_VARS = frozenset({
    "ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DISPOSABLE",
    "ESHU_REDUCER_FAIRNESS_PROOF_DISPOSABLE",
})


def fail(message: str) -> ValueError:
    """Build the generator's fail-closed error."""
    return ValueError(message)


def live_files(root: pathlib.Path) -> list[pathlib.Path]:
    """Return every live test file, in LC_ALL=C order."""
    found = sorted(root.joinpath("go").rglob("*_live_test.go"))
    for path in sorted(root.joinpath("go").rglob("*_test.go")):
        if path.name.endswith("_live_test.go"):
            continue
        try:
            source = path.read_text(encoding="utf-8")
        except OSError:
            continue
        build = BUILD_TAG.search(source)
        if not build:
            continue
        atoms = set(re.findall(r"[A-Za-z0-9_]+", build.group(1)))
        if (any(atom.startswith("live_") for atom in atoms)
                and TEST_DECLARATION.search(source)):
            found.append(path)
    return sorted(set(found))


def parse_marker(rel: str, source: str) -> dict | None:
    """Parse one file's enrollment marker; None when not enrolled."""
    lines = [line for line in source.splitlines()
             if line.startswith("//eshu:readiness")
             and not line.startswith("//eshu:readiness-helper")]
    if not lines:
        return None
    enroll = [line for line in lines if line.startswith(ENROLL)]
    if len(enroll) != 1 or enroll[0] != ENROLL + SUITE:
        raise fail(f"{rel}: malformed enrollment line: {enroll}")
    for line in lines:
        if not line.startswith(KNOWN_FIELDS):
            raise fail(f"{rel}: unknown marker field: {line}")
    take = lambda prefix: [line[len(prefix):] for line in lines
                           if line.startswith(prefix)]
    reasons, envs, optionals, helpers = (take(REASON), take(ENV),
                                        take(ENV_OPTIONAL), take(HELPER_FIELD))
    if len(reasons) != 1 or not reasons[0].strip():
        raise fail(f"{rel}: exactly one non-blank reason line is required")
    if len(envs) != 1 or not envs[0].split():
        raise fail(f"{rel}: exactly one non-empty env line is required")
    if len(optionals) > 1 or len(helpers) > 1:
        raise fail(f"{rel}: at most one env-optional and one helper line")
    declared = envs[0].split() + (optionals[0].split() if optionals else [])
    for var in declared:
        if not VAR_PATTERN.fullmatch(var):
            raise fail(f"{rel}: malformed env var: {var}")
    if len(set(declared)) != len(declared):
        raise fail(f"{rel}: duplicate env var")
    tests = TEST_DECLARATION.findall(source)
    if not tests:
        raise fail(f"{rel}: marked file defines no Test func")
    return {"reason": reasons[0], "env": sorted(envs[0].split()),
            "optional": sorted(optionals[0].split()) if optionals else [],
            "helper": helpers[0] if helpers else "", "tests": tests,
            "source": source}


def parse_helper_marker(rel: str, source: str) -> str | None:
    """Parse a helper-only file's reason marker; None when absent."""
    lines = [line for line in source.splitlines()
             if line.startswith("//eshu:readiness-helper")]
    if not lines:
        return None
    enroll = [line for line in lines if line.startswith(HELPER_ENROLL)]
    if len(enroll) != 1 or enroll[0] != HELPER_ENROLL + SUITE:
        raise fail(f"{rel}: malformed helper enrollment line: {enroll}")
    for line in lines:
        if not (line.startswith(HELPER_ENROLL)
                or line.startswith(HELPER_REASON)):
            raise fail(f"{rel}: unknown helper marker field: {line}")
    reasons = [line[len(HELPER_REASON):] for line in lines
               if line.startswith(HELPER_REASON)]
    if len(reasons) != 1 or not reasons[0].strip():
        raise fail(f"{rel}: exactly one non-blank helper reason is required")
    if TEST_DECLARATION.search(source):
        raise fail(f"{rel}: helper file defines a Test func")
    if BUILD_TAG.search(source):
        raise fail(f"{rel}: helper file carries a build tag")
    return reasons[0]


def collect(root: pathlib.Path) -> tuple[dict, dict]:
    """Scan every live file; return (enrolled, helper binding)."""
    enrolled: dict[str, dict] = {}
    helpers: dict[str, str] = {}
    for path in live_files(root):
        rel = path.relative_to(root).as_posix()
        try:
            source = path.read_text(encoding="utf-8")
        except OSError as error:
            raise fail(f"{rel}: unreadable: {error}") from error
        marker = parse_marker(rel, source)
        helper_reason = parse_helper_marker(rel, source)
        if marker and helper_reason is not None:
            raise fail(f"{rel}: enrollment and helper markers are exclusive")
        if marker:
            if rel in enrolled:
                raise fail(f"{rel}: duplicate enrollment")
            enrolled[rel] = marker
        elif helper_reason is not None:
            if rel in helpers:
                raise fail(f"{rel}: duplicate helper marker")
            helpers[rel] = helper_reason
    if len(helpers) != 1:
        raise fail(f"exactly one helper binding is required, "
                   f"found {len(helpers)}")
    helper_path = next(iter(helpers))
    consumers = [rel for rel, marker in enrolled.items()
                 if marker["helper"] == helper_path]
    if len(consumers) != 1:
        raise fail(f"helper {helper_path} needs exactly one consumer")
    consumer = consumers[0]
    stray = [rel for rel, marker in enrolled.items()
             if marker["helper"] and marker["helper"] != helper_path]
    if stray:
        raise fail(f"unknown helper reference in {stray}")
    package = root.joinpath(helper_path).parent
    if root.joinpath(consumer).parent != package:
        raise fail(f"helper and consumer packages differ: {helper_path}")
    helper_sha = hashlib.sha256(
        root.joinpath(helper_path).read_bytes()).hexdigest()
    consumer_sha = hashlib.sha256(
        root.joinpath(consumer).read_bytes()).hexdigest()
    binding = {"path": helper_path, "reason": helpers[helper_path],
               "helper_sha256": helper_sha, "consumer": consumer,
               "consumer_sha256": consumer_sha}
    return enrolled, binding


def code_literals(source: str) -> set[str]:
    """Return quoted env literals outside // line comments."""
    code = "\n".join(line.split("//", 1)[0] for line in source.splitlines())
    return set(ENV_LITERAL.findall(code))


def check_env_honesty(root: pathlib.Path, enrolled: dict) -> None:
    """Bind marker vars to real literals; flag undeclared proof literals.

    Only the enrolled file's own literals are checked: sibling test files
    in the same package legitimately serve other runners (scheduled,
    bench, integration) with their own DSNs. A proof DSN read through a
    shared helper is declared in the using test's marker and reviewed
    there; the drift mirror guards the union.
    """
    anywhere: set[str] = set()
    for path in sorted(root.joinpath("go").rglob("*.go")):
        try:
            source = path.read_text(encoding="utf-8")
        except OSError:
            continue
        anywhere.update(code_literals(source))
    for rel in sorted(enrolled):
        marker = enrolled[rel]
        for var in marker["env"] + marker["optional"]:
            if var not in anywhere and var not in ACK_VARS:
                raise fail(f"{rel}: marker var {var} matches no Go literal")
        declared = set(marker["env"]) | set(marker["optional"])
        for var in sorted(code_literals(marker["source"])):
            if var not in declared | DENYLIST | CONSTANT_MANAGED:
                raise fail(f"{rel}: undeclared proof env literal: {var}")


def required_vars(enrolled: dict) -> tuple[list[str], list[str]]:
    """Split marker vars into runner-required and workflow-only lists."""
    required: set[str] = set(ACK_VARS)
    optional: set[str] = set()
    for marker in enrolled.values():
        required.update(marker["env"])
        optional.update(marker["optional"])
    overlap = required & optional
    if overlap:
        raise fail(f"vars both required and optional: {sorted(overlap)}")
    for var in required | optional:
        if var.endswith("_DISPOSABLE"):
            twin = var.removesuffix("_DISPOSABLE") + "_DSN"
            if twin not in required | optional:
                raise fail(f"DISPOSABLE without DSN twin: {var}")
    return sorted(required), sorted(optional)


def slug(package: str) -> str:
    """Map a ./-relative package path to a module name slug."""
    return package.removeprefix("./").replace("/", "_").replace("-", "_")


def packages_of(enrolled: dict) -> dict[str, dict]:
    """Group enrolled files by Go package, packages and files sorted."""
    packages: dict[str, dict[str, list[str]]] = {}
    for rel in sorted(enrolled):
        package = "./" + pathlib.PurePosixPath(rel).parent.as_posix(
        ).removeprefix("go/")
        packages.setdefault(package, {})[rel] = enrolled[rel]["tests"]
    for package, files in packages.items():
        tests: list[str] = []
        for names in files.values():
            for name in names:
                if name in tests:
                    raise fail(f"{package}: duplicate test {name}")
                tests.append(name)
    slugs: dict[str, str] = {}
    for package in packages:
        name = slug(package)
        if name in slugs:
            raise fail(f"module slug collision: {package} vs {slugs[name]}")
        slugs[name] = package
    return packages
