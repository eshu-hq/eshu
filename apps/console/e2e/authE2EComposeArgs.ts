/**
 * Return the exact Compose file stack used by the auth E2E shell runner.
 * The e2e Compose file moved to eshu-deploy under #8871: the stack resolves
 * inside the ESHU_DEPLOY_CHECKOUT checkout, and the helper fails closed when
 * it is unset rather than returning the deleted in-tree file name.
 */
export function authE2EComposeArgs(
  project: string,
  overlay: string | undefined = process.env.ESHU_CI_COMPOSE_OVERLAY,
): string[] {
  const deployCheckout = process.env.ESHU_DEPLOY_CHECKOUT;
  if (deployCheckout === undefined || deployCheckout === "") {
    throw new Error(
      "ESHU_DEPLOY_CHECKOUT must point at an eshu-deploy checkout (the e2e Compose file moved there under #8871)",
    );
  }
  const composeFile = `${deployCheckout}/compose/docker-compose.e2e.yaml`;
  const args = ["compose", "-p", project, "-f", composeFile];
  if (overlay) {
    args.push("-f", overlay);
  }
  return args;
}
