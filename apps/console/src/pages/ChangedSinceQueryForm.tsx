import type { Dispatch, SetStateAction } from "react";

import { ChangedSinceRepositorySelector } from "./ChangedSinceRepositorySelector";
import { FilterInput } from "./ChangedSincePresentation";
import { isBoundedChangedSince, type ChangedSinceFormState } from "./changedSinceQuery";
import type { ChangedSinceMode } from "../api/changedSince";
import type { RepoListItem } from "../api/repoCatalog";

export function ChangedSinceQueryForm({
  busy,
  form,
  hasLiveClient,
  onChange,
  onSelectRepository,
  onSubmit,
  repositories,
  selectedRepositoryId,
}: {
  readonly busy: boolean;
  readonly form: ChangedSinceFormState;
  readonly hasLiveClient: boolean;
  readonly onChange: Dispatch<SetStateAction<ChangedSinceFormState>>;
  readonly onSelectRepository: (repositoryId: string) => void;
  readonly onSubmit: () => void;
  readonly repositories: readonly RepoListItem[];
  readonly selectedRepositoryId: string;
}): React.JSX.Element {
  return (
    <form
      className="changed-since-query"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit();
      }}
    >
      <label>
        <span>Mode</span>
        <select
          aria-label="Mode"
          className="popover-input"
          value={form.mode}
          onChange={(event) =>
            onChange((current) => ({ ...current, mode: event.target.value as ChangedSinceMode }))
          }
        >
          <option value="repository">Repository</option>
          <option value="service">Service</option>
        </select>
      </label>
      {form.mode === "service" ? (
        <FilterInput
          label="Service ID"
          value={form.serviceId}
          onChange={(value) => onChange((current) => ({ ...current, serviceId: value }))}
        />
      ) : (
        <ChangedSinceRepositorySelector
          onChange={onSelectRepository}
          repositories={repositories}
          selectedRepositoryId={selectedRepositoryId}
        />
      )}
      <FilterInput
        label="Since generation"
        value={form.sinceGenerationId}
        onChange={(value) => onChange((current) => ({ ...current, sinceGenerationId: value }))}
      />
      {form.mode === "repository" ? (
        <FilterInput
          label="Since observed at"
          value={form.sinceObservedAt}
          onChange={(value) => onChange((current) => ({ ...current, sinceObservedAt: value }))}
        />
      ) : null}
      <FilterInput
        label="Sample limit"
        value={form.sampleLimit}
        onChange={(value) => onChange((current) => ({ ...current, sampleLimit: value }))}
      />
      <button
        className="btn-ghost active"
        disabled={!hasLiveClient || busy || !isBoundedChangedSince(form)}
        type="submit"
      >
        {busy ? "Loading..." : "Load changes"}
      </button>
    </form>
  );
}
