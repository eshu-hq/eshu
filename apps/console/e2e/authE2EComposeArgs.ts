/** Return the exact Compose file stack used by the auth E2E shell runner. */
export function authE2EComposeArgs(
  project: string,
  overlay: string | undefined = process.env.ESHU_CI_COMPOSE_OVERLAY,
): string[] {
  const args = ["compose", "-p", project, "-f", "docker-compose.e2e.yaml"];
  if (overlay) {
    args.push("-f", overlay);
  }
  return args;
}
