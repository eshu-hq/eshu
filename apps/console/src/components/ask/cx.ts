// cx.ts — minimal class-name joiner for the Ask Eshu components. Falsy values
// (false, null, undefined, "") are dropped so callers can write
// cx("base", active && "is-active") without leaking "false" into the DOM.
export function cx(...values: ReadonlyArray<string | false | null | undefined>): string {
  return values.filter((value): value is string => typeof value === "string" && value.length > 0).join(" ");
}
