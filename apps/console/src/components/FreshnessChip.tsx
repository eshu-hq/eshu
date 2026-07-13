// components/FreshnessChip.tsx
// Verdict chip for per-repository commit-receipt freshness (issue #5143):
// renders {tone, headline} from api/repositoryFreshness.ts on the Badge atom.
// The title tooltip carries the fuller detail copy plus the full (untruncated)
// observed commit SHA, so a shortened headline never hides the real value
// from an operator who wants to copy it.
import { Badge } from "./atoms";
import type { RepositoryFreshness } from "../api/repositoryFreshness";

export function FreshnessChip({
  freshness,
}: {
  readonly freshness: RepositoryFreshness;
}): React.JSX.Element {
  const { copy, observedCommit } = freshness;
  const title = observedCommit ? `${copy.detail} (${observedCommit})` : copy.detail;
  return (
    <span title={title}>
      <Badge tone={copy.tone} dot>
        {copy.headline}
      </Badge>
    </span>
  );
}
