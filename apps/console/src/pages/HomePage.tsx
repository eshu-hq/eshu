import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { EshuApiClient } from "../api/client";
import { loadSearchCandidates } from "../api/liveData";
import type { SearchCandidate } from "../api/mockData";
import { SearchCommand } from "../components/SearchCommand";
import { loadConsoleEnvironment } from "../config/environment";

export function HomePage(): React.JSX.Element {
  const navigate = useNavigate();
  const [candidates, setCandidates] = useState<readonly SearchCandidate[]>([]);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "unavailable">(
    "loading"
  );

  useEffect(() => {
    const environment = loadConsoleEnvironment();
    const client =
      environment.mode === "private"
        ? new EshuApiClient({ baseUrl: environment.apiBaseUrl })
        : undefined;
    void loadSearchCandidates({ client, mode: environment.mode })
      .then((loadedCandidates) => {
        setCandidates(loadedCandidates);
        setLoadState("ready");
      })
      .catch(() => {
        setCandidates([]);
        setLoadState("unavailable");
      });
  }, []);

  return (
    <section className="home-page">
      <p className="eyebrow">Eshu Console</p>
      <h1>Ask or search your engineering estate</h1>
      <p>
        Search for a repository, service, or workload to open a read-only
        workspace with story, evidence, deployment, code, findings, and
        freshness.
      </p>
      {loadState === "unavailable" ? (
        <p className="inline-state">Local Eshu API unavailable.</p>
      ) : null}
      <SearchCommand
        candidates={candidates}
        onSelect={(candidate) =>
          navigate(`/workspace/${candidate.kind}/${encodeURIComponent(candidate.id)}`)
        }
      />
    </section>
  );
}
