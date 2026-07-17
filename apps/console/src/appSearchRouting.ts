export function repositorySearchDestination(pathname: string, repositoryId: string): string {
  const encoded = encodeURIComponent(repositoryId);
  if (pathname === "/code-graph") return `/code-graph?repo_id=${encoded}`;
  return `/repositories/${encoded}/source`;
}
