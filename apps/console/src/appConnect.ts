// Private-mode connect lifecycle, lifted out of App.tsx to keep that file
// clear of the 500-line ceiling and to give the seed-retry policy a home next
// to the state transitions it drives.
import type { BrowserSessionResponse, EshuApiClient } from "./api/client";
import { bootFromKey } from "./appBoot";
import { seedRetryKey } from "./appConnectSeed";
import type { SourceState } from "./components/SourceControls";
import type { ConsoleEnvironment } from "./config/environment";
import { emptyConsoleModel } from "./console/liveModel";
import type { ConsoleModel } from "./console/types";
import type { RepositoryCatalogState } from "./repositoryCatalogLifecycle";

// ConnectDeps is the slice of AppShell state the connect lifecycle drives.
export interface ConnectDeps {
  readonly environment: Pick<ConsoleEnvironment, "apiBaseUrl" | "apiKey">;
  readonly unreachableMessage: string;
  readonly setSource: (update: SourceState | ((previous: SourceState) => SourceState)) => void;
  readonly setModel: (model: ConsoleModel) => void;
  readonly setClient: (client: EshuApiClient | undefined) => void;
  readonly setSession: (session: BrowserSessionResponse | null) => void;
  readonly setOpen: (open: boolean) => void;
  readonly clearRepositoryCatalog: () => void;
  readonly activateRepositoryCatalog: (
    client: EshuApiClient,
    catalog: Promise<RepositoryCatalogState>,
  ) => void;
}

// createConnect builds the private-mode connect handler used by both the
// saved-environment boot effect and the Data source popover.
//
// When a credential-less attempt against the configured base finds no browser
// session, it retries once with the build-time seed. The retry cannot recurse:
// the second call carries a non-empty key, and seedRetryKey returns "" for any
// attempt that already has one.
export function createConnect(deps: ConnectDeps): (base: string, key: string) => Promise<void> {
  async function connect(base: string, key: string): Promise<void> {
    deps.setSource((s) => ({ ...s, base, key, mode: "private", status: "connecting", msg: "" }));
    deps.setModel(emptyConsoleModel("loading"));
    try {
      const result = await bootFromKey(base, key);
      if (result === null) {
        const seededKey = seedRetryKey({ base, key }, deps.environment);
        if (seededKey.length > 0) {
          await connect(base, seededKey);
          return;
        }
        deps.setClient(undefined);
        deps.clearRepositoryCatalog();
        deps.setSession(null);
        deps.setModel(emptyConsoleModel("unavailable"));
        deps.setSource({ base, key: "", mode: "private", status: "needs-connection", msg: "" });
        deps.setOpen(false);
        return;
      }
      deps.setClient(result.client);
      deps.setModel(result.model);
      deps.activateRepositoryCatalog(result.client, result.repositoryCatalog);
      deps.setSession(result.session);
      deps.setSource({
        base,
        key: result.session === null ? key : "",
        mode: "private",
        status: "connected",
        msg: "",
      });
      deps.setOpen(false);
    } catch (e) {
      deps.setClient(undefined);
      deps.clearRepositoryCatalog();
      deps.setSession(null);
      deps.setModel(emptyConsoleModel("unavailable"));
      deps.setSource({
        base,
        key,
        mode: "private",
        status: "error",
        msg: e instanceof Error ? e.message : deps.unreachableMessage,
      });
    }
  }
  return connect;
}
