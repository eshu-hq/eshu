import type { DriftFilters } from "./CloudDriftQuery";

export function CloudDriftToolbar({
  busy,
  draft,
  onDraft,
  onReset,
  onSubmit,
}: {
  readonly busy: boolean;
  readonly draft: DriftFilters;
  readonly onDraft: (filters: DriftFilters) => void;
  readonly onReset: () => void;
  readonly onSubmit: () => void;
}): React.JSX.Element {
  return (
    <form
      className="evidence-toolbar"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <select
        aria-label="Provider filter"
        className="popover-input"
        value={draft.provider}
        onChange={(event) =>
          onDraft({
            ...draft,
            provider: event.target.value as DriftFilters["provider"],
          })
        }
      >
        <option value="">Provider</option>
        <option value="aws">AWS</option>
        <option value="gcp">GCP</option>
        <option value="azure">Azure</option>
      </select>
      <input
        aria-label="Account ID filter"
        className="popover-input mono"
        placeholder="account_id"
        value={draft.accountId}
        onChange={(event) => onDraft({ ...draft, accountId: event.target.value })}
      />
      <input
        aria-label="Region filter"
        className="popover-input mono"
        placeholder="region"
        value={draft.region}
        onChange={(event) => onDraft({ ...draft, region: event.target.value })}
      />
      <input
        aria-label="Scope ID filter"
        className="popover-input mono"
        placeholder="scope_id"
        value={draft.scopeId}
        onChange={(event) => onDraft({ ...draft, scopeId: event.target.value })}
      />
      <button className="btn-ghost active" disabled={busy} type="submit">
        Load drift findings
      </button>
      <button className="btn-ghost" type="button" onClick={onReset}>
        Reset
      </button>
    </form>
  );
}
