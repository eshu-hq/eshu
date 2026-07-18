import { useId, useMemo, useState } from "react";

import {
  filterExposureServiceOptions,
  type ExposureServiceOption,
} from "../api/exposureServiceSelection";

const MAX_VISIBLE_OPTIONS = 20;

export function ExposureServiceSelector({
  busy,
  onChoose,
  onValueChange,
  options,
  value,
}: {
  readonly busy: boolean;
  readonly onChoose: (option: ExposureServiceOption) => void;
  readonly onValueChange: (value: string) => void;
  readonly options: readonly ExposureServiceOption[];
  readonly value: string;
}): React.JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const listboxID = useId();
  const helpID = useId();
  const matches = useMemo(() => filterExposureServiceOptions(options, value), [options, value]);
  const visible = matches.slice(0, MAX_VISIBLE_OPTIONS);

  return (
    <div className="exposure-selector">
      <label className="exposure-entry-field">
        <span>Service</span>
        <input
          aria-autocomplete="list"
          aria-controls={listboxID}
          aria-describedby={helpID}
          aria-expanded={expanded && visible.length > 0}
          aria-label="Service selection"
          autoComplete="off"
          className="popover-input"
          disabled={busy}
          onChange={(event) => {
            onValueChange(event.currentTarget.value);
            setExpanded(true);
          }}
          onFocus={() => setExpanded(true)}
          placeholder="Search authorized services or paste workload:…"
          role="combobox"
          value={value}
        />
      </label>
      {expanded && visible.length > 0 ? (
        <div
          aria-label="Authorized services"
          className="exposure-selector-options"
          id={listboxID}
          role="listbox"
        >
          {visible.map((option) => (
            <button
              aria-selected={false}
              className="exposure-selector-option"
              key={option.canonicalId}
              onClick={() => {
                onChoose(option);
                setExpanded(false);
              }}
              role="option"
              type="button"
            >
              <strong>{option.displayName}</strong>
              <span className="mono">{option.canonicalId}</span>
              {option.repoName ? <small>{option.repoName}</small> : null}
            </button>
          ))}
          {matches.length > visible.length ? (
            <p className="exposure-selector-more">
              {matches.length - visible.length} more matches — keep typing to narrow the list.
            </p>
          ) : null}
        </div>
      ) : null}
      <p className="exposure-selector-help" id={helpID}>
        This route-local selector controls Exposure Path. Global search opens a service spotlight;
        use its <strong>Trace exposure</strong> action to pivot here explicitly.
      </p>
    </div>
  );
}
