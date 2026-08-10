// Seed-selection policy for the private-mode connect lifecycle.
//
// The Data source popover reconnects without a credential (#3685 removed the
// key-paste field), so App.connect has to decide on its own whether the
// build-time VITE_ESHU_API_KEY seed may stand in. That decision is security
// relevant: bootFromKey sends the seed as a bearer token to whichever base it
// is handed, so a seed must never follow an operator-typed origin.
import { defaultApiBaseUrl, type ConsoleEnvironment } from "./config/environment";

// sameApiBase compares two console API bases for seed-trust purposes. Bases are
// compared after trimming surrounding whitespace and any trailing slashes, so
// "/eshu-api" and "/eshu-api/" are the same deployment. Everything else,
// including a differing scheme, host, or port, is a different origin.
export function sameApiBase(left: string, right: string): boolean {
  const normalize = (value: string): string => value.trim().replace(/\/+$/, "");
  const normalizedLeft = normalize(left);
  return normalizedLeft.length > 0 && normalizedLeft === normalize(right);
}

// seedRetryKey returns the build-time key when a credential-less connect
// attempt against this build's own base found no browser session, and an empty
// string in every other case.
//
// The trust anchor is defaultApiBaseUrl, NOT environment.apiBaseUrl. The saved
// base is operator-influenced: bootFromKey persists whatever base was
// attempted even when the attempt fails, so typing a hostile origin into the
// Data source popover leaves that origin saved. Anchoring on the saved value
// would send the seed there on the next load, with no further user action.
export function seedRetryKey(
  attempt: { readonly base: string; readonly key: string },
  environment: Pick<ConsoleEnvironment, "apiKey">,
): string {
  if (attempt.key.trim().length > 0) return "";
  const seed = environment.apiKey.trim();
  if (seed.length === 0) return "";
  if (!sameApiBase(attempt.base, defaultApiBaseUrl)) return "";
  return seed;
}
