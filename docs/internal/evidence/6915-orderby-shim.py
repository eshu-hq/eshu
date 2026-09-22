"""Bare-backend ORDER BY differential shim for eshu-hq/eshu#6915.

Seeds an identical small graph into two Bolt endpoints, runs the same
statements on both, and prints a row-by-row diff. No Eshu runtime involved.
Evidence and exact container commands: 6915-orderby-shim.md.

Usage: uv run --with neo4j python 6915-orderby-shim.py results.json
"""

import json
import logging
import os
import sys

from neo4j import GraphDatabase

logging.getLogger("neo4j").setLevel(logging.ERROR)

ENDPOINTS = {
    "nornicdb": os.environ.get("NORNICDB_BOLT", "bolt://127.0.0.1:19687"),
    "neo4j": os.environ.get("NEO4J_BOLT", "bolt://127.0.0.1:19787"),
}

# Seed in scrambled order so insertion order != any sort order.
# Functions: complexity ties (7, 7, 7, 4, 4, ...) force the secondary keys
# (e.name, then e.id) to decide order. Every (complexity, name, id) is distinct.
FUNCTIONS = [
    ("fn-09", "Worker", 7), ("fn-03", "Apply", 7), ("fn-11", "start", 4),
    ("fn-01", "Divide", 7), ("fn-07", "Error", 4), ("fn-12", "Map", 4),
    ("fn-05", "calculate_sum", 2), ("fn-02", "Min", 9), ("fn-10", "get_first", 2),
    ("fn-04", "Pop", 4), ("fn-08", "show", 1), ("fn-06", "Error", 7),
]
# DEPLOYS_FROM: several sources with multiple targets each, so s.id ties force
# the second key. Target key comes from t.id or (when id absent) t.uid.
DEPLOYS = [
    ("repo-c", "tgt-2"), ("repo-a", "tgt-9"), ("repo-b", "tgt-1"),
    ("repo-a", "tgt-3"), ("repo-c", "uid:tgt-0"), ("repo-b", "tgt-7"),
    ("repo-a", "uid:tgt-5"), ("repo-c", "tgt-8"), ("repo-b", "uid:tgt-4"),
    ("repo-a", "tgt-6"), ("repo-c", "tgt-1b"), ("repo-b", "tgt-0b"),
]

SEED = [
    "MATCH (n) DETACH DELETE n",
    # Repository + File so the OPTIONAL MATCH in the Function shape binds.
    "CREATE (:Repository {id: 'repo-x', name: 'x'})",
]

F_BASE = (
    "MATCH (e:Function) OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)"
    "<-[:REPO_CONTAINS]-(repo:Repository) "
    "WHERE coalesce(e.cyclomatic_complexity, 0) > 0 "
    "RETURN e.id AS id, e.name AS name, "
    "coalesce(e.cyclomatic_complexity, 0) AS complexity "
)
F_PLAIN = (
    "MATCH (e:Function) "
    "RETURN e.id AS id, e.name AS name, "
    "coalesce(e.cyclomatic_complexity, 0) AS complexity "
)
D_BASE = (
    "MATCH (s:Repository)-[r:DEPLOYS_FROM]->(t) "
    "RETURN coalesce(s.id, s.uid, s.name, s.path) AS source_id, "
    "coalesce(t.id, t.uid, t.name, t.path) AS target_id "
)

QUERIES = {
    # --- Function top-N shape (allowlist statement, trimmed projection) ---
    "F1_exact_limit": F_BASE + "ORDER BY complexity DESC, e.name, e.id LIMIT 5",
    "F1_exact_nolimit": F_BASE + "ORDER BY complexity DESC, e.name, e.id",
    "F2_aliases_limit": F_BASE + "ORDER BY complexity DESC, name, id LIMIT 5",
    "F2_aliases_nolimit": F_BASE + "ORDER BY complexity DESC, name, id",
    "F3_raw_limit": F_BASE
    + "ORDER BY coalesce(e.cyclomatic_complexity, 0) DESC, e.name, e.id LIMIT 5",
    "F3_raw_nolimit": F_BASE
    + "ORDER BY coalesce(e.cyclomatic_complexity, 0) DESC, e.name, e.id",
    "F4_nooptional_exact_limit": F_PLAIN
    + "ORDER BY complexity DESC, e.name, e.id LIMIT 5",
    "F4_nooptional_exact_nolimit": F_PLAIN + "ORDER BY complexity DESC, e.name, e.id",
    "F5_nooptional_aliases_limit": F_PLAIN + "ORDER BY complexity DESC, name, id LIMIT 5",
    # --- DEPLOYS_FROM shape ---
    "D1_exact_limit": D_BASE + "ORDER BY s.id, coalesce(t.id, t.uid) LIMIT 6",
    "D1_exact_nolimit": D_BASE + "ORDER BY s.id, coalesce(t.id, t.uid)",
    "D2_aliases_limit": D_BASE + "ORDER BY source_id, target_id LIMIT 6",
    "D2_aliases_nolimit": D_BASE + "ORDER BY source_id, target_id",
    "D3_rawid_nolimit": D_BASE + "ORDER BY s.id, t.id",
    # --- bisect: which clause shape drops non-projected sort keys ---
    "B1_node_only_rawkey": "MATCH (e:Function) RETURN e.id AS id ORDER BY e.name, e.id",
    "B2_rel_pattern_rawkey": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id ORDER BY e.name, e.id",
    "B3_optional_nowhere_rawkey": "MATCH (e:Function) OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File) RETURN e.id AS id ORDER BY e.name, e.id",
    "B4_node_where_rawkey": "MATCH (e:Function) WHERE e.cyclomatic_complexity > 0 RETURN e.id AS id ORDER BY e.name, e.id",
    "B5_rel_pattern_projected_same_expr": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id, e.name AS name ORDER BY e.name, e.id",
    "B6_rel_pattern_single_rawkey": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id ORDER BY e.name",
    "B7_rel_pattern_single_rawkey_desc": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id ORDER BY e.cyclomatic_complexity DESC, e.id",
    "B8_rel_pattern_return_node_prop_key": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e ORDER BY e.name, e.id",
    "B9_with_then_return": "MATCH (f:File)-[:CONTAINS]->(e:Function) WITH e ORDER BY e.name, e.id RETURN e.id AS id",
}


def seed(session):
    for stmt in SEED:
        session.run(stmt).consume()
    session.run(
        "MATCH (r:Repository {id: 'repo-x'}) CREATE (r)-[:REPO_CONTAINS]->"
        "(:File {relative_path: 'a.go'})"
    ).consume()
    for fid, name, cc in FUNCTIONS:
        session.run(
            "MATCH (f:File {relative_path: 'a.go'}) "
            "CREATE (f)-[:CONTAINS]->(:Function {id: $id, name: $name, "
            "cyclomatic_complexity: $cc})",
            id=fid, name=name, cc=cc,
        ).consume()
    for src in sorted({s for s, _ in DEPLOYS}, reverse=True):
        session.run("CREATE (:Repository {id: $id, name: $id})", id=src).consume()
    for src, tgt in DEPLOYS:
        if tgt.startswith("uid:"):
            session.run(
                "MATCH (s:Repository {id: $s}) "
                "CREATE (s)-[:DEPLOYS_FROM]->(:Target {uid: $u})",
                s=src, u=tgt[4:],
            ).consume()
        else:
            session.run(
                "MATCH (s:Repository {id: $s}) "
                "CREATE (s)-[:DEPLOYS_FROM]->(:Target {id: $t})",
                s=src, t=tgt,
            ).consume()


def run_all(uri):
    out = {}
    with GraphDatabase.driver(uri, auth=None) as drv:
        with drv.session() as s:
            seed(s)
            for name, q in QUERIES.items():
                try:
                    out[name] = [[v.get("id") if hasattr(v, "get") else v for v in r.values()] for r in s.run(q)]
                except Exception as exc:  # report, do not hide
                    out[name] = f"ERROR: {type(exc).__name__}: {exc}"
    return out


def main():
    results = {k: run_all(u) for k, u in ENDPOINTS.items()}
    json.dump({"queries": QUERIES, "results": results},
              open(sys.argv[1], "w"), indent=1)
    for name in QUERIES:
        a, b = results["nornicdb"][name], results["neo4j"][name]
        verdict = "MATCH" if a == b else "DIFF"
        print(f"== {name}: {verdict}")
        if verdict == "DIFF":
            if isinstance(a, str) or isinstance(b, str):
                print("  nornicdb:", a)
                print("  neo4j:   ", b)
                continue
            same_set = sorted(map(json.dumps, a)) == sorted(map(json.dumps, b))
            print(f"  same row multiset: {same_set}")
            for i in range(max(len(a), len(b))):
                ra = a[i] if i < len(a) else None
                rb = b[i] if i < len(b) else None
                mark = "  " if ra == rb else "!!"
                print(f"  {mark} {i:2d} nornicdb={ra} neo4j={rb}")


if __name__ == "__main__":
    main()
