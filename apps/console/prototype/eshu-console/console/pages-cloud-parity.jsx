/* Eshu Console - prototype cloud live parity overlay.
   Demo mode delegates to the original rich cloud page. Live mode uses the same
   bounded cloud resources endpoint as the production console. */
(function () {
  const DemoCloud = window.Cloud;
  const { useEffect: useEffectCP, useMemo: useMemoCP, useState: useStateCP } = React;
  const PAGE_LIMIT = 50;

  function cloudData(response) {
    if (response && response.error) throw new Error(response.error.message || response.error.code || "api error");
    return response && Object.prototype.hasOwnProperty.call(response, "data") ? response.data : response;
  }

  function text(value) {
    return value == null ? "" : String(value);
  }

  function buildCloudPath(filters, cursor) {
    const params = new URLSearchParams();
    params.set("limit", String(PAGE_LIMIT));
    if (filters.provider) params.set("provider", filters.provider);
    if (filters.resourceType) params.set("resource_type", filters.resourceType);
    if (filters.region) params.set("region", filters.region);
    if (filters.accountId) params.set("account_id", filters.accountId);
    if (cursor && cursor.afterResourceType && cursor.afterId) {
      params.set("after_resource_type", cursor.afterResourceType);
      params.set("after_id", cursor.afterId);
    }
    return "/api/v0/cloud/resources?" + params.toString();
  }

  function rowFromResource(row) {
    return {
      id: text(row.id),
      resourceType: text(row.resource_type),
      name: text(row.name),
      provider: text(row.provider),
      region: text(row.region),
      accountId: text(row.account_id),
      arn: text(row.arn),
      serviceName: text(row.service_name),
      state: text(row.state)
    };
  }

  async function loadCloudPage(client, filters, cursor) {
    const env = await client.get(buildCloudPath(filters, cursor));
    const data = cloudData(env) || {};
    const rows = (data.resources || []).map(rowFromResource).filter((row) => row.id);
    const next = data.next_cursor || {};
    return {
      rows,
      count: typeof data.count === "number" ? data.count : rows.length,
      truncated: data.truncated === true,
      nextCursor: data.truncated && text(next.after_id) ? { afterResourceType: text(next.after_resource_type), afterId: text(next.after_id) } : null,
      truth: env.truth || {}
    };
  }

  function familyFor(row) {
    const type = row.resourceType.toLowerCase();
    if (type.indexOf("iam") >= 0 || type.indexOf("security") >= 0 || type.indexOf("role") >= 0) {
      return { key: "identity", label: "Identity & access", color: "var(--blue)" };
    }
    if (type.indexOf("s3") >= 0 || type.indexOf("rds") >= 0 || type.indexOf("dynamo") >= 0 || type.indexOf("elastic") >= 0 || type.indexOf("postgres") >= 0) {
      return { key: "storage", label: "Storage", color: "var(--ember)" };
    }
    if (type.indexOf("vpc") >= 0 || type.indexOf("subnet") >= 0 || type.indexOf("load") >= 0 || type.indexOf("gateway") >= 0) {
      return { key: "network", label: "Network", color: "var(--teal)" };
    }
    if (type.indexOf("eks") >= 0 || type.indexOf("cluster") >= 0 || type.indexOf("lambda") >= 0 || type.indexOf("compute") >= 0) {
      return { key: "compute", label: "Compute", color: "var(--violet)" };
    }
    if (type.indexOf("log") >= 0 || type.indexOf("metric") >= 0 || type.indexOf("alarm") >= 0 || type.indexOf("dashboard") >= 0) {
      return { key: "observability", label: "Observability", color: "var(--ok, #22c55e)" };
    }
    return { key: "other", label: "Other", color: "var(--muted)" };
  }

  function familyRollups(rows) {
    const byKey = {};
    rows.forEach((row) => {
      const family = familyFor(row);
      byKey[family.key] = byKey[family.key] || Object.assign({ count: 0 }, family);
      byKey[family.key].count += 1;
    });
    return Object.keys(byKey).map((key) => byKey[key]).sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
  }

  function accountRollups(rows) {
    const byKey = {};
    rows.forEach((row) => {
      const id = row.accountId || "unknown-account";
      byKey[id] = byKey[id] || { id, provider: row.provider || "provider", region: row.region || "-", count: 0 };
      byKey[id].count += 1;
    });
    return Object.keys(byKey).map((key) => byKey[key]).sort((a, b) => b.count - a.count || a.id.localeCompare(b.id));
  }

  function cloudGraph(rows, accountId) {
    const scoped = rows.filter((row) => !accountId || (row.accountId || "unknown-account") === accountId);
    const nodes = [];
    const edges = [];
    const seen = new Set();
    function add(node) {
      if (seen.has(node.id)) return;
      seen.add(node.id);
      nodes.push(node);
    }
    scoped.forEach((row) => {
      const account = row.accountId || "unknown-account";
      const region = row.region || "unknown-region";
      const family = familyFor(row);
      const accountNode = "account:" + account;
      const regionNode = "region:" + account + ":" + region;
      const familyNode = "family:" + account + ":" + region + ":" + family.key;
      add({ id: accountNode, kind: "aws", label: account, sub: row.provider || "provider", col: 0, hero: true });
      add({ id: regionNode, kind: "aws", label: region, sub: "region", col: 1 });
      add({ id: familyNode, kind: "aws", label: family.label, sub: "resource family", col: 2 });
      add({ id: row.id, kind: "aws", label: row.name || row.id, sub: row.resourceType, col: 3, _res: row });
      edges.push({ s: accountNode, t: regionNode, verb: "CONTAINS", layer: "infra" });
      edges.push({ s: regionNode, t: familyNode, verb: "GROUPS", layer: "infra" });
      edges.push({ s: familyNode, t: row.id, verb: "HAS_RESOURCE", layer: "infra" });
    });
    return { nodes, edges };
  }

  function Cloud({ data, client, onOpenService, onOpenNode }) {
    if (!client) return <DemoCloud data={data} client={client} onOpenService={onOpenService} onOpenNode={onOpenNode} />;
    return <LiveCloud data={data || ESHU} client={client} />;
  }

  function LiveCloud({ data, client }) {
    const D = data || ESHU;
    const emptyFilters = { provider: "", resourceType: "", region: "", accountId: "" };
    const [page, setPage] = useStateCP(null);
    const [busy, setBusy] = useStateCP(false);
    const [err, setErr] = useStateCP("");
    const [view, setView] = useStateCP("network");
    const [draft, setDraft] = useStateCP(emptyFilters);
    const [filters, setFilters] = useStateCP(emptyFilters);
    const [stack, setStack] = useStateCP([null]);
    const [networkAccount, setNetworkAccount] = useStateCP("");

    function fetchPage(nextFilters, cursor) {
      setBusy(true);
      setErr("");
      loadCloudPage(client, nextFilters, cursor)
        .then((next) => { setPage(next); setBusy(false); })
        .catch((error) => { setPage(null); setErr((error && error.message) || "failed"); setBusy(false); });
    }

    useEffectCP(() => {
      let cancelled = false;
      setBusy(true);
      setErr("");
      loadCloudPage(client, filters, null)
        .then((next) => { if (!cancelled) { setPage(next); setBusy(false); } })
        .catch((error) => { if (!cancelled) { setPage(null); setErr((error && error.message) || "failed"); setBusy(false); } });
      return () => { cancelled = true; };
    }, [client, filters.provider, filters.resourceType, filters.region, filters.accountId]);

    const rows = page && page.rows ? page.rows : [];
    const families = useMemoCP(() => familyRollups(rows), [rows]);
    const accounts = useMemoCP(() => accountRollups(rows), [rows]);
    const selectedAccount = networkAccount || (accounts[0] && accounts[0].id) || "";
    const graph = useMemoCP(() => cloudGraph(rows, selectedAccount), [rows, selectedAccount]);
    const pageNumber = stack.length;

    function applyFilters(event) {
      event.preventDefault();
      setStack([null]);
      setFilters({
        provider: draft.provider.trim(),
        resourceType: draft.resourceType.trim(),
        region: draft.region.trim(),
        accountId: draft.accountId.trim()
      });
    }

    function resetFilters() {
      setDraft(emptyFilters);
      setStack([null]);
      setFilters(emptyFilters);
    }

    function nextPage() {
      if (!page || !page.nextCursor) return;
      const cursor = page.nextCursor;
      setStack((current) => current.concat([cursor]));
      fetchPage(filters, cursor);
    }

    function prevPage() {
      if (stack.length <= 1) return;
      const nextStack = stack.slice(0, -1);
      setStack(nextStack);
      fetchPage(filters, nextStack[nextStack.length - 1]);
    }

    return (
      <div className="page">
        <div className="page-intro">
          <h2>Cloud</h2>
          <p>Cloud-provider resource inventory from <span className="mono">GET /api/v0/cloud/resources</span>. Network and table views are derived from the current bounded page of authoritative <span className="mono">CloudResource</span> rows.</p>
        </div>

        <div className="grid g-4">
          <StatTile label="Cloud resources" value={page ? fmt(page.count) : fmt(rows.length)} color="var(--blue)" sub={"page " + pageNumber + (page && page.truncated ? " - more available" : "")} />
          <StatTile label="Accounts" value={accounts.length} color="var(--ember)" sub="on current page" />
          <StatTile label="Resource families" value={families.length} color="var(--teal)" sub="typed from resource_type" />
          <StatTile label="Endpoint" value="live" color="var(--violet)" sub="/api/v0/cloud/resources" />
        </div>

        <div className="grid mt" style={{ gridTemplateColumns: "minmax(0,1fr) minmax(0,1fr)", gap: "var(--gap)" }}>
          <Panel title="Resources by family" sub="Current bounded page" glyph={<Icon.layers />}>
            <div className="kv-list">
              {families.map((family) => <div className="kv" key={family.key}><span style={{ color: family.color }}>{family.label}</span><strong>{family.count}</strong></div>)}
              {families.length === 0 ? <p className="empty">No family rollup yet.</p> : null}
            </div>
          </Panel>
          <Panel title="Accounts" sub="Provider - region - resources" glyph={<Icon.cloud />}>
            <div className="acct-list">
              {accounts.map((account) => (
                <button key={account.id} type="button" className="acct-row" onClick={() => setNetworkAccount(account.id)}>
                  <span className="acct-prov"><i />{account.provider}</span>
                  <span className="cell-stack" style={{ flex: 1, minWidth: 0 }}><span className="t-name" style={{ fontSize: ".84rem" }}>{account.id}</span><small className="mono">{account.region}</small></span>
                  <span className="mono t-mut" style={{ fontSize: ".78rem" }}>{account.count}</span>
                </button>
              ))}
              {accounts.length === 0 ? <p className="empty">No accounts on this page.</p> : null}
            </div>
          </Panel>
        </div>

        <div className="row mt" style={{ justifyContent: "space-between", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <div className="dep-toggle" style={{ margin: 0 }}>
            <button className={view === "network" ? "active" : ""} onClick={() => setView("network")}>Network</button>
            <button className={view === "table" ? "active" : ""} onClick={() => setView("table")}>Table</button>
          </div>
          {view === "network" && accounts.length ? (
            <div className="seg branch-seg"><Icon.cloud size={14} />{accounts.map((account) => <button key={account.id} className={selectedAccount === account.id ? "active" : ""} onClick={() => setNetworkAccount(account.id)}>{account.id}</button>)}</div>
          ) : null}
        </div>

        <form className="row mt" style={{ gap: 8, flexWrap: "wrap", alignItems: "center" }} onSubmit={applyFilters}>
          <input className="popover-input mono" placeholder="provider (aws)" value={draft.provider} onChange={(e) => setDraft((current) => Object.assign({}, current, { provider: e.target.value }))} />
          <input className="popover-input mono" placeholder="resource_type (aws_iam_role)" value={draft.resourceType} onChange={(e) => setDraft((current) => Object.assign({}, current, { resourceType: e.target.value }))} />
          <input className="popover-input mono" placeholder="region (us-east-1)" value={draft.region} onChange={(e) => setDraft((current) => Object.assign({}, current, { region: e.target.value }))} />
          <input className="popover-input mono" placeholder="account_id" value={draft.accountId} onChange={(e) => setDraft((current) => Object.assign({}, current, { accountId: e.target.value }))} />
          <button type="submit" className="btn-ghost active">Apply</button>
          <button type="button" className="btn-ghost" onClick={resetFilters}>Reset</button>
        </form>

        {view === "network" ? (
          <Panel className="flush mt" title="Network topology" sub={"Account -> region -> family -> resources - " + (selectedAccount || "no account selected")} glyph={<Icon.branch />}>
            {busy && page === null ? <div className="conn-state" style={{ padding: 40 }}><div className="conn-spinner" aria-hidden /><p>Loading cloud resources...</p></div> : graph.nodes.length ? <GraphCanvas graph={graph} data={D} layout="layered" height={520} onSelect={(node) => window.ESHU_ROUTES.setHash("explorer", "?q=" + encodeURIComponent(node.id))} /> : <p className="empty">{err ? "Failed to load: " + err : "No cloud resources match this scope."}</p>}
          </Panel>
        ) : (
          <Panel className="flush mt" title={"Cloud resources - page " + pageNumber} sub="Grouped by family - live" glyph={<Icon.cloud />}>
            <table className="tbl cloud-tbl">
              <thead><tr><th>Type</th><th>Name / ID</th><th>Region</th><th>Account</th><th>Provider</th><th>State</th><th>Family</th><th></th></tr></thead>
              <tbody>
                {families.map((family) => (
                  <React.Fragment key={family.key}>
                    <tr className="group-row"><td colSpan={8}><span className="group-label" style={{ color: family.color }}>{family.label}</span><span className="group-meta">{family.count} {family.count === 1 ? "resource" : "resources"}</span></td></tr>
                    {rows.filter((row) => familyFor(row).key === family.key).map((row) => <CloudRow key={row.id} row={row} />)}
                  </React.Fragment>
                ))}
                {rows.length === 0 ? <tr><td colSpan={8}><p className="empty">{err ? "Failed to load: " + err : "No cloud resources match this scope."}</p></td></tr> : null}
              </tbody>
            </table>
          </Panel>
        )}

        <div className="row" style={{ gap: 8, justifyContent: "space-between", padding: "10px 0" }}>
          <span className="t-mut mono" style={{ fontSize: ".76rem" }}>{page ? page.count + " on this page" + (page.truncated ? " - more available" : "") : "-"}</span>
          <span className="row" style={{ gap: 8 }}>
            <button type="button" className="btn-ghost" disabled={busy || pageNumber <= 1} onClick={prevPage}>Prev</button>
            <button type="button" className="btn-ghost active" disabled={busy || !page || !page.nextCursor} onClick={nextPage}>Next</button>
          </span>
        </div>
      </div>
    );
  }

  function CloudRow({ row }) {
    const family = familyFor(row);
    const label = row.name || row.id;
    return (
      <tr>
        <td className="mono" style={{ fontSize: ".78rem" }}>{row.resourceType || "-"}</td>
        <td className="t-name" title={row.arn || row.id}><a href={window.ESHU_ROUTES.hashFor("explorer", "?q=" + encodeURIComponent(row.id))}>{label}</a></td>
        <td className="t-mut">{row.region || "-"}</td>
        <td className="t-mut mono" style={{ fontSize: ".76rem" }}>{row.accountId || "-"}</td>
        <td className="t-mut">{row.provider || "-"}</td>
        <td className="t-mut">{row.state || "-"}</td>
        <td><Badge tone="neutral">{family.label}</Badge></td>
        <td className="t-mut mono" style={{ fontSize: ".7rem" }}>{row.serviceName || ""}</td>
      </tr>
    );
  }

  window.Cloud = Cloud;
})();
