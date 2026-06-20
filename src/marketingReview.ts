// Reusable, DOM-agnostic evaluation logic for the marketing-site browser
// review gate (issue #3330).
//
// The Playwright script in `scripts/marketing-review.mjs` collects observed
// facts from the rendered ROOT marketing site (anchors, CTAs, links, headings,
// accessibility signals, and a coarse load timing) for both desktop and mobile
// viewports. This module turns those observed facts into a deterministic
// pass/fail verdict. Keeping the decision logic here (instead of inline in the
// script) makes the regression contract unit-testable without a browser.

/** Identifier for the viewport a single observation was captured under. */
export type ReviewViewport = "desktop" | "mobile";

/** A required in-page anchor target (e.g. a nav link to `#product`). */
export interface AnchorExpectation {
  /** Anchor id without the leading `#`, e.g. `product`. */
  readonly id: string;
  /** Human label for findings output. */
  readonly label: string;
}

/** A required call to action rendered as a link. */
export interface CtaExpectation {
  /** Stable label substring the CTA link text must contain. */
  readonly label: string;
  /** Expected `href` value for the CTA link. */
  readonly href: string;
}

/**
 * The static review contract for the marketing site. These are the primary
 * routes (anchor sections), CTAs, and the Ask Eshu positioning that must not
 * regress. The contract is intentionally derived from the shipped
 * `siteContent` so the gate stays aligned with the source of truth.
 */
export interface MarketingReviewContract {
  /** In-page anchor sections that must exist and be reachable. */
  readonly anchors: readonly AnchorExpectation[];
  /** Primary calls to action that must render with the expected hrefs. */
  readonly ctas: readonly CtaExpectation[];
  /**
   * Substrings that prove the Ask Eshu / first-run positioning is present in
   * the hero copy and must not regress.
   */
  readonly askEshuPositioning: readonly string[];
  /** Maximum acceptable DOM-content-loaded time per viewport, milliseconds. */
  readonly maxDomContentLoadedMs: number;
}

/** Observed facts captured from one rendered viewport. */
export interface ViewportObservation {
  /** Which viewport these facts came from. */
  readonly viewport: ReviewViewport;
  /** Anchor ids that resolved to a visible section element. */
  readonly resolvedAnchorIds: readonly string[];
  /** Observed CTA links as `{ text, href }` pairs. */
  readonly ctaLinks: readonly { readonly text: string; readonly href: string }[];
  /** Full visible body text, used for positioning substring checks. */
  readonly bodyText: string;
  /** Count of `<img>` elements rendered without a non-empty `alt`. */
  readonly imagesMissingAlt: number;
  /** Count of `<h1>` elements rendered (a single landmark heading expected). */
  readonly h1Count: number;
  /** External links (http/https) that were verified reachable. */
  readonly reachableExternalLinks: readonly string[];
  /** External links that failed reachability verification. */
  readonly unreachableExternalLinks: readonly string[];
  /** DOMContentLoaded timing in milliseconds for this viewport load. */
  readonly domContentLoadedMs: number;
}

/** Severity of a single finding. */
export type FindingSeverity = "pass" | "fail";

/** A single check result for the human-readable report. */
export interface ReviewFinding {
  readonly viewport: ReviewViewport;
  readonly check: string;
  readonly severity: FindingSeverity;
  readonly detail: string;
}

/** Aggregate verdict across every viewport observation. */
export interface MarketingReviewResult {
  readonly passed: boolean;
  readonly findings: readonly ReviewFinding[];
}

function evaluateViewport(
  contract: MarketingReviewContract,
  observation: ViewportObservation
): ReviewFinding[] {
  const findings: ReviewFinding[] = [];
  const { viewport } = observation;
  const resolved = new Set(observation.resolvedAnchorIds);

  for (const anchor of contract.anchors) {
    const present = resolved.has(anchor.id);
    findings.push({
      viewport,
      check: `anchor:#${anchor.id}`,
      severity: present ? "pass" : "fail",
      detail: present
        ? `${anchor.label} section reachable`
        : `${anchor.label} section (#${anchor.id}) missing`
    });
  }

  for (const cta of contract.ctas) {
    const match = observation.ctaLinks.find(
      (link) => link.text.includes(cta.label) && link.href === cta.href
    );
    findings.push({
      viewport,
      check: `cta:${cta.label}`,
      severity: match ? "pass" : "fail",
      detail: match
        ? `CTA "${cta.label}" -> ${cta.href}`
        : `CTA "${cta.label}" -> ${cta.href} not found`
    });
  }

  for (const phrase of contract.askEshuPositioning) {
    const present = observation.bodyText.includes(phrase);
    findings.push({
      viewport,
      check: `positioning:${phrase}`,
      severity: present ? "pass" : "fail",
      detail: present
        ? `positioning present: "${phrase}"`
        : `positioning missing: "${phrase}"`
    });
  }

  findings.push({
    viewport,
    check: "a11y:img-alt",
    severity: observation.imagesMissingAlt === 0 ? "pass" : "fail",
    detail:
      observation.imagesMissingAlt === 0
        ? "all images expose alt text"
        : `${observation.imagesMissingAlt} image(s) missing alt text`
  });

  findings.push({
    viewport,
    check: "a11y:single-h1",
    severity: observation.h1Count === 1 ? "pass" : "fail",
    detail:
      observation.h1Count === 1
        ? "exactly one <h1> landmark"
        : `expected 1 <h1>, found ${observation.h1Count}`
  });

  findings.push({
    viewport,
    check: "links:external-reachable",
    severity: observation.unreachableExternalLinks.length === 0 ? "pass" : "fail",
    detail:
      observation.unreachableExternalLinks.length === 0
        ? `${observation.reachableExternalLinks.length} external link(s) reachable`
        : `unreachable: ${observation.unreachableExternalLinks.join(", ")}`
  });

  findings.push({
    viewport,
    check: "perf:dom-content-loaded",
    severity:
      observation.domContentLoadedMs <= contract.maxDomContentLoadedMs
        ? "pass"
        : "fail",
    detail: `DOMContentLoaded ${observation.domContentLoadedMs}ms (budget ${contract.maxDomContentLoadedMs}ms)`
  });

  return findings;
}

/**
 * Evaluate observed marketing-site facts from every viewport against the
 * review contract. Returns a deterministic, ordered set of findings and a
 * single aggregate pass flag (true only when every finding passes).
 */
export function evaluateMarketingReview(
  contract: MarketingReviewContract,
  observations: readonly ViewportObservation[]
): MarketingReviewResult {
  const findings = observations.flatMap((observation) =>
    evaluateViewport(contract, observation)
  );
  const passed =
    observations.length > 0 &&
    findings.every((finding) => finding.severity === "pass");
  return { passed, findings };
}
