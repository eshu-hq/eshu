/* Extends the standalone prototype live loader with newer console surfaces.
   Kept outside data.js so the legacy fixture file does not keep growing. */
(function () {
  if (!window.ESHU || typeof window.ESHU.loadLive !== "function") return;

  const baseLoadLive = window.ESHU.loadLive;

  function str(value) {
    return typeof value === "string" ? value : "";
  }
  function num(value) {
    return typeof value === "number" && Number.isFinite(value) ? value : 0;
  }
  function truthLevel(env) {
    return env && env.truth && env.truth.level ? env.truth.level : "exact";
  }
  async function section(out, key, fn) {
    try {
      const value = await fn();
      if (value !== undefined && value !== null) {
        Object.assign(out, value);
        out.prov[key] = "live";
      } else {
        out.prov[key] = "empty";
      }
    } catch (e) {
      out.prov[key] = "error:" + ((e && e.message) || "failed");
    }
  }

  function mapImage(row, env) {
    const repository = str(row.repository);
    const tag = str(row.tag);
    const imageBase = [str(row.registry), repository].filter(Boolean).join("/");
    return {
      id: str(row.id) || str(row.digest),
      digest: str(row.digest),
      registry: str(row.registry),
      repository,
      name: str(row.name) || repository,
      tag,
      image: imageBase ? imageBase + (tag ? ":" + tag : "") : str(row.digest),
      mediaType: str(row.media_type),
      artifactType: str(row.artifact_type),
      sizeBytes: num(row.size_bytes) || null,
      sourceSystem: str(row.source_system),
      truth: truthLevel(env)
    };
  }

  function mapIac(row, env) {
    return {
      id: str(row.id),
      name: str(row.name) || str(row.resource_name) || str(row.id),
      kind: str(row.type) || str(row.kind) || "resource",
      ownerId: str(row.resource_service),
      owner: str(row.resource_service) || "shared platform",
      source: str(row.relative_path) ? str(row.relative_path) : "Terraform state",
      account: str(row.provider),
      region: str(row.module),
      truth: truthLevel(env)
    };
  }

  function mapDependency(row) {
    return {
      id: str(row.edge_id) || [str(row.anchor_package), str(row.related_package)].join("->"),
      sourceLabel: str(row.anchor_package) || str(row.anchor_package_id) || "package",
      targetLabel: str(row.related_package) || str(row.related_package_id) || "dependency",
      verb: row.direction === "reverse" ? "DEPENDED_ON_BY" : "DEPENDS_ON",
      layer: "code",
      system: str(row.related_ecosystem),
      range: str(row.dependency_range),
      dependencyType: str(row.dependency_type),
      optional: row.optional === true
    };
  }

  function mapSbomBucket(bucket) {
    return {
      id: str(bucket.value),
      advisory: str(bucket.value),
      pkg: str(bucket.dimension) || "subject_digest",
      version: "",
      ecosystem: "sbom",
      severity: "",
      source: "sbom-attestations",
      fix: "",
      services: [],
      kind: "bucket",
      count: num(bucket.count)
    };
  }

  window.ESHU.loadLive = async function loadLiveWithParity(client) {
    const out = await baseLoadLive(client);
    out.prov = out.prov || {};

    await section(out, "imageInventory", async () => {
      const env = await client.get("/api/v0/images?limit=50&offset=0");
      const rows = ((env.data && env.data.images) || []).map((row) => mapImage(row, env)).filter((row) => row.id);
      return rows.length ? { imageInventory: rows } : null;
    });

    await section(out, "iacParityRows", async () => {
      const env = await client.get("/api/v0/iac/resources?limit=200");
      const rows = ((env.data && env.data.resources) || []).map((row) => mapIac(row, env)).filter((row) => row.id);
      return rows.length ? { iacParityRows: rows } : null;
    });

    await section(out, "sbomInventory", async () => {
      const countEnv = await client.get("/api/v0/supply-chain/sbom-attestations/attachments/count");
      const invEnv = await client.get("/api/v0/supply-chain/sbom-attestations/attachments/inventory?group_by=subject_digest&limit=50&offset=0");
      const buckets = ((invEnv.data && invEnv.data.buckets) || []).map(mapSbomBucket).filter((row) => row.id);
      return {
        sbomSummary: {
          total: num(countEnv.data && countEnv.data.total_attachments),
          byStatus: (countEnv.data && countEnv.data.by_attachment_status) || {},
          byArtifactKind: (countEnv.data && countEnv.data.by_artifact_kind) || {},
          truth: truthLevel(countEnv)
        },
        sbomInventory: {
          groupBy: str(invEnv.data && invEnv.data.group_by) || "subject_digest",
          buckets,
          truncated: Boolean(invEnv.data && invEnv.data.truncated)
        }
      };
    });

    await section(out, "dependencyInventory", async () => {
      const env = await client.get("/api/v0/dependencies?direction=forward&limit=50");
      const rows = ((env.data && env.data.dependencies) || []).map(mapDependency).filter((row) => row.id);
      return rows.length ? { dependencyInventory: rows } : null;
    });

    return out;
  };
})();
