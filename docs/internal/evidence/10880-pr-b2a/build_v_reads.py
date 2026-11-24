#!/usr/bin/env python3
"""Writes the route V and image read statements of the PR-B2a theory proof.

Usage: python3 build_v_reads.py <out-dir>

Each file is one self-contained statement over the real bootstrapped schema, with
the selectors written as literals so a partial index can be proved to apply
(the shipped statements bind them as parameters and the supplement's profile is
fixed to precise, so the partial index predicate is implied by the statement text).
The precise predicate below is the one in the shipped read, spelled once and used
for the winner row, the support row and the two partial indexes.
"""

import os
import sys

REASONS = [
    "npm_semver_affected_range", "npm_semver_known_fixed",
    "nuget_semver_affected_range", "nuget_semver_known_fixed",
    "cargo_semver_affected_range", "cargo_semver_known_fixed",
    "hex_semver_affected_range", "hex_semver_known_fixed",
    "maven_range_match", "maven_known_fixed",
    "swift_semver_affected_range", "swift_semver_known_fixed",
]
HIDDEN = "'not_affected','accepted_risk','false_positive','ignored','advisory_retracted'"
OPERATOR = "operator:vulnerability_suppressions"


def precise(alias):
    """The shipped precise filter over one table alias (the index predicate when alias is empty).

    Two arms, as in the shipped read: a finding the writer stamped precise, and a finding
    from before profile tagging (empty profile) that passes the status, version and
    match-reason test."""
    p = (alias + ".") if alias else ""
    reasons = ", ".join("'%s'" % r for r in REASONS)
    return (
        "(%sdetection_profile = 'precise' OR (%sdetection_profile = '' AND %simpact_status IN "
        "('affected_exact', 'not_affected_known_fixed') AND %sobserved_version <> '' AND %smatch_reason IN (%s)))"
        % (p, p, p, p, p, reasons)
    )


def selectors(alias, status="", cve="", pkg=""):
    out = []
    if status:
        out.append("%s.impact_status = '%s'" % (alias, status))
    if cve:
        out.append("%s.cve_id = '%s'" % (alias, cve))
    if pkg:
        out.append("%s.package_id = '%s'" % (alias, pkg))
    return "".join("\n    AND " + c for c in out)


STATIC_W = """
    AND NOT EXISTS (SELECT 1 FROM scanner_image_result_fact_associations AS finding_association
                    WHERE finding_association.finding_fact_id = w.winner_fact_id)
    AND w.winner_scope_id NOT LIKE 'image://derived/%'"""

STATIC_S = """
    AND association.finding_fact_id IS NULL
    AND s.scope_id NOT LIKE 'image://derived/%'
    AND (class.finding_fact_id IS NULL OR (class.source_class = 'static' AND class.lifecycle = 'active'))"""

OVERRIDE = """LEFT JOIN LATERAL (
    SELECT o.suppression_state FROM supply_chain_impact_finding_supports AS o
    WHERE o.scope_id = '%s' AND o.canonical_key = s.canonical_key AND o.suppression_state <> 'active'
      AND o.generation_id = (SELECT g.generation_id FROM ingestion_scopes AS sc
                             JOIN scope_generations AS g ON g.scope_id = sc.scope_id AND g.generation_id = sc.active_generation_id
                             WHERE sc.scope_id = '%s' AND g.status = 'active')
    ORDER BY o.priority_score DESC, o.has_payload_finding_id DESC, o.fact_id ASC LIMIT 1
  ) AS override ON TRUE""" % (OPERATOR, OPERATOR)


def v_findings(status="", cve="", pkg="", prune=False, branch_b=True):
    """The V finding set: columns canonical_key, finding_id, priority_score, impact_status,
    priority_bucket, severity_bucket, repository_id, ecosystem, pick_fact_id (NULL for branch A)."""
    sel_w = selectors("w", status, cve, pkg)
    sel_c = selectors("w", status, cve, pkg)
    prune_sql = "\n    AND w.impact_status IN ('affected_exact', 'not_affected_known_fixed') AND w.observed_version <> ''" if prune else ""
    b = ""
    if branch_b:
        b = """,
b_candidates AS (
  SELECT w.canonical_key FROM supply_chain_impact_canonical_winners AS w
  WHERE TRUE%s%s
    AND NOT (%s)%s
),
branch_b AS (
  SELECT s.canonical_key, s.finding_id, s.priority_score, s.impact_status, s.priority_bucket,
         s.severity_bucket, s.repository_id, s.ecosystem, s.fact_id AS pick_fact_id,
         COALESCE(override.suppression_state, s.suppression_state) AS suppression_state
  FROM b_candidates AS c
  CROSS JOIN LATERAL (
    SELECT s.* FROM supply_chain_impact_finding_supports AS s
    WHERE s.canonical_key = c.canonical_key AND %s AND s.scope_id <> '%s'
      -- eligibility as scalar probes, so the planner keeps the ordered index scan and stops
      -- at the first eligible support instead of joining every precise support of the key
      AND COALESCE((SELECT TRUE FROM ingestion_scopes AS sc
                    JOIN scope_generations AS gen ON gen.scope_id = sc.scope_id AND gen.generation_id = sc.active_generation_id
                    WHERE sc.scope_id = s.scope_id AND sc.active_generation_id = s.generation_id AND gen.status = 'active' LIMIT 1), FALSE)
      AND NOT EXISTS (SELECT 1 FROM scanner_image_result_fact_associations AS association WHERE association.finding_fact_id = s.fact_id)
      AND s.scope_id NOT LIKE 'image://derived/%%'
      AND COALESCE((SELECT class.source_class = 'static' AND class.lifecycle = 'active' FROM scanner_image_result_fact_classifications AS class
                    WHERE class.finding_fact_id = s.fact_id LIMIT 1), TRUE)
    ORDER BY s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC
    LIMIT 1
  ) AS s
  %s
)""" % (sel_c, STATIC_W, precise("w"), prune_sql,
        precise("s"), OPERATOR, OVERRIDE)
    # note: b_candidates reuses sel_c on w; STATIC_W is a winner-row predicate
    a = """branch_a AS (
  SELECT w.canonical_key, w.finding_id, w.priority_score, w.impact_status, w.priority_bucket,
         w.severity_bucket, w.repository_id, w.ecosystem, NULL::text AS pick_fact_id,
         w.suppression_state
  FROM supply_chain_impact_canonical_winners AS w
  WHERE TRUE%s%s
    AND %s
)""" % (sel_w, STATIC_W, precise("w"))
    union = "SELECT * FROM branch_a" + (" UNION ALL SELECT canonical_key, finding_id, priority_score, impact_status, priority_bucket, severity_bucket, repository_id, ecosystem, pick_fact_id, suppression_state FROM branch_b" if branch_b else "")
    return "WITH " + a + b + ",\nv_findings AS (\n  " + union + "\n),\nvisible AS (\n  SELECT * FROM v_findings WHERE suppression_state NOT IN (" + HIDDEN + ")\n)"


def write(out, name, sql):
    with open(os.path.join(out, name + ".sql"), "w") as f:
        f.write(sql.strip() + ";\n")


def main():
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    cases = {
        "status": dict(status="affected_exact"),
        "axios": dict(pkg="pkg:npm/axios"),
        "cve": dict(cve="CVE-2026-17"),
        "none": dict(),
    }
    for variant, prune in (("unpruned", False), ("pruned", True)):
        for cname, kw in cases.items():
            base = v_findings(prune=prune, **kw)
            # list: finding_id sort, limit 50, payload from winners (A) or the pick (B)
            write(out, "v_list_%s_%s" % (cname, variant), base + """,
page AS (SELECT * FROM visible ORDER BY finding_id ASC LIMIT 50)
SELECT page.finding_id, COALESCE(win.payload, sup.payload) AS payload
FROM page
LEFT JOIN supply_chain_impact_canonical_winners AS win ON page.pick_fact_id IS NULL AND win.canonical_key = page.canonical_key
LEFT JOIN supply_chain_impact_finding_supports AS sup ON sup.fact_id = page.pick_fact_id
ORDER BY page.finding_id ASC""")
            # list sorted by priority desc
            write(out, "v_list_prio_%s_%s" % (cname, variant), base + """,
page AS (SELECT * FROM visible ORDER BY priority_score DESC, finding_id ASC LIMIT 50)
SELECT page.finding_id, COALESCE(win.payload, sup.payload) AS payload
FROM page
LEFT JOIN supply_chain_impact_canonical_winners AS win ON page.pick_fact_id IS NULL AND win.canonical_key = page.canonical_key
LEFT JOIN supply_chain_impact_finding_supports AS sup ON sup.fact_id = page.pick_fact_id
ORDER BY page.priority_score DESC, page.finding_id ASC""")
            # count: totals and both bucket maps in one statement
            write(out, "v_count_%s_%s" % (cname, variant), base + """
SELECT
  (SELECT count(*) FROM visible) AS total,
  (SELECT count(*) FROM visible WHERE impact_status IN ('affected_exact','affected_derived','possibly_affected')) AS affected,
  (SELECT count(*) FROM visible WHERE impact_status = 'affected_exact') AS affected_exact,
  (SELECT jsonb_agg(jsonb_build_object('bucket', b, 'n', n)) FROM (SELECT COALESCE(NULLIF(priority_bucket,''),'unknown') AS b, count(*) AS n FROM visible GROUP BY 1) x) AS priority_buckets,
  (SELECT jsonb_agg(jsonb_build_object('bucket', b, 'n', n)) FROM (SELECT COALESCE(NULLIF(severity_bucket,''),'none') AS b, count(*) AS n FROM visible GROUP BY 1) x) AS severity_buckets""")
        # five inventory dimensions, no selector
        base = v_findings(prune=prune)
        for dim, expr in (
            ("impact_status", "COALESCE(NULLIF(impact_status, ''), 'unknown')"),
            ("priority_bucket", "COALESCE(NULLIF(priority_bucket, ''), 'unknown')"),
            ("severity", "COALESCE(NULLIF(severity_bucket, ''), 'none')"),
            ("repository_id", "COALESCE(NULLIF(repository_id, ''), 'unknown')"),
            ("ecosystem", "COALESCE(NULLIF(ecosystem, ''), 'unknown')"),
        ):
            write(out, "v_inventory_%s_%s" % (dim, variant), base + """
SELECT %s AS bucket, count(*) AS bucket_count FROM visible GROUP BY 1 ORDER BY bucket_count DESC, bucket LIMIT 50""" % expr)
        # grouped inventory: fan out to a repository -> service map
        write(out, "v_grouped_service_%s" % variant, base + """,
fanned AS (
  SELECT COALESCE(m.dim_id, '') AS bucket, COALESCE(NULLIF(f.impact_status,''),'unknown') AS impact_status,
         COALESCE(NULLIF(f.severity_bucket,''),'none') AS severity_bucket, count(*) AS cell_count, max(f.priority_score) AS max_priority
  FROM visible AS f LEFT JOIN b2a_service_map AS m ON m.repository_id = f.repository_id
  GROUP BY 1, 2, 3
),
page AS (
  SELECT bucket, sum(cell_count)::bigint AS bucket_count, max(max_priority) AS max_priority FROM fanned GROUP BY bucket
  ORDER BY max(max_priority) DESC, sum(cell_count) DESC, bucket LIMIT 50
)
SELECT page.bucket, page.bucket_count, page.max_priority, (SELECT count(*) FROM visible) AS distinct_findings FROM page""")

    # winners-only references (what the shipped winners route costs without the supplement)
    for cname, kw in {"status": dict(status="affected_exact"), "none": dict()}.items():
        base = v_findings(branch_b=False, **kw)
        write(out, "ref_winners_count_%s" % cname, base + "\nSELECT count(*) FROM visible")

    # image_ref reads: supports only, through the partial image_ref index
    for img_name, img in (("typical", "registry.example/img17:1"), ("hot", "registry.example/hot:1")):
        picked = """WITH image_supports AS MATERIALIZED (
  -- the partial image_ref index leads; the joins below see only these rows
  SELECT * FROM supply_chain_impact_finding_supports WHERE image_ref = '%s' AND image_ref <> ''
),
candidates AS (
  SELECT s.* FROM image_supports AS s
  JOIN ingestion_scopes AS sc ON sc.scope_id = s.scope_id AND sc.active_generation_id = s.generation_id
  JOIN scope_generations AS gen ON gen.scope_id = s.scope_id AND gen.generation_id = s.generation_id
  LEFT JOIN scanner_image_result_fact_classifications AS class ON class.finding_fact_id = s.fact_id
  LEFT JOIN scanner_image_result_fact_associations AS association ON association.finding_fact_id = s.fact_id
  WHERE gen.status = 'active' AND s.scope_id <> '%s'%s
),
winners_of AS (
  SELECT DISTINCT ON (s.canonical_key) s.*,
         COALESCE(override.suppression_state, s.suppression_state) AS effective_state
  FROM candidates AS s
  %s
  ORDER BY s.canonical_key, s.priority_score DESC, s.has_payload_finding_id DESC, s.fact_id ASC
),
visible AS (SELECT * FROM winners_of WHERE effective_state NOT IN (%s))""" % (img, OPERATOR, STATIC_S, OVERRIDE, HIDDEN)
        write(out, "image_list_%s" % img_name, picked + """,
page AS (SELECT * FROM visible ORDER BY finding_id ASC LIMIT 50)
SELECT finding_id, payload FROM page ORDER BY finding_id ASC""")
        write(out, "image_count_%s" % img_name, picked + """
SELECT count(*) AS total, count(*) FILTER (WHERE impact_status = 'affected_exact') AS affected_exact FROM visible""")
        write(out, "image_inventory_%s" % img_name, picked + """
SELECT COALESCE(NULLIF(impact_status,''),'unknown') AS bucket, count(*) FROM visible GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT 50""")

    with open(os.path.join(out, "_precise_predicate.txt"), "w") as f:
        f.write(precise("") + "\n")


if __name__ == "__main__":
    main()
