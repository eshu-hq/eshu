// pages/ExposurePathPage.tsx
// Entrypoint-first Exposure Path surface (#3403). An operator picks an
// internet-facing service entrypoint and the view renders the proven ingress
// chain (Internet -> entrypoint -> edge/runtime) as clickable hops, each opening
// its evidence + truth level inline, with WAF/TLS/hop posture tiles. The chain
// and posture come from the bounded service context at
// GET /api/v0/services/{name}/context (entrypoints, network_paths,
// ingress_posture). The original handler-trace form is kept as advanced mode.
//
// It never fabricates a chain: the synthetic "Internet" origin hop is drawn only
// for an observed-public entrypoint, and WAF/TLS tiles render the honest
// three-valued posture (protected/unprotected/unproven) returned by the backend.
import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router-dom";

import { ExposureIngressView, NoExposureChainNotice } from "./ExposureIngressView";
import { ExposurePathAdvanced } from "./ExposurePathAdvanced";
import { ExposureServiceSelector } from "./ExposureServiceSelector";
import type { EshuApiClient } from "../api/client";
import { loadExposureIngress, type ExposureIngress, type IngressHop } from "../api/exposureIngress";
import {
  exposureServiceOptions,
  resolveExposureServiceSelection,
  type ExposureServiceSelectionResult,
} from "../api/exposureServiceSelection";
import { Badge } from "../components/atoms";
import type { ServiceRow } from "../console/types";
import "./exposurePathPage.css";

export function ExposurePathPage({
  catalogTruncated = false,
  client,
  services = [],
}: {
  readonly catalogTruncated?: boolean;
  readonly client?: EshuApiClient;
  readonly services?: readonly ServiceRow[];
}): React.JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();
  const [service, setService] = useState(() => searchParams.get("service") ?? "");
  const [ingress, setIngress] = useState<ExposureIngress | null>(null);
  const [selectedChain, setSelectedChain] = useState(0);
  const [selectedHop, setSelectedHop] = useState<IngressHop | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const deepLinkRef = useRef("");
  const selectedCanonicalRef = useRef("");
  const requestVersionRef = useRef(0);
  const canLoad = client !== undefined;
  const options = useMemo(() => exposureServiceOptions(services), [services]);

  const clearDeepLink = useCallback(() => {
    deepLinkRef.current = "";
    if (!searchParams.has("service")) {
      return;
    }
    const params = new URLSearchParams(searchParams);
    params.delete("service");
    setSearchParams(params, { replace: true });
  }, [searchParams, setSearchParams]);

  const invalidateActiveRequest = useCallback(() => {
    requestVersionRef.current += 1;
    abortRef.current?.abort();
    abortRef.current = null;
    setBusy(false);
    setError("");
    setIngress(null);
    setSelectedChain(0);
    setSelectedHop(null);
  }, []);

  const runSelection = useCallback(
    async (query: string) => {
      const trimmed = query.trim();
      if (!client || trimmed.length === 0) {
        return;
      }
      const version = requestVersionRef.current + 1;
      requestVersionRef.current = version;
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;
      setBusy(true);
      setError("");
      setIngress(null);
      setSelectedChain(0);
      setSelectedHop(null);

      const resolution = await resolveExposureServiceSelection({
        client,
        options,
        query: trimmed,
        signal: controller.signal,
      });
      if (version !== requestVersionRef.current) {
        return;
      }
      if (resolution.status !== "resolved") {
        setError(selectionError(resolution));
        abortRef.current = null;
        setBusy(false);
        return;
      }

      const canonicalID = resolution.option.canonicalId;
      selectedCanonicalRef.current = canonicalID;
      setService(resolution.option.displayName);
      deepLinkRef.current = canonicalID;
      const params = new URLSearchParams(searchParams);
      params.set("service", canonicalID);
      setSearchParams(params, { replace: true });

      const loaded = await loadExposureIngress(client, canonicalID, controller.signal);
      if (version !== requestVersionRef.current) {
        return;
      }
      setIngress(loaded);
      if (loaded.provenance === "unavailable") {
        setError(loaded.error ?? "Service resolution or ingress tracing is unavailable.");
      }
      abortRef.current = null;
      setBusy(false);
    },
    [client, options, searchParams, setSearchParams],
  );

  useEffect(() => {
    const initial = searchParams.get("service")?.trim() ?? "";
    if (initial.length === 0) {
      if (deepLinkRef.current.length > 0) {
        deepLinkRef.current = "";
        selectedCanonicalRef.current = "";
        invalidateActiveRequest();
        setService("");
      }
      return;
    }
    if (!canLoad) {
      return;
    }
    if (deepLinkRef.current === initial) {
      return;
    }
    deepLinkRef.current = initial;
    void runSelection(initial);
  }, [canLoad, invalidateActiveRequest, runSelection, searchParams]);

  useEffect(
    () => () => {
      deepLinkRef.current = "";
      requestVersionRef.current += 1;
      abortRef.current?.abort();
    },
    [],
  );

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const name = service.trim();
    if (name.length === 0) {
      setError("Choose an authorized service or paste a canonical workload:… handle.");
      return;
    }
    void runSelection(selectedCanonicalRef.current || name);
  }

  const chains = ingress?.chains ?? [];
  const active = chains[selectedChain] ?? chains[0];

  return (
    <div className="page exposure-page">
      <div className="page-intro impact-intro">
        <div>
          <h2>Exposure Path</h2>
          <p>
            Pick an internet-facing entrypoint and follow its proven ingress chain to the runtime —
            each hop opens its evidence and truth level. Posture tiles summarize public entrypoints,
            hops, WAF coverage, and TLS termination.
          </p>
        </div>
        <Badge tone={canLoad ? "teal" : "warn"}>{canLoad ? "live API" : "connect live API"}</Badge>
      </div>

      <form className="exposure-entry-form" onSubmit={submit}>
        <ExposureServiceSelector
          busy={busy}
          catalogTruncated={catalogTruncated}
          onChoose={(option) => {
            invalidateActiveRequest();
            clearDeepLink();
            selectedCanonicalRef.current = option.canonicalId;
            setService(option.displayName);
          }}
          onValueChange={(value) => {
            invalidateActiveRequest();
            clearDeepLink();
            selectedCanonicalRef.current = "";
            setService(value);
          }}
          options={options}
          value={service}
        />
        <button className="btn-ghost active" disabled={!canLoad || busy} type="submit">
          {busy ? "Resolving…" : "Trace ingress"}
        </button>
      </form>

      {!canLoad ? <p className="inline-state">Live Eshu API connection unavailable.</p> : null}
      {error ? (
        <p className="src-err" role="alert">
          {error}
        </p>
      ) : null}

      {busy ? (
        <div className="conn-state compact mt">
          <div aria-hidden className="conn-spinner" />
          <p>Resolving service and tracing ingress chain…</p>
        </div>
      ) : ingress !== null && ingress.provenance === "live" ? (
        <ExposureIngressView
          active={active}
          ingress={ingress}
          onSelectChain={(index) => {
            setSelectedChain(index);
            setSelectedHop(null);
          }}
          onSelectHop={setSelectedHop}
          selectedChain={selectedChain}
          selectedHop={selectedHop}
        />
      ) : ingress !== null && ingress.provenance === "empty" && !error ? (
        <NoExposureChainNotice ingress={ingress} />
      ) : ingress === null && !error ? (
        <p className="empty mt">Enter an internet-facing service to trace its ingress chain.</p>
      ) : null}

      <details
        className="exposure-advanced-disclosure"
        onToggle={(e) => setAdvancedOpen((e.target as HTMLDetailsElement).open)}
      >
        <summary>Advanced: handler trace</summary>
        {advancedOpen ? <ExposurePathAdvanced client={client} /> : null}
      </details>
    </div>
  );
}

function selectionError(
  result: Exclude<ExposureServiceSelectionResult, { status: "resolved" }>,
): string {
  switch (result.status) {
    case "ambiguous":
      if (result.truncated) {
        return `The bounded resolver found ${result.candidates.length} authorized candidate${
          result.candidates.length === 1 ? "" : "s"
        } for “${result.query}”, but more may exist. Keep typing or choose a canonical workload:… handle.`;
      }
      return `Multiple authorized services match “${result.query}”. Choose one canonical service: ${result.candidates
        .map((candidate) => `${candidate.displayName} (${candidate.canonicalId})`)
        .join(", ")}.`;
    case "not_authorized":
      return "Your active session is not authorized to use service resolution.";
    case "not_found":
      return `No authorized service matches “${result.query}”. Choose an available service or paste a canonical workload:… handle.`;
    case "unavailable":
      return "Service resolution is temporarily unavailable. The prior ingress result was cleared.";
  }
}
