import type { FormEvent } from "react";

import type { ReplatformingSelectorInventory } from "../api/replatformingSelectors";
import type { ReplatformingScopeKind } from "../api/replatforming";

export interface ReplatformingFormState {
  readonly accountId: string;
  readonly findingKinds: string;
  readonly limit: string;
  readonly offset: string;
  readonly region: string;
  readonly scopeId: string;
  readonly scopeKind: ReplatformingScopeKind;
}

interface ReplatformingFiltersProps {
  readonly canLoad: boolean;
  readonly form: ReplatformingFormState;
  readonly inventory: ReplatformingSelectorInventory | null;
  readonly onChange: (form: ReplatformingFormState) => void;
  readonly onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}

const defaultScopeKinds = ["account", "region", "service"] as const;
const defaultPageSizes = [25, 50, 100, 200] as const;

export function ReplatformingFilters({
  canLoad,
  form,
  inventory,
  onChange,
  onSubmit,
}: ReplatformingFiltersProps): React.JSX.Element {
  const scopeKinds = inventory?.supportedScopeKinds.length
    ? inventory.supportedScopeKinds
    : defaultScopeKinds;
  const pageSizes = inventory?.pageSizes.length ? inventory.pageSizes : defaultPageSizes;
  const accounts = unique(inventory?.scopes.map((scope) => scope.accountId) ?? []);
  const regions = unique(
    (inventory?.scopes ?? [])
      .filter((scope) => form.accountId === "" || scope.accountId === form.accountId)
      .map((scope) => scope.region),
  );

  function change(patch: Partial<ReplatformingFormState>): void {
    onChange({ ...form, ...patch });
  }

  return (
    <form className="replatforming-query" onSubmit={onSubmit}>
      <label>
        <span>Scope kind</span>
        <select
          aria-label="Scope kind"
          className="popover-input"
          value={form.scopeKind}
          onChange={(event) =>
            change({
              accountId: "",
              offset: "0",
              region: "",
              scopeId: "",
              scopeKind: event.target.value as ReplatformingScopeKind,
            })
          }
        >
          {scopeKinds.map((kind) => (
            <option key={kind} value={kind}>
              {formatLabel(kind)}
            </option>
          ))}
        </select>
      </label>

      <label>
        <span>Account</span>
        <input
          aria-label="Account"
          className="popover-input mono"
          list="replatforming-accounts"
          onChange={(event) =>
            change({ accountId: event.target.value, offset: "0", region: "", scopeId: "" })
          }
          placeholder="Choose an AWS account"
          role="combobox"
          value={form.accountId}
        />
        <datalist id="replatforming-accounts">
          {accounts.map((account) => (
            <option key={account} value={account} />
          ))}
        </datalist>
      </label>

      <label>
        <span>Region</span>
        <input
          aria-label="Region"
          className="popover-input mono"
          list="replatforming-regions"
          onChange={(event) => change({ offset: "0", region: event.target.value, scopeId: "" })}
          placeholder="All regions"
          role="combobox"
          value={form.region}
        />
        <datalist id="replatforming-regions">
          {regions.map((region) => (
            <option key={region} value={region} />
          ))}
        </datalist>
      </label>

      {form.scopeKind === "service" ? (
        <label>
          <span>Source scope</span>
          <input
            aria-label="Source scope"
            className="popover-input mono"
            list="replatforming-source-scopes"
            onChange={(event) => change({ offset: "0", scopeId: event.target.value })}
            placeholder="Choose a collector scope"
            role="combobox"
            value={form.scopeId}
          />
          <datalist id="replatforming-source-scopes">
            {(inventory?.scopes ?? []).map((scope) => (
              <option
                key={scope.scopeId}
                label={`${scope.label} — ${scope.findingCount} findings`}
                value={scope.scopeId}
              />
            ))}
          </datalist>
        </label>
      ) : null}

      <label>
        <span>Finding kinds</span>
        <select
          aria-label="Finding kinds"
          className="popover-input"
          multiple
          onChange={(event) =>
            change({
              findingKinds: Array.from(
                event.currentTarget.selectedOptions,
                (option) => option.value,
              ).join(","),
              offset: "0",
            })
          }
          value={splitKinds(form.findingKinds)}
        >
          {(inventory?.findingKinds ?? []).map((kind) => (
            <option key={kind} value={kind}>
              {formatLabel(kind)}
            </option>
          ))}
        </select>
      </label>

      <label>
        <span>Page size</span>
        <select
          aria-label="Page size"
          className="popover-input"
          onChange={(event) => change({ limit: event.target.value, offset: "0" })}
          value={form.limit}
        >
          {pageSizes.map((size) => (
            <option key={size} value={String(size)}>
              {size}
            </option>
          ))}
        </select>
      </label>

      <button className="btn-ghost active" disabled={!canLoad || inventory === null} type="submit">
        Review plan
      </button>
    </form>
  );
}

export function ReplatformingPagination({
  busy,
  canMoveNext,
  canMovePrevious,
  onNext,
  onPrevious,
}: {
  readonly busy: boolean;
  readonly canMoveNext: boolean;
  readonly canMovePrevious: boolean;
  readonly onNext: () => void;
  readonly onPrevious: () => void;
}): React.JSX.Element {
  return (
    <nav aria-label="Replatforming result pages" className="replatforming-pagination">
      <button
        className="btn-ghost"
        disabled={busy || !canMovePrevious}
        onClick={onPrevious}
        type="button"
      >
        Previous page
      </button>
      <button className="btn-ghost" disabled={busy || !canMoveNext} onClick={onNext} type="button">
        Next page
      </button>
    </nav>
  );
}

function splitKinds(value: string): readonly string[] {
  return value
    .split(",")
    .map((kind) => kind.trim())
    .filter(Boolean);
}

function unique(values: readonly string[]): readonly string[] {
  return [...new Set(values.filter((value) => value !== ""))];
}

function formatLabel(value: string): string {
  return value.replace(/_/g, " ");
}
