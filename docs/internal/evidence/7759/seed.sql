-- 7759 shim seed: ~585k rows, 7 repos, skewed like the evidence notes.
-- repository:big mirrors repo-B (240k rows, ~44k javascript Function).
-- Rare trigram term 'zxqvlogin' (~40 rows/repo); common term 'handler' (30%).
INSERT INTO content_entities (
    entity_id, repo_id, relative_path, entity_type, entity_name,
    start_line, end_line, language, source_cache, metadata, indexed_at
)
SELECT
    r.repo || ':e' || g AS entity_id,
    r.repo AS repo_id,
    'src/p' || (g % 400) || '/f' || (g % 4000) ||
        CASE m.lang
            WHEN 'javascript' THEN '.js' WHEN 'typescript' THEN '.ts'
            WHEN 'php' THEN '.php' WHEN 'go' THEN '.go'
            WHEN 'python' THEN '.py' WHEN 'hcl' THEN '.tf'
            WHEN 'terraform' THEN '.tf' WHEN 'yaml' THEN '.yaml'
            WHEN 'jsx' THEN '.jsx' WHEN 'kotlin' THEN '.kt'
            ELSE '.txt' END AS relative_path,
    m.etype AS entity_type,
    CASE WHEN g % 6000 = 0 THEN 'zxqvlogin_probe_' || g
         WHEN g % 10 < 3 THEN 'handler_' || (g % 50000)
         ELSE 'entity_' || r.short || '_' || g END AS entity_name,
    1 + (g % 1500) AS start_line,
    1 + (g % 1500) + 10 + (g % 90) AS end_line,
    m.lang AS language,
    'function body(' || (g % 997) || ') { return handler_ok; } /* pad ' ||
        CASE WHEN g % 6000 = 1 THEN 'zxqvlogin ' ELSE '' END ||
        'lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod */' AS source_cache,
    CASE WHEN m.etype = 'K8sResource' AND g % 2 = 0
              THEN '{"kind":"Service","namespace":"default","selector":"app=web","pod_template_labels":"app=web"}'
         WHEN m.etype = 'K8sResource'
              THEN '{"kind":"Deployment","namespace":"prod","selector":"app=api","pod_template_labels":"app=api"}'
         WHEN m.lang = 'jsx' THEN '{"jsx_component_usage":["TopNav","SideBar"]}'
         ELSE '{}' END::jsonb AS metadata,
    TIMESTAMPTZ '2026-10-01 00:00:00+00' AS indexed_at
FROM (VALUES
    ('repository:big', 'big', 240000),
    ('repository:k8smono', 'k8s', 60000),
    ('repository:phpheavy', 'php', 120000),
    ('repository:meda', 'meda', 80000),
    ('repository:medb', 'medb', 60000),
    ('repository:small', 'sml', 20000),
    ('repository:tiny', 'tny', 5000)
) AS r(repo, short, n)
CROSS JOIN LATERAL generate_series(1, r.n) AS g
CROSS JOIN LATERAL (
    SELECT * FROM (VALUES
        (CASE WHEN r.repo = 'repository:big' AND g % 100 < 19 THEN 'javascript'
              WHEN r.repo = 'repository:big' AND g % 100 < 31 THEN 'javascript'
              WHEN r.repo = 'repository:big' AND g % 100 < 38 THEN 'typescript'
              WHEN r.repo = 'repository:big' AND g % 100 < 44 THEN 'php'
              WHEN r.repo = 'repository:big' AND g % 100 < 50 THEN 'go'
              WHEN r.repo = 'repository:big' AND g % 100 < 54 THEN 'jsx'
              WHEN r.repo = 'repository:big' AND g % 100 < 56 THEN 'kotlin'
              WHEN r.repo = 'repository:big' AND g % 100 < 58 THEN 'hcl'
              WHEN r.repo = 'repository:big' AND g % 100 < 60 THEN 'terraform'
              WHEN r.repo = 'repository:big' AND g % 100 < 62 THEN 'yaml'
              WHEN r.repo = 'repository:big' THEN 'python'
              WHEN r.repo = 'repository:k8smono' AND g % 10 = 0 THEN 'yaml'
              WHEN r.repo = 'repository:k8smono' AND g % 10 < 3 THEN 'yaml'
              WHEN r.repo = 'repository:k8smono' THEN 'yaml'
              WHEN r.repo = 'repository:phpheavy' AND g % 100 < 75 THEN 'php'
              WHEN r.repo = 'repository:phpheavy' AND g % 100 < 85 THEN 'javascript'
              WHEN r.repo = 'repository:phpheavy' THEN 'python'
              WHEN r.repo = 'repository:meda' AND g % 100 < 20 THEN 'javascript'
              WHEN r.repo = 'repository:meda' AND g % 100 < 24 THEN 'kotlin'
              WHEN r.repo = 'repository:meda' AND g % 100 < 27 THEN 'hcl'
              WHEN r.repo = 'repository:meda' AND g % 100 < 30 THEN 'terraform'
              WHEN r.repo = 'repository:meda' AND g % 100 < 60 THEN 'python'
              WHEN r.repo = 'repository:meda' THEN 'go'
              WHEN r.repo = 'repository:medb' AND g % 100 < 25 THEN 'javascript'
              WHEN r.repo = 'repository:medb' AND g % 100 < 40 THEN 'typescript'
              WHEN r.repo = 'repository:medb' AND g % 100 < 65 THEN 'python'
              WHEN r.repo = 'repository:medb' AND g % 100 < 85 THEN 'go'
              WHEN r.repo = 'repository:medb' THEN 'php'
              WHEN r.repo IN ('repository:small', 'repository:tiny') AND g % 100 < 30 THEN 'javascript'
              WHEN r.repo IN ('repository:small', 'repository:tiny') AND g % 100 < 60 THEN 'python'
              ELSE 'go' END,
        CASE WHEN r.repo = 'repository:big' AND g % 100 < 19 THEN 'Function'
              WHEN r.repo = 'repository:big' AND g % 100 < 31 THEN 'Class'
              WHEN r.repo = 'repository:big' AND g % 100 < 38 THEN 'Function'
              WHEN r.repo = 'repository:big' AND g % 100 < 44 THEN 'Function'
              WHEN r.repo = 'repository:big' AND g % 100 < 50 THEN 'Function'
              WHEN r.repo = 'repository:big' AND g % 100 < 54 THEN 'Function'
              WHEN r.repo = 'repository:big' AND g % 100 < 56 THEN 'Function'
              WHEN r.repo = 'repository:big' AND g % 100 < 58 THEN 'TerraformVariable'
              WHEN r.repo = 'repository:big' AND g % 100 < 60 THEN 'TerraformResource'
              WHEN r.repo = 'repository:big' AND g % 100 < 62 THEN 'K8sResource'
              WHEN r.repo = 'repository:big' THEN 'Function'
              WHEN r.repo = 'repository:k8smono' AND g % 10 = 0 THEN 'K8sResource'
              WHEN r.repo = 'repository:k8smono' AND g % 10 < 3 THEN 'ArgoCDApplication'
              WHEN r.repo = 'repository:k8smono' THEN 'HelmChart'
              WHEN r.repo = 'repository:phpheavy' AND g % 100 < 60 THEN 'Function'
              WHEN r.repo = 'repository:phpheavy' AND g % 100 < 75 THEN 'Class'
              WHEN r.repo = 'repository:phpheavy' THEN 'Function'
              WHEN r.repo = 'repository:meda' AND g % 100 < 20 THEN 'Function'
              WHEN r.repo = 'repository:meda' AND g % 100 < 24 THEN 'Function'
              WHEN r.repo = 'repository:meda' AND g % 100 < 27 THEN 'TerraformVariable'
              WHEN r.repo = 'repository:meda' AND g % 100 < 30 THEN 'TerraformResource'
              ELSE 'Function' END)
    ) AS t(lang, etype)
) AS m;
