import {
  type ChangedSincePageData,
  type ChangeClassification,
  type ChangedSinceCategory,
  type GenerationLifecyclePage,
} from "../api/changedSince";
import { Badge } from "../components/atoms";
import { fmt } from "../console/types";

const classifications: readonly ChangeClassification[] = [
  "added",
  "updated",
  "retired",
  "superseded",
  "unchanged",
];

export function FilterInput({
  label,
  onChange,
  value,
}: {
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly value: string;
}): React.JSX.Element {
  return (
    <label>
      <span>{label}</span>
      <input
        aria-label={label}
        className="popover-input mono"
        onChange={(event) => onChange(event.target.value)}
        value={value}
      />
    </label>
  );
}

export function ChangedSinceCategoryRows({
  categories,
  repositoryId,
}: {
  readonly categories: readonly ChangedSinceCategory[];
  readonly repositoryId?: string;
}): React.JSX.Element {
  return (
    <>
      {categories.map((category) => (
        <CategoryRow category={category} key={category.category} repositoryId={repositoryId} />
      ))}
      {categories.length === 0 ? (
        <tr>
          <td colSpan={3} className="empty">
            No delta categories returned for this retained window.
          </td>
        </tr>
      ) : null}
    </>
  );
}

export function GenerationLifecycleRows({
  generations,
}: {
  readonly generations: GenerationLifecyclePage;
}): React.JSX.Element {
  return (
    <>
      {generations.generations.map((generation) => (
        <tr key={generation.generationId}>
          <td className="cell-stack">
            <span className="mono">{generation.generationId}</span>
            <small>
              {generation.sourceSystem || "source"} / {generation.collectorKind || "collector"}
            </small>
          </td>
          <td>
            {generation.isActive ? (
              <Badge tone="teal">active</Badge>
            ) : (
              <Badge>{generation.status || "unknown"}</Badge>
            )}
          </td>
          <td className="mono">{generation.observedAt ?? "-"}</td>
          <td>{fmt(generation.queueOutstanding)} outstanding</td>
        </tr>
      ))}
      {generations.generations.length === 0 ? (
        <tr>
          <td colSpan={4} className="empty">
            No generation lifecycle rows returned.
          </td>
        </tr>
      ) : null}
    </>
  );
}

export function generationPair(page: ChangedSincePageData): string {
  const since = page.sinceGenerationId || page.sinceObservedAt || "baseline";
  const current = page.currentActiveGenerationId || page.currentObservedAt || "current";
  return `${since} -> ${current}`;
}

export function impactLink(page: ChangedSincePageData): string {
  const params = new URLSearchParams();
  params.set("kind", page.mode === "service" ? "service" : "repository");
  params.set("target", page.scopeLabel || page.scopeId);
  return `/impact?${params.toString()}`;
}

export function sampleTotal(category: ChangedSinceCategory): number {
  return classifications.reduce(
    (sum, classification) => sum + category.samples[classification].length,
    0,
  );
}

function CategoryRow({
  category,
  repositoryId = "",
}: {
  readonly category: ChangedSinceCategory;
  readonly repositoryId?: string;
}): React.JSX.Element {
  return (
    <tr>
      <td className="cell-stack">
        <strong>{category.category || "unknown"}</strong>
        <small>{fmt(category.changedCount)} changed</small>
      </td>
      <td className="changed-since-counts">
        {classifications.map((classification) => (
          <span key={classification}>
            {classification} <b>{fmt(category.counts[classification])}</b>
          </span>
        ))}
      </td>
      <td>
        <div className="changed-since-samples">
          {classifications.flatMap((classification) =>
            category.samples[classification].map((sample) => (
              <span key={`${classification}:${sample.stableFactKey}:${sample.factKind}`}>
                <Badge
                  tone={
                    classification === "retired" || classification === "superseded"
                      ? "warn"
                      : "neutral"
                  }
                >
                  {classification}
                </Badge>
                <SampleIdentity
                  factKind={sample.factKind}
                  repositoryId={repositoryId}
                  stableFactKey={sample.stableFactKey}
                />
                <small>{sample.factKind || "fact"}</small>
                {category.truncated[classification] ? <em>truncated</em> : null}
              </span>
            )),
          )}
          {sampleTotal(category) === 0 ? <span className="t-mut">no samples</span> : null}
        </div>
      </td>
    </tr>
  );
}

function SampleIdentity({
  factKind,
  repositoryId,
  stableFactKey,
}: {
  readonly factKind: string;
  readonly repositoryId: string;
  readonly stableFactKey: string;
}): React.JSX.Element {
  const prefix = `file:${repositoryId}:`;
  if (factKind === "file" && repositoryId !== "" && stableFactKey.startsWith(prefix)) {
    return (
      <details className="changed-since-sample-identity">
        <summary className="mono">{stableFactKey.slice(prefix.length) || "-"}</summary>
        <code>{stableFactKey}</code>
      </details>
    );
  }
  return <span className="mono">{stableFactKey || "-"}</span>;
}
