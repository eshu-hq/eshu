
SELECT ids.id AS lookup_id, LEAST(COALESCE(cve.mass, 0) + COALESCE(adv.mass, 0) + COALESCE(m3.mass, 0), 10001) AS mass
FROM UNNEST($1::text[]) AS ids(id)
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > 100 THEN 10001 ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.cve_id <> ''
          AND support.cve_id = ids.id
        LIMIT 101
    ) AS limited
) AS cve ON TRUE
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > 100 THEN 10001 ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.advisory_id <> ''
          AND support.advisory_id = ids.id
        LIMIT 101
    ) AS limited
) AS adv ON TRUE
LEFT JOIN LATERAL (
    SELECT CASE WHEN COUNT(*) > 100 THEN 10001 ELSE COUNT(*) END AS mass
    FROM (
        SELECT 1
        FROM supply_chain_impact_finding_supports AS support
        WHERE support.payload #> '{provenance,advisory_group_members}' ? ids.id
        LIMIT 101
    ) AS limited
) AS m3 ON (COALESCE(cve.mass, 0) + COALESCE(adv.mass, 0)) < 10001
