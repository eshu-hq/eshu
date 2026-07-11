// pages/GuidedQuestionsPage.tsx
// Guided questions -- the live counterpart to the console's demo_fixture
// "Prospect demo" mode (see console/demoModel.ts). This surface fetches the
// deterministic query playbook catalog from the LIVE
// GET /api/v0/query-playbooks API, lets an operator run a playbook against
// declared inputs via POST /api/v0/query-playbooks/resolve, and renders the
// resolved bounded tool calls plus the response's truth envelope. It never
// hardcodes a playbook: whatever the live catalog returns is what renders
// (issue #4745 authors the catalog entries; this component is generic).
//
// Distinct from demo mode by design (issue #4746): demo mode ships fixtures
// only and has no playbook engine to run, so it renders an explanatory state
// and never calls the API client -- mirroring AskPage's demo treatment.
import { useEffect, useRef, useState, type FormEvent } from "react";

import type { EshuApiClient } from "../api/client";
import {
  listPlaybooks,
  resolvePlaybook,
  type PlaybookCatalogPage,
  type PlaybookFailureMode,
  type QueryPlaybook,
  type ResolvedCall,
  type ResolvedPlaybook,
} from "../api/queryPlaybooks";
import { Badge, FreshDot, Panel, TruthChip } from "../components/atoms";
import type { SourceState } from "../components/SourceControls";
import { uiFresh, uiTruth } from "../console/types";
import "./liveInventory.css";

export function GuidedQuestionsPage({
  client,
  source,
}: {
  readonly client?: EshuApiClient;
  readonly source: SourceState;
}): React.JSX.Element {
  if (source.mode === "demo") {
    return (
      <div className="page">
        <Intro />
        <Panel className="mt">
          <div className="empty">
            <strong>Guided questions need a live connection.</strong>
            <p>
              Guided questions run playbooks against the live query-playbooks API. The prospect demo
              ships fixtures only, so there is no live catalog to run here. Connect to a live Eshu
              API with a shared or admin token from the data-source menu to run guided questions.
            </p>
          </div>
        </Panel>
      </div>
    );
  }
  return <GuidedQuestionsLive client={client} />;
}

function Intro(): React.JSX.Element {
  return (
    <div className="page-intro">
      <h2>Guided Questions</h2>
      <p>
        Pick a guided question from the live playbook catalog, supply its declared inputs, and run
        it against the live API. Every run renders the ordered bounded tool calls the resolver
        produced, their expected truth class, and the response&apos;s truth envelope -- live
        provenance, not a fixture.
      </p>
    </div>
  );
}

function GuidedQuestionsLive({ client }: { readonly client?: EshuApiClient }): React.JSX.Element {
  const [catalog, setCatalog] = useState<PlaybookCatalogPage | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    if (!client) {
      setCatalog({ playbooks: [], versions: [], count: 0, truth: null, provenance: "unavailable" });
      return;
    }
    void listPlaybooks(client).then((page) => {
      if (!cancelled) setCatalog(page);
    });
    return () => {
      cancelled = true;
    };
  }, [client]);

  return (
    <div className="page">
      <Intro />

      {catalog === null ? (
        <div className="conn-state compact mt">
          <div aria-hidden className="conn-spinner" />
          <p>Loading guided questions…</p>
        </div>
      ) : catalog.provenance === "unavailable" ? (
        <p className="empty mt">Guided questions catalog unavailable from this source.</p>
      ) : catalog.playbooks.length === 0 ? (
        <p className="empty mt">No guided questions are available from this source yet.</p>
      ) : (
        <ul aria-label="Guided questions" className="evidence-card-list mt">
          {catalog.playbooks.map((playbook) => (
            <PlaybookCard
              client={client}
              key={playbook.id}
              onToggle={() =>
                setSelectedId((current) => (current === playbook.id ? null : playbook.id))
              }
              playbook={playbook}
              selected={selectedId === playbook.id}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function PlaybookCard({
  client,
  onToggle,
  playbook,
  selected,
}: {
  readonly client: EshuApiClient | undefined;
  readonly onToggle: () => void;
  readonly playbook: QueryPlaybook;
  readonly selected: boolean;
}): React.JSX.Element {
  return (
    <li className="evidence-card">
      <div className="panel-action-stack">
        <div>
          <h3>{playbook.name}</h3>
          {playbook.description ? <p>{playbook.description}</p> : null}
        </div>
        {playbook.promptFamily ? <Badge tone="violet">{playbook.promptFamily}</Badge> : null}
      </div>
      <p className="mono">
        {playbook.requiredInputs.length === 0
          ? "no inputs required"
          : playbook.requiredInputs
              .map((input) => (input.required ? `${input.name} *` : input.name))
              .join(", ")}
      </p>
      <button
        aria-expanded={selected}
        className="btn-ghost active"
        onClick={onToggle}
        type="button"
      >
        {selected ? "Hide" : "Run"}
      </button>
      {selected ? <PlaybookRunner client={client} playbook={playbook} /> : null}
    </li>
  );
}

type RunPhase = "idle" | "resolving" | "resolved" | "error";

function PlaybookRunner({
  client,
  playbook,
}: {
  readonly client: EshuApiClient | undefined;
  readonly playbook: QueryPlaybook;
}): React.JSX.Element {
  const [values, setValues] = useState<Record<string, string>>({});
  const [validationError, setValidationError] = useState("");
  const [phase, setPhase] = useState<RunPhase>("idle");
  const [error, setError] = useState("");
  const [resolved, setResolved] = useState<ResolvedPlaybook | null>(null);
  const [truth, setTruth] = useState<{
    level: string;
    freshness: string;
    capability: string;
  } | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);
  // Gate the in-flight resolvePlaybook callbacks so collapsing the panel
  // ("Hide") or navigating away mid-request does not set state on an unmounted
  // component (mirrors GuidedQuestionsLive's cancelled-flag pattern).
  const mountedRef = useRef(true);
  useEffect(
    () => () => {
      mountedRef.current = false;
    },
    [],
  );

  useEffect(() => {
    if (phase === "resolved") {
      headingRef.current?.focus();
    }
  }, [phase]);

  function run(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    if (!client) {
      setValidationError("Live Eshu API connection unavailable.");
      return;
    }
    const missing = playbook.requiredInputs.find(
      (input) => input.required && (values[input.name] ?? "").trim().length === 0,
    );
    if (missing) {
      setValidationError(`${missing.name} is required.`);
      return;
    }
    setValidationError("");
    setPhase("resolving");
    setError("");
    const inputs: Record<string, string> = {};
    for (const input of playbook.requiredInputs) {
      const value = (values[input.name] ?? "").trim();
      if (value.length > 0) inputs[input.name] = value;
    }
    void resolvePlaybook(client, { playbookId: playbook.id, inputs })
      .then((resolution) => {
        if (!mountedRef.current) return;
        setResolved(resolution.resolved);
        setTruth(
          resolution.truth
            ? {
                level: resolution.truth.level,
                freshness: resolution.truth.freshness.state,
                capability: resolution.truth.capability,
              }
            : null,
        );
        setPhase("resolved");
      })
      .catch((runError: unknown) => {
        if (!mountedRef.current) return;
        setError(runError instanceof Error ? runError.message : "failed to resolve playbook");
        setPhase("error");
      });
  }

  return (
    <div className="mt">
      <form aria-label={`Run ${playbook.name}`} className="panel-action-stack" onSubmit={run}>
        {playbook.requiredInputs.map((input) => (
          <label key={input.name}>
            <span>
              {input.name}
              {input.required ? " *" : ""}
            </span>{" "}
            <input
              aria-label={input.name}
              className="popover-input mono"
              onChange={(event) =>
                setValues((current) => ({ ...current, [input.name]: event.target.value }))
              }
              placeholder={input.description}
              value={values[input.name] ?? ""}
            />
          </label>
        ))}
        <button className="btn-ghost active" disabled={phase === "resolving"} type="submit">
          {phase === "resolving" ? "Resolving…" : "Resolve"}
        </button>
      </form>

      {validationError ? <p className="src-err">{validationError}</p> : null}

      {phase === "error" ? (
        <div className="mt">
          <p className="src-err">{error}</p>
          <button className="btn-ghost" onClick={() => setPhase("idle")} type="button">
            Try again
          </button>
        </div>
      ) : null}

      {phase === "resolved" && resolved ? (
        <ResolvedPlaybookView
          failureModes={playbook.failureModes}
          headingRef={headingRef}
          resolved={resolved}
          truth={truth}
        />
      ) : null}
    </div>
  );
}

function ResolvedPlaybookView({
  failureModes,
  headingRef,
  resolved,
  truth,
}: {
  readonly failureModes: readonly PlaybookFailureMode[];
  readonly headingRef: React.RefObject<HTMLHeadingElement | null>;
  readonly resolved: ResolvedPlaybook;
  readonly truth: { level: string; freshness: string; capability: string } | null;
}): React.JSX.Element {
  return (
    <Panel className="mt">
      <div className="panel-action-stack">
        <h3 ref={headingRef} tabIndex={-1}>
          Resolved plan
        </h3>
        {truth ? (
          <div className="panel-action-stack">
            <span className="mono">{truth.capability}</span>
            <TruthChip level={uiTruth(truth.level)} />
            <FreshDot state={uiFresh(truth.freshness)} />
          </div>
        ) : null}
      </div>

      <ol aria-label="Resolved bounded calls" className="evidence-card-list mt">
        {resolved.calls.map((call) => (
          <ResolvedCallItem call={call} key={call.stepId} />
        ))}
      </ol>

      {failureModes.length > 0 ? (
        <dl className="surface-dl mt">
          {failureModes.map((mode) => (
            <div key={mode.condition}>
              <dt>{mode.condition}</dt>
              <dd>
                {mode.meaning} -- fallback: <span className="mono">{mode.fallback}</span>
              </dd>
            </div>
          ))}
        </dl>
      ) : null}
    </Panel>
  );
}

function ResolvedCallItem({ call }: { readonly call: ResolvedCall }): React.JSX.Element {
  const argEntries = Object.entries(call.arguments);
  return (
    <li className="evidence-card">
      <div className="panel-action-stack">
        <span className="mono">{call.tool}</span>
        <Badge tone="teal">{call.expectedTruth}</Badge>
      </div>
      <p>{call.evidenceExpected}</p>
      {argEntries.length > 0 ? (
        <p className="mono">
          {argEntries.map(([key, value]) => `${key}=${String(value)}`).join(", ")}
        </p>
      ) : null}
      {call.drilldowns.length > 0 ? (
        <p>
          Drilldowns:{" "}
          {call.drilldowns.map((drilldown) => (
            <span className="mono" key={drilldown.tool} title={drilldown.reason}>
              {drilldown.tool}{" "}
            </span>
          ))}
        </p>
      ) : null}
    </li>
  );
}
