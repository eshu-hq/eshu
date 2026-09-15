# Good And Bad Cypher Patterns

Worked shapes for the [Query Checklist and Write Checklist](../SKILL.md#query-checklist).
For the full anti-pattern list and backend planner detail, see
[Cypher Performance: Anti-Patterns](../../../../docs/public/reference/cypher-performance.md#anti-patterns).

## Reads

Bad: unlabelled scan plus late filter.

```cypher
MATCH (n)
WHERE n.id = $id
RETURN n
```

Good: indexed label-property anchor.

```cypher
MATCH (s:Service {id: $id})
RETURN s
```

Bad: broad expansion before limiting.

```cypher
MATCH (r:Repository)-[:CONTAINS*]->(n)
RETURN n
LIMIT 25
```

Good: anchor, bound, filter, then limit.

```cypher
MATCH (r:Repository {id: $repo_id})-[:CONTAINS*1..3]->(n:File)
WHERE n.language = $language
RETURN n.path
ORDER BY n.path
LIMIT 25
```

## Writes

Bad: wide mutable `MERGE` identity.

```cypher
UNWIND $rows AS row
MERGE (s:Service {id: row.id, name: row.name, owner: row.owner})
```

Good: stable identity plus mutable updates.

```cypher
UNWIND $rows AS row
MERGE (s:Service {id: row.id})
SET s.name = row.name,
    s.owner = row.owner,
    s.updated_at = row.updated_at
```

Bad: independent matches that can create a cartesian write multiplier.

```cypher
MATCH (s:Service {id: $service_id})
MATCH (e:Environment)
MERGE (s)-[:RUNS_IN]->(e)
```

Good: constrain both sides.

```cypher
MATCH (s:Service {id: $service_id})
MATCH (e:Environment {name: $environment})
MERGE (s)-[:RUNS_IN]->(e)
```
