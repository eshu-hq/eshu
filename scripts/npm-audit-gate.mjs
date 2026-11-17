#!/usr/bin/env node
// npm-audit-gate.mjs - decision logic for scripts/verify-npm-audit.sh (#10854).
//
// Reads two `npm audit --json` reports (all dependencies, and --omit=dev) and
// the dated allowlist in specs/npm-audit-allowlist.v1.json, then fails closed.
// It fails when any of these holds:
//   - a high or critical advisory is reported that is not allowlisted by both
//     GHSA id and package;
//   - an allowlisted entry is past its `expires` date (UTC);
//   - an allowlisted advisory, or any high or critical advisory, appears in
//     the --omit=dev (production) report;
//   - the reported severity is above the one recorded for the entry;
//   - an entry is stale (its advisory is no longer reported);
//   - an entry is malformed, or expires - added is over 31 days;
//   - a report is missing, unparsable, an npm error object, or not report v2.
// Lines tagged `gate:<name>` are the mutation points of the self-test.
//
// Usage: node npm-audit-gate.mjs --all F --prod F --allowlist F [--now YYYY-MM-DD]
// --now is for the self-test only; the wrapper refuses it in live mode.
// Exit: 0 pass, 1 gate failure, 2 usage error.

import { readFileSync } from "node:fs";

const MAX_WINDOW_DAYS = 31;
const WARN_DAYS = 7;
const DAY_MS = 86_400_000;
const RANKS = { info: 0, low: 1, moderate: 2, high: 3, critical: 4 };
const ENTRY_KEYS = ["ghsa", "package", "severity", "reason", "issue", "added", "expires"];

const rank = (severity) => (severity in RANKS ? RANKS[severity] : RANKS.critical);
const blocking = (severity) => rank(severity) >= RANKS.high;
const out = (stream, line) => stream.write(`${line}\n`);

/** Parses a strict YYYY-MM-DD date to UTC epoch ms, or null when invalid. */
function parseDay(text) {
  if (typeof text !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(text)) return null;
  const ms = Date.parse(`${text}T00:00:00Z`);
  if (Number.isNaN(ms) || new Date(ms).toISOString().slice(0, 10) !== text) return null;
  return ms;
}

function readJson(path, label, problems) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (err) {
    problems.push(`${label}: cannot read or parse ${path}: ${err.message}`);
    return null;
  }
}

/**
 * Extracts advisories from an npm audit v2 report. A vulnerability's `via`
 * holds objects (the advisory itself) or strings (a transitive dependency
 * name that points at another vulnerability entry).
 */
function readReport(report, label, problems) {
  if (report === null) return null;
  if (typeof report !== "object" || Array.isArray(report)) {
    problems.push(`${label}: report is not a JSON object`);
    return null;
  }
  if (report.error) {
    const e = report.error;
    problems.push(
      `${label}: npm reported an error: ${e.code ?? "unknown"} ${e.summary ?? ""}`.trim(),
    );
    return null;
  }
  const vulns = report.vulnerabilities;
  if (
    report.auditReportVersion !== 2 ||
    vulns === null ||
    typeof vulns !== "object" ||
    Array.isArray(vulns)
  ) {
    problems.push(
      `${label}: not an npm audit report v2 (auditReportVersion/vulnerabilities missing)`,
    );
    return null;
  }
  const advisories = [];
  for (const [name, vuln] of Object.entries(vulns)) {
    for (const via of Array.isArray(vuln.via) ? vuln.via : []) {
      if (via === null || typeof via !== "object") continue;
      const url = typeof via.url === "string" ? via.url : "";
      const ghsa = /(GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4})$/i.exec(url);
      advisories.push({
        id: ghsa ? ghsa[1] : url || `source:${via.source}`,
        pkg: typeof via.name === "string" ? via.name : name,
        severity: String(via.severity),
      });
    }
  }
  // A high/critical node must trace back to at least one advisory object;
  // otherwise it is unattributed and the gate cannot vouch for it.
  const reaches = (name, seen) => {
    if (seen.has(name) || !vulns[name]) return false;
    seen.add(name);
    const via = Array.isArray(vulns[name].via) ? vulns[name].via : [];
    return via.some((v) =>
      typeof v === "string" ? reaches(v, seen) : v !== null && typeof v === "object",
    );
  };
  const unattributed = Object.entries(vulns)
    .filter(([name, v]) => blocking(String(v.severity)) && !reaches(name, new Set()))
    .map(([name]) => name);
  return { advisories, unattributed };
}

/** Validates the allowlist document; returns the usable entries. */
function readAllowlist(doc, problems) {
  if (doc === null) return [];
  if (
    typeof doc !== "object" ||
    Array.isArray(doc) ||
    doc.version !== 1 ||
    !Array.isArray(doc.entries) ||
    Object.keys(doc).length !== 2
  ) {
    problems.push('allowlist: must be {"version":1,"entries":[...]} with no other keys');
    return [];
  }
  const entries = [];
  const seen = new Set();
  doc.entries.forEach((entry, i) => {
    const where = `allowlist entry ${i}`;
    const keys = entry !== null && typeof entry === "object" ? Object.keys(entry).sort() : [];
    if (JSON.stringify(keys) !== JSON.stringify([...ENTRY_KEYS].sort())) {
      problems.push(`${where}: keys must be exactly ${ENTRY_KEYS.join(", ")}`);
      return;
    }
    const bad = ENTRY_KEYS.filter((k) => typeof entry[k] !== "string" || entry[k].trim() === "");
    const added = parseDay(entry.added);
    const expires = parseDay(entry.expires);
    if (bad.length > 0) problems.push(`${where}: empty or non-string field(s): ${bad.join(", ")}`);
    else if (!/^GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}$/i.test(entry.ghsa))
      problems.push(`${where}: ghsa is not a GHSA id`);
    else if (!/^#\d+$/.test(entry.issue)) problems.push(`${where}: issue must look like #1234`);
    else if (!blocking(entry.severity) || !(entry.severity in RANKS))
      problems.push(`${where}: severity must be high or critical`);
    else if (added === null || expires === null)
      problems.push(`${where}: added/expires must be valid YYYY-MM-DD dates`);
    else if (expires <= added) problems.push(`${where}: expires must be after added`);
    else if ((expires - added) / DAY_MS > MAX_WINDOW_DAYS)
      problems.push(`${where}: expires - added is over ${MAX_WINDOW_DAYS} days`);
    else if (seen.has(`${entry.ghsa}|${entry.package}`))
      problems.push(`${where}: duplicate ${entry.ghsa} for ${entry.package}`);
    else {
      seen.add(`${entry.ghsa}|${entry.package}`);
      entries.push({ ...entry, expiresMs: expires });
    }
  });
  return entries;
}

function checkExpiry(entries, todayMs, problems, warnings) {
  for (const e of entries) {
    const left = Math.round((e.expiresMs - todayMs) / DAY_MS);
    if (left <= 0) {
      problems.push(
        `${e.ghsa} (${e.package}): allowlist entry expired on ${e.expires}; re-check for a patched ${e.package} and renew in a reviewed PR`,
      );
    } else if (left <= WARN_DAYS) {
      warnings.push(
        `${e.ghsa} (${e.package}) allowlist entry expires on ${e.expires} (${left} day(s) left); this check turns red then (${e.issue})`,
      );
    }
  }
}

function checkProdClean(prod, entries, problems) {
  for (const a of prod.advisories) {
    const listed = entries.some((e) => e.ghsa === a.id && e.package === a.pkg);
    if (listed)
      problems.push(
        `${a.id} (${a.pkg}) is allowlisted for dev tooling but appears in the --omit=dev report`,
      );
    else if (blocking(a.severity))
      problems.push(
        `${a.id} (${a.pkg}, ${a.severity}) appears in the --omit=dev (production) report`,
      );
  }
  for (const name of prod.unattributed)
    problems.push(`production report: ${name} is high/critical with no traceable advisory`);
}

function checkAll(all, entries, problems) {
  for (const a of all.advisories) {
    if (!blocking(a.severity)) continue;
    const e = entries.find((x) => x.ghsa === a.id && x.package === a.pkg);
    if (!e) problems.push(`${a.id} (${a.pkg}, ${a.severity}) is not allowlisted`);
    else if (rank(a.severity) > rank(e.severity))
      problems.push(`${a.id} (${a.pkg}) is ${a.severity}, above the recorded ${e.severity}`);
  }
  for (const name of all.unattributed)
    problems.push(`${name} is high/critical with no traceable advisory`);
  for (const e of entries) {
    if (!all.advisories.some((a) => a.id === e.ghsa && a.pkg === e.package)) {
      problems.push(
        `${e.ghsa} (${e.package}): stale allowlist entry, the advisory is no longer reported; remove it from specs/npm-audit-allowlist.v1.json`,
      );
    }
  }
}

function parseArgs(argv) {
  const opts = {};
  for (let i = 0; i < argv.length; i += 2) {
    const flag = argv[i];
    if (!["--all", "--prod", "--allowlist", "--now"].includes(flag) || i + 1 >= argv.length)
      return null;
    opts[flag.slice(2)] = argv[i + 1];
  }
  return opts.all && opts.prod && opts.allowlist ? opts : null;
}

function main() {
  const opts = parseArgs(process.argv.slice(2));
  if (opts === null) {
    out(
      process.stderr,
      "usage: npm-audit-gate.mjs --all F --prod F --allowlist F [--now YYYY-MM-DD]",
    );
    return 2;
  }
  const todayMs =
    opts.now === undefined
      ? Date.parse(`${new Date().toISOString().slice(0, 10)}T00:00:00Z`)
      : parseDay(opts.now);
  if (todayMs === null) {
    out(process.stderr, `invalid --now ${opts.now}`);
    return 2;
  }
  const problems = [];
  const warnings = [];
  const entries = readAllowlist(readJson(opts.allowlist, "allowlist", problems), problems);
  const all = readReport(readJson(opts.all, "audit (all)", problems), "audit (all)", problems);
  const prod = readReport(
    readJson(opts.prod, "audit (--omit=dev)", problems),
    "audit (--omit=dev)",
    problems,
  );
  checkExpiry(entries, todayMs, problems, warnings); // gate:expiry
  if (prod !== null) checkProdClean(prod, entries, problems); // gate:prod-clean
  if (all !== null) checkAll(all, entries, problems);
  for (const w of warnings) out(process.stdout, `::warning::npm audit allowlist: ${w}`);
  for (const p of problems) {
    out(process.stderr, `verify-npm-audit: FAIL: ${p}`);
    out(process.stdout, `::error::npm audit gate: ${p}`);
  }
  if (problems.length > 0) return 1;
  const used = entries.map((e) => `${e.ghsa} (${e.package}, until ${e.expires})`).join("; ");
  out(
    process.stdout,
    `verify-npm-audit: PASS; production clean; allowlisted dev-only advisories: ${used || "none"}`,
  );
  return 0;
}

process.exitCode = main();
