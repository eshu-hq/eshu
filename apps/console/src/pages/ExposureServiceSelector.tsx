import { useId, useMemo, useState } from "react";

import {
  filterExposureServiceOptions,
  type ExposureServiceOption,
} from "../api/exposureServiceSelection";

const MAX_VISIBLE_OPTIONS = 20;

export function ExposureServiceSelector({
  busy,
  catalogTruncated,
  onChoose,
  onValueChange,
  options,
  value,
}: {
  readonly busy: boolean;
  readonly catalogTruncated: boolean;
  readonly onChoose: (option: ExposureServiceOption) => void;
  readonly onValueChange: (value: string) => void;
  readonly options: readonly ExposureServiceOption[];
  readonly value: string;
}): React.JSX.Element {
  const [expanded, setExpanded] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  const listboxID = useId();
  const helpID = useId();
  const matches = useMemo(() => filterExposureServiceOptions(options, value), [options, value]);
  const visible = matches.slice(0, MAX_VISIBLE_OPTIONS);
  const selectedIndex = activeIndex >= 0 && activeIndex < visible.length ? activeIndex : -1;
  const activeOptionID = selectedIndex >= 0 ? `${listboxID}-option-${selectedIndex}` : undefined;

  function choose(option: ExposureServiceOption): void {
    onChoose(option);
    setActiveIndex(-1);
    setExpanded(false);
  }

  return (
    <div
      className="exposure-selector"
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) {
          setActiveIndex(-1);
          setExpanded(false);
        }
      }}
    >
      <label className="exposure-entry-field">
        <span>Service</span>
        <input
          aria-autocomplete="list"
          aria-activedescendant={expanded ? activeOptionID : undefined}
          aria-busy={busy}
          aria-controls={listboxID}
          aria-describedby={helpID}
          aria-expanded={expanded && visible.length > 0}
          aria-label="Service selection"
          autoComplete="off"
          className="popover-input"
          onChange={(event) => {
            onValueChange(event.currentTarget.value);
            setActiveIndex(-1);
            setExpanded(true);
          }}
          onFocus={() => setExpanded(true)}
          onKeyDown={(event) => {
            if (event.key === "ArrowDown" && visible.length > 0) {
              event.preventDefault();
              setExpanded(true);
              setActiveIndex((current) => (current + 1) % visible.length);
              return;
            }
            if (event.key === "ArrowUp" && visible.length > 0) {
              event.preventDefault();
              setExpanded(true);
              setActiveIndex((current) => (current <= 0 ? visible.length - 1 : current - 1));
              return;
            }
            if (event.key === "Enter" && expanded && selectedIndex >= 0) {
              event.preventDefault();
              const option = visible[selectedIndex];
              if (option) choose(option);
              return;
            }
            if (event.key === "Escape" && expanded) {
              event.preventDefault();
              setActiveIndex(-1);
              setExpanded(false);
            }
          }}
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
          {visible.map((option, index) => (
            <button
              aria-selected={selectedIndex === index}
              className="exposure-selector-option"
              id={`${listboxID}-option-${index}`}
              key={option.canonicalId}
              onClick={() => choose(option)}
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
      {catalogTruncated ? (
        <p className="exposure-selector-help exposure-selector-bounded" role="status">
          The visible service list is bounded. The submit-time resolver searches beyond it and
          refuses to guess when more candidates may exist.
        </p>
      ) : null}
    </div>
  );
}
