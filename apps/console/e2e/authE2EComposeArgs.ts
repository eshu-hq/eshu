/**
 * Return the exact Compose file stack used by the auth E2E shell runner.
 * The e2e Compose file moved to eshu-deploy under #8871: when
 * ESHU_DEPLOY_CHECKOUT points at that checkout the stack resolves inside
 * it, otherwise the legacy repo-root-relative name is returned.
 */
export function authE2EComposeArgs(
  project: string,
  overlay: string | undefined = process.env.ESHU_CI_COMPOSE_OVERLAY,
): string[] {
  const deployCheckout = process.env.ESHU_DEPLOY_CHECKOUT;
  const composeFile =
    deployCheckout !== undefined && deployCheckout !== ""
      ? `${deployCheckout}/compose/docker-compose.e2e.yaml`
      : "docker-compose.e2e.yaml";
  const args = ["compose", "-p", project, "-f", composeFile];
  if (overlay) {
    args.push("-f", overlay);
  }
  return args;
}
