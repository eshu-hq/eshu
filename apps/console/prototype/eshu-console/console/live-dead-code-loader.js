/* Dead-code live loader shared by the standalone prototype parity shim. */
(function () {
  window.ESHU_LIVE_PARITY = window.ESHU_LIVE_PARITY || {};

  function str(value) {
    return typeof value === "string" ? value : "";
  }

  function num(value) {
    return typeof value === "number" && Number.isFinite(value) ? value : 0;
  }

  function truthLevel(env) {
    return env && env.truth && env.truth.level ? env.truth.level : "exact";
  }

  function envelopeErrorMessage(error) {
    if (!error) return "";
    if (typeof error === "string") return error;
    if (typeof error !== "object") return "api error";
    return str(error.message) || str(error.code) || "api error";
  }

  function apiData(env) {
    const message = envelopeErrorMessage(env && env.error);
    if (message) throw new Error(message);
    return env && env.data ? env.data : {};
  }

  async function loadRepositoryNameLookup(client) {
    const env = await client.get("/api/v0/repositories?limit=500&offset=0");
    const rows = apiData(env).repositories || [];
    const names = {};
    rows.forEach((row) => {
      const id = str(row.id);
      const name = str(row.name);
      if (id && name) names[id] = name;
    });
    return names;
  }

  function repoDisplayName(row, repoNames) {
    const explicit = str(row.repo_name);
    const repoId = str(row.repo_id);
    if (explicit) return explicit;
    if (repoId && repoNames && repoNames[repoId]) return repoNames[repoId];
    return repositoryFallbackLabel(repoId);
  }

  function repositoryFallbackLabel(repoId) {
    const id = str(repoId).trim();
    if (!id) return "repository";
    const prefixed = id.match(/^repository[:_](.+)$/i);
    if (!prefixed) return id;
    const suffix = prefixed[1] || "";
    if (/^r_[0-9a-f]+$/i.test(suffix) || /^r[0-9a-f]+$/i.test(suffix)) return "unresolved repository";
    return suffix || "repository";
  }

  function mapDeadCode(row, index, env, repoNames) {
    const labels = Array.isArray(row.labels) ? row.labels : [];
    const line = num(row.line) || num(row.start_line);
    const endLine = num(row.end_line) || line;
    const repoId = str(row.repo_id);
    const displayName = repoDisplayName(row, repoNames);
    return {
      id: str(row.entity_id) || "dc-" + index,
      entityId: str(row.entity_id),
      repo: displayName,
      repoId,
      repoName: displayName,
      symbol: str(row.name) || "symbol",
      file: str(row.file_path) || str(row.relative_path),
      line,
      endLine,
      kind: (str(labels[0]) || str(row.entity_kind) || "function").toLowerCase(),
      refs: num(row.reference_count),
      confidence: truthLevel(env),
      age: "live",
      loc: num(row.loc) || num(row.line_count),
      reason: str(row.reason) || str(row.classification) || "No inbound CALLS / IMPORTS edges",
      classification: str(row.classification)
    };
  }

  window.ESHU_LIVE_PARITY.loadDeadCode = async function loadDeadCode(client) {
    let repoNames = {};
    try { repoNames = await loadRepositoryNameLookup(client); } catch (e) {}
    const env = await client.post("/api/v0/code/dead-code", { limit: 100 });
    const rows = (apiData(env).results || [])
      .map((row, index) => mapDeadCode(row, index, env, repoNames));
    return rows.length ? { deadCode: rows } : null;
  };
})();
