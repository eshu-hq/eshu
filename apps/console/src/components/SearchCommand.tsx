import { useMemo, useState } from "react";
import type { SearchCandidate } from "../api/mockData";

interface SearchCommandProps {
  readonly candidates: readonly SearchCandidate[];
  readonly onSelect: (candidate: SearchCandidate) => void;
}

export function SearchCommand({
  candidates,
  onSelect
}: SearchCommandProps): React.JSX.Element {
  const [query, setQuery] = useState("");
  const filteredCandidates = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase();
    if (normalizedQuery.length === 0) {
      return candidates;
    }
    return candidates.filter((candidate) =>
      `${candidate.label} ${candidate.description} ${candidate.id}`
        .toLowerCase()
        .includes(normalizedQuery)
    );
  }, [candidates, query]);

  return (
    <div className="command-search">
      <label>
        <span>Search Eshu</span>
        <input
          onChange={(event) => setQuery(event.currentTarget.value)}
          placeholder="Search repos, services, workloads"
          value={query}
        />
      </label>
      <div className="candidate-list">
        {filteredCandidates.map((candidate) => (
          <button
            key={`${candidate.kind}:${candidate.id}`}
            onClick={() => onSelect(candidate)}
            type="button"
          >
            <strong>{candidate.label}</strong>
            <span>{candidate.description}</span>
          </button>
        ))}
      </div>
    </div>
  );
}
