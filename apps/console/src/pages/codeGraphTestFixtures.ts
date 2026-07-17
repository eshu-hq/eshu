import type { RepoListItem } from "../api/repoCatalog";

export function codeGraphRepository(id: string, name: string): RepoListItem {
  return {
    groupKey: "source",
    groupKind: "source",
    groupReason: "fixture",
    groupSource: "fixture",
    groupTruth: "exact",
    id,
    isDependency: false,
    name,
    remoteUrl: "",
    repoSlug: `platform/${name}`,
  };
}
