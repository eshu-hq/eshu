"""Minimal ORDER BY repro filed upstream for eshu-hq/eshu#6915.

One CREATE seed, nine queries plus the RETURN e ORDER BY e.id check, run on
both Bolt endpoints. See 6915-orderby-shim.md.

Usage: uv run --with neo4j python 6915-orderby-minimal.py results.json
"""
import json, logging, os, sys
from neo4j import GraphDatabase, __version__
logging.getLogger("neo4j").setLevel(logging.ERROR)
SEED = """CREATE (r:Repository {id:'repo-x', name:'x'})-[:REPO_CONTAINS]->(f:File {relative_path:'a.go'}),
       (f)-[:CONTAINS]->(:Function {id:'fn-09', name:'Worker',        cyclomatic_complexity:7}),
       (f)-[:CONTAINS]->(:Function {id:'fn-03', name:'Apply',         cyclomatic_complexity:7}),
       (f)-[:CONTAINS]->(:Function {id:'fn-11', name:'start',         cyclomatic_complexity:4}),
       (f)-[:CONTAINS]->(:Function {id:'fn-01', name:'Divide',        cyclomatic_complexity:7}),
       (f)-[:CONTAINS]->(:Function {id:'fn-07', name:'Error',         cyclomatic_complexity:4}),
       (f)-[:CONTAINS]->(:Function {id:'fn-12', name:'Map',           cyclomatic_complexity:4}),
       (f)-[:CONTAINS]->(:Function {id:'fn-05', name:'calculate_sum', cyclomatic_complexity:2}),
       (f)-[:CONTAINS]->(:Function {id:'fn-02', name:'Min',           cyclomatic_complexity:9}),
       (f)-[:CONTAINS]->(:Function {id:'fn-10', name:'get_first',     cyclomatic_complexity:2}),
       (f)-[:CONTAINS]->(:Function {id:'fn-04', name:'Pop',           cyclomatic_complexity:4}),
       (f)-[:CONTAINS]->(:Function {id:'fn-08', name:'show',          cyclomatic_complexity:1}),
       (f)-[:CONTAINS]->(:Function {id:'fn-06', name:'Error',         cyclomatic_complexity:7})"""
Q6 = ("MATCH (e:Function) OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository) "
      "WHERE coalesce(e.cyclomatic_complexity, 0) > 0 "
      "RETURN e.id AS id, e.name AS name, coalesce(e.cyclomatic_complexity, 0) AS complexity ")
Q = {
 "1": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id ORDER BY e.name",
 "2": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id ORDER BY e.name, e.id",
 "3": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e.id AS id, e.name AS name ORDER BY e.name, e.id",
 "4": "MATCH (e:Function) RETURN e.id AS id ORDER BY e.name, e.id",
 "5": "MATCH (e:Function) OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File) RETURN e.id AS id ORDER BY e.name, e.id",
 "6": Q6 + "ORDER BY complexity DESC, e.name, e.id LIMIT 5",
 "7": Q6 + "ORDER BY complexity DESC, name, id LIMIT 5",
 "8": Q6 + "ORDER BY coalesce(e.cyclomatic_complexity, 0) DESC, e.name, e.id LIMIT 5",
 "9": "MATCH (e:Function) RETURN e.id AS id ORDER BY coalesce(e.cyclomatic_complexity, 0) DESC, e.id",
 "10": "MATCH (f:File)-[:CONTAINS]->(e:Function) RETURN e ORDER BY e.id",
}
def norm(v):
    return v.get("id") if hasattr(v, "get") else v
out = {"driver": __version__, "seed": SEED, "queries": Q, "results": {}}
ENDPOINTS = [("nornicdb", os.environ.get("NORNICDB_BOLT", "bolt://127.0.0.1:19687")),
             ("neo4j", os.environ.get("NEO4J_BOLT", "bolt://127.0.0.1:19787"))]
for be, uri in ENDPOINTS:
    with GraphDatabase.driver(uri, auth=None) as d, d.session() as s:
        s.run("MATCH (n) DETACH DELETE n").consume(); s.run(SEED).consume()
        out["results"][be] = {k: [[norm(v) for v in r.values()] for r in s.run(q)] for k, q in Q.items()}
json.dump(out, open(sys.argv[1], "w"), indent=1)
for k in Q:
    a, b = out["results"]["nornicdb"][k], out["results"]["neo4j"][k]
    f = lambda rows: ", ".join(" ".join(map(str, r)) for r in rows)
    print(f"{k}: {'MATCH' if a==b else 'DIFF'}\n  nornicdb: {f(a)}\n  neo4j:    {f(b)}")
