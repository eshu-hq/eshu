/* Repository source route parity for the standalone prototype. */
(function () {
  const { useEffect: useEffectSP, useState: useStateSP } = React;

  function sourceRoute() {
    const raw = String(location.hash || "#repositories/svc-catalog/source").slice(1);
    const parts = raw.split("?");
    const routeParts = parts[0].split("/");
    const params = new URLSearchParams(parts.slice(1).join("?"));
    return {
      repoId: decodeURIComponent(routeParts[1] || "svc-catalog"),
      filePath: params.get("path") || "",
      lineStart: lineParam(params.get("lineStart")),
      lineEnd: lineParam(params.get("lineEnd"))
    };
  }

  function lineParam(value) {
    const line = Number(value);
    return Number.isInteger(line) && line > 0 ? line : null;
  }

  function envelopeErrorMessage(error) {
    if (!error) return "";
    if (typeof error === "string") return error;
    if (typeof error !== "object") return "api error";
    return String(error.message || error.code || "api error");
  }

  function apiData(env) {
    const message = envelopeErrorMessage(env && env.error);
    if (message) throw new Error(message);
    return env && env.data ? env.data : {};
  }

  function parentPath(path) {
    const idx = path.lastIndexOf("/");
    return idx > 0 ? path.slice(0, idx) : "";
  }

  function serviceForRepo(D, repoId) {
    const clean = repoId.replace(/^repository:/, "");
    return D.servicesById[clean] || D.services.find((svc) =>
      svc.id === clean || svc.name === clean || (svc.repo && (svc.repo === clean || svc.repo.endsWith("/" + clean)))
    );
  }

  function repoSourceDisplayName(D, repoId) {
    const svc = serviceForRepo(D, repoId);
    return (svc && (svc.name || svc.id)) || repoId;
  }

  function flattenTree(items, prefix) {
    const rows = [];
    (items || []).forEach((item) => {
      const path = prefix ? prefix + "/" + item.name : item.name;
      rows.push(Object.assign({}, item, { path }));
      if (item.type === "dir") rows.push(...flattenTree(item.items || [], path));
    });
    return rows;
  }

  function childEntries(items, path) {
    if (!path) return items || [];
    const parts = path.split("/");
    let current = items || [];
    for (const part of parts) {
      const next = current.find((item) => item.type === "dir" && item.name === part);
      if (!next) return [];
      current = next.items || [];
    }
    return current;
  }

  function demoTree(D, repoId, path) {
    const svc = serviceForRepo(D, repoId);
    if (!svc || typeof repoTree !== "function") return null;
    const root = repoTree({
      id: svc.id,
      name: svc.name,
      slug: svc.repo || svc.name,
      lang: svc.lang,
      langLabel: (D.lang[svc.lang] || {}).label || svc.lang,
      owner: svc.owner,
      system: svc.system,
      version: svc.version,
      port: svc.port,
      deps: svc.deps || [],
      desc: svc.story || svc.name
    });
    const entries = childEntries(root, path).map((item) => ({
      name: item.name,
      type: item.type,
      path: path ? path + "/" + item.name : item.name,
      size: item.content ? item.content.split("\n").length : null,
      childCount: item.items ? item.items.length : null
    }));
    return { ref: "demo-indexed-ref", path, entries, root };
  }

  function demoFile(D, repoId, path) {
    const tree = demoTree(D, repoId, "");
    if (!tree) return null;
    const row = flattenTree(tree.root, "").find((item) => item.path === path && item.type === "file");
    if (!row) return null;
    return {
      path,
      ref: tree.ref,
      encoding: "utf-8",
      content: row.content || "",
      size: (row.content || "").split("\n").length,
      language: path.split(".").pop() || null,
      truncated: false,
      provenance: "demo"
    };
  }

  async function liveTree(client, repoId, path) {
    const qs = path ? "?path=" + encodeURIComponent(path) : "";
    const env = await client.get("/api/v0/repositories/" + encodeURIComponent(repoId) + "/tree" + qs);
    const data = apiData(env);
    const entries = (data.entries || []).map((entry) => ({
      name: entry.name || "",
      type: entry.type === "dir" ? "dir" : "file",
      path: entry.path || "",
      size: typeof entry.size === "number" ? entry.size : null,
      childCount: typeof entry.child_count === "number" ? entry.child_count : null
    })).filter((entry) => entry.name);
    return { ref: data.ref || "", path: data.path || path, entries };
  }

  async function liveBranches(client, repoId) {
    const env = await client.get("/api/v0/repositories/" + encodeURIComponent(repoId) + "/branches");
    const data = apiData(env);
    const branches = (data.branches || []).map((branch) => ({
      name: branch.name || "",
      headSha: branch.head_sha || "",
      lastIndexedAt: branch.last_indexed_at || null
    })).filter((branch) => branch.name || branch.headSha);
    return { defaultBranch: data.default_branch || "", branches };
  }

  async function liveFile(client, repoId, path) {
    const env = await client.get("/api/v0/repositories/" + encodeURIComponent(repoId) + "/content?path=" + encodeURIComponent(path));
    const data = apiData(env);
    return {
      path: data.path || path,
      ref: data.ref || "",
      encoding: data.encoding === "base64" ? "base64" : "utf-8",
      content: data.content || "",
      size: typeof data.size === "number" ? data.size : 0,
      language: data.language || null,
      truncated: data.truncated === true,
      provenance: "live"
    };
  }

  async function liveRepoDisplayName(client, repoId) {
    const env = await client.get("/api/v0/repositories?limit=500&offset=0");
    const repos = apiData(env).repositories || [];
    const match = repos.find((repo) => repo && (repo.id === repoId || repo.name === repoId));
    return (match && (match.name || match.id)) || repoId;
  }

  function RepoSource({ data, client }) {
    const D = data || ESHU;
    const [{ repoId, filePath, lineStart, lineEnd }, setRoute] = useStateSP(sourceRoute);
    const [path, setPath] = useStateSP(parentPath(filePath));
    const [tree, setTree] = useStateSP(null);
    const [treeErr, setTreeErr] = useStateSP("");
    const [file, setFile] = useStateSP(null);
    const [fileErr, setFileErr] = useStateSP("");
    const [repoLabel, setRepoLabel] = useStateSP(repoSourceDisplayName(D, repoId));
    const [branches, setBranches] = useStateSP(null);
    const [branchesErr, setBranchesErr] = useStateSP("");

    useEffectSP(() => {
      function onHash() {
        const next = sourceRoute();
        setRoute(next);
        setPath(parentPath(next.filePath));
      }
      window.addEventListener("hashchange", onHash);
      return () => window.removeEventListener("hashchange", onHash);
    }, []);

    useEffectSP(() => {
      let cancelled = false;
      setTree(null); setTreeErr("");
      const load = client ? liveTree(client, repoId, path) : Promise.resolve(demoTree(D, repoId, path));
      load.then((value) => { if (!cancelled) setTree(value); })
        .catch((e) => { if (!cancelled) setTreeErr((e && e.message) || "failed"); });
      return () => { cancelled = true; };
    }, [D, client, repoId, path]);

    useEffectSP(() => {
      let cancelled = false;
      setBranches(null); setBranchesErr("");
      if (!client) return () => { cancelled = true; };
      liveBranches(client, repoId)
        .then((value) => { if (!cancelled) setBranches(value); })
        .catch((e) => { if (!cancelled) setBranchesErr((e && e.message) || "failed"); });
      return () => { cancelled = true; };
    }, [client, repoId]);

    useEffectSP(() => {
      let cancelled = false;
      setRepoLabel(repoSourceDisplayName(D, repoId));
      if (!client) return () => { cancelled = true; };
      liveRepoDisplayName(client, repoId)
        .then((label) => { if (!cancelled) setRepoLabel(label); })
        .catch(() => { if (!cancelled) setRepoLabel(repoId); });
      return () => { cancelled = true; };
    }, [D, client, repoId]);

    useEffectSP(() => {
      let cancelled = false;
      setFile(null); setFileErr("");
      if (!filePath) return () => { cancelled = true; };
      const load = client ? liveFile(client, repoId, filePath) : Promise.resolve(demoFile(D, repoId, filePath));
      load.then((value) => { if (!cancelled) setFile(value); })
        .catch((e) => { if (!cancelled) setFileErr((e && e.message) || "failed"); });
      return () => { cancelled = true; };
    }, [D, client, repoId, filePath]);

    function openEntry(entry) {
      if (entry.type === "dir") {
        setPath(entry.path);
        return;
      }
      const suffix = "/" + encodeURIComponent(repoId) + "/source?path=" + encodeURIComponent(entry.path);
      window.ESHU_ROUTES.setHash("reposource", suffix);
    }

    const crumbs = path ? path.split("/") : [];
    const indexedRef = (branches && branches.branches[0] && branches.branches[0].headSha) || (tree && tree.ref) || "";
    const indexedBranchName = (branches && branches.branches[0] && branches.branches[0].name) || (branches && branches.defaultBranch) || "";
    const lastIndexedAt = (branches && branches.branches[0] && branches.branches[0].lastIndexedAt) || null;
    return (
      <div className="page" style={{ maxWidth: "none" }}>
        <div className="page-intro">
          <a className="link-btn" href={window.ESHU_ROUTES.hashFor("repos")}>{"<-"} Repositories</a>
          <h2 style={{ marginTop: 8 }}>{repoLabel} <span className="t-mut" style={{ fontSize: "0.8rem", fontWeight: 400 }}>· source</span></h2>
          <p>File tree and code viewer from <span className="mono">/api/v0/repositories/{"{id}"}/tree</span>, <span className="mono">/content?path=</span>, and <span className="mono">/branches</span>.</p>
          <div className="explorer-filters" style={{ gap: 8, marginTop: 10 }}>
            <span className="t-mut">Indexed ref</span>
            {indexedRef ? <Badge tone="neutral">{String(indexedRef).slice(0, 10)}</Badge> : <Badge tone="neutral">unavailable</Badge>}
            {indexedBranchName ? <span className="t-mut mono">{indexedBranchName}</span> : null}
            {lastIndexedAt ? <span className="t-mut mono">{new Date(lastIndexedAt).toLocaleString()}</span> : null}
            {branchesErr ? <span className="t-mut">ref list unavailable: {branchesErr}</span> : null}
          </div>
        </div>

        <div className="explorer-filters" style={{ gap: 4 }}>
          <button className="link-btn" onClick={() => setPath("")}>root</button>
          {crumbs.map((crumb, index) => (
            <span key={index}><span className="t-mut">/</span> <button className="link-btn" onClick={() => setPath(crumbs.slice(0, index + 1).join("/"))}>{crumb}</button></span>
          ))}
        </div>

        <div className="grid" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,2fr)", gap: "var(--gap)" }}>
          <Panel className="flush" title="Files" sub={tree ? tree.entries.length + " entries" : "loading..."}>
            {treeErr ? <p className="empty" style={{ padding: 20 }}>Repository source unavailable: {treeErr}</p>
              : !tree ? <div className="conn-state" style={{ padding: 32 }}><div className="conn-spinner" aria-hidden /><p>Loading tree...</p></div>
                : (
                  <table className="tbl">
                    <tbody>
                      {tree.entries.map((entry) => (
                        <tr key={entry.path} style={{ cursor: "pointer" }} onClick={() => openEntry(entry)}>
                          <td className="t-name">{entry.type === "dir" ? "[dir] " : "[file] "}{entry.name}</td>
                          <td className="t-mut mono" style={{ fontSize: ".72rem", textAlign: "right" }}>{entry.type === "dir" ? (entry.childCount || 0) + " files" : entry.size != null ? entry.size + " lines" : ""}</td>
                        </tr>
                      ))}
                      {!tree.entries.length ? <tr><td className="empty">Empty directory.</td></tr> : null}
                    </tbody>
                  </table>
                )}
          </Panel>

          <Panel className="flush" title={file ? file.path : "Viewer"} sub={file ? (file.language || file.provenance) : fileErr ? "unavailable" : "select a file"}>
            {fileErr ? <p className="empty" style={{ padding: 28 }}>Repository source unavailable: {fileErr}</p>
              : !file ? <p className="empty" style={{ padding: 28 }}>Select a file to view its source.</p>
                : file.provenance === "unavailable" ? <p className="empty" style={{ padding: 28 }}>File content unavailable from this source.</p>
                  : renderSource(file, lineStart, lineEnd)}
          </Panel>
        </div>
      </div>
    );
  }

  function renderSource(file, start, end) {
    if (file.encoding === "base64") return <p className="empty" style={{ padding: 28 }}>Binary file ({file.size} bytes) not shown.</p>;
    const lines = String(file.content || "").split("\n");
    return (
      <div className="code-view">
        {file.truncated ? <div className="prov-banner warn" style={{ padding: "6px 12px" }}>Truncated to the size cap.</div> : null}
        <pre className="code-pre"><code>{lines.map((line, index) => {
          const n = index + 1;
          const hot = start !== null && n >= start && n <= (end || start);
          return <span key={n} className={cx("code-line", hot && "is-highlighted")}><span className="code-ln">{n}</span>{line}{"\n"}</span>;
        })}</code></pre>
      </div>
    );
  }

  window.RepoSource = RepoSource;
})();
