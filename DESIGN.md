---
name: Eshu Console
description: A vibrant product console for code-to-cloud graph truth, evidence, and operational drilldowns.
colors:
  canvas-warm: "oklch(0.94 0.015 78)"
  sidebar-ink: "oklch(0.24 0.026 250)"
  surface-paper: "oklch(0.985 0.006 78)"
  surface-raised: "oklch(0.965 0.012 78)"
  field-porcelain: "oklch(0.998 0.003 78)"
  text-ink: "oklch(0.25 0.018 250)"
  text-muted: "oklch(0.46 0.018 250)"
  text-subtle: "oklch(0.56 0.018 250)"
  line-warm: "oklch(0.82 0.018 78)"
  accent-amber: "oklch(0.62 0.15 58)"
  danger-red: "oklch(0.55 0.17 27)"
  success-green: "oklch(0.52 0.13 150)"
  relationship-blue: "oklch(0.52 0.14 260)"
  sidebar-active: "oklch(0.32 0.045 250)"
  sidebar-active-line: "oklch(0.44 0.04 250)"
  sidebar-text: "oklch(0.78 0.025 250)"
  sidebar-title: "oklch(0.96 0.006 78)"
typography:
  display:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "2rem"
    fontWeight: 800
    lineHeight: 1.15
    letterSpacing: "0"
  headline:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "1.12rem"
    fontWeight: 800
    lineHeight: 1.2
    letterSpacing: "0"
  title:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "1rem"
    fontWeight: 700
    lineHeight: 1.25
    letterSpacing: "0"
  body:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "0"
  label:
    fontFamily: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, sans-serif"
    fontSize: "0.75rem"
    fontWeight: 650
    lineHeight: 1.2
    letterSpacing: "0"
rounded:
  sm: "7px"
  md: "8px"
spacing:
  xs: "6px"
  sm: "8px"
  md: "12px"
  lg: "18px"
  xl: "24px"
  page-x: "36px"
components:
  nav-link:
    backgroundColor: "transparent"
    textColor: "{colors.sidebar-text}"
    rounded: "{rounded.sm}"
    padding: "9px 10px"
  nav-link-active:
    backgroundColor: "{colors.sidebar-active}"
    textColor: "{colors.sidebar-title}"
    rounded: "{rounded.sm}"
    padding: "9px 10px"
  search-input:
    backgroundColor: "{colors.field-porcelain}"
    textColor: "{colors.text-ink}"
    rounded: "{rounded.sm}"
    padding: "0 12px"
    height: "42px"
  panel:
    backgroundColor: "{colors.surface-paper}"
    textColor: "{colors.text-ink}"
    rounded: "{rounded.md}"
    padding: "16px"
  row-button:
    backgroundColor: "{colors.field-porcelain}"
    textColor: "{colors.text-ink}"
    rounded: "{rounded.sm}"
    padding: "8px 10px"
  graph-canvas:
    backgroundColor: "{colors.field-porcelain}"
    textColor: "{colors.text-ink}"
    rounded: "{rounded.md}"
---

# Design System: Eshu Console

## 1. Overview

**Creative North Star: "The Living Map Room"**

Eshu Console is a bright operating room for graph truth. It should feel like a
living map room: warm enough for non-engineers to enter, precise enough for
operators to trust, and vibrant enough that relationships, freshness, and
evidence are visually alive.

The system rejects generic SaaS dashboard patterns, dark-card sameness, and
decorative data theater. The console is not a homepage and not a database
viewer. It is a working product surface where the story comes first, then the
proof is immediately available.

**Key Characteristics:**

- Warm light canvas with cool technical structure.
- Dense, scannable product UI with restrained but meaningful color.
- Evidence-first surfaces that separate known, missing, inferred, and stale.
- Readable graph labels and table alternatives for broad organizational use.
- Flat tonal layering instead of decorative shadows or glass effects.

## 2. Colors

The palette is a warm product canvas with a cool slate shell and deliberate
relationship colors. OKLCH is canonical because the implementation uses OKLCH
tokens directly.

### Primary

- **Signal Amber** (`accent-amber`): Used for focus, active evidence, workflow
  emphasis, and high-value selection states. It is rare on purpose.
- **Relationship Blue** (`relationship-blue`): Used for graph relationship
  edges, selected relationship nodes, and evidence coverage counts.

### Secondary

- **Runtime Green** (`success-green`): Used only for healthy service, workload,
  or runtime placement signals.
- **Failure Red** (`danger-red`): Reserved for failure, dead-letter, dangerous,
  or blocked states.

### Neutral

- **Warm Canvas** (`canvas-warm`): The app background. It keeps the console
  bright without becoming sterile white.
- **Slate Shell** (`sidebar-ink`): The persistent navigation shell. It anchors
  the product and gives the light content area contrast.
- **Paper Surface** (`surface-paper`): Default panels, sections, tables, and
  repeated containers.
- **Raised Paper** (`surface-raised`): Hover and subtle active surfaces.
- **Porcelain Field** (`field-porcelain`): Inputs, graph canvases, compact
  rows, and data fields.
- **Ink Text** (`text-ink`): Primary text and important data labels.
- **Muted Text** (`text-muted`) and **Subtle Text** (`text-subtle`): Supporting
  copy, labels, truth metadata, and secondary descriptions.
- **Warm Line** (`line-warm`): Borders, separators, table lines, and graph
  panel dividers.

### Named Rules

**The Vibrancy Serves Truth Rule.** Color is for relationship type, status,
freshness, risk, and selection. Color is never filler.

**The No Dark Default Rule.** Do not make the console dark by default. The
product must feel bright, readable, and usable across the whole organization.

## 3. Typography

**Display Font:** system sans stack with native platform fallbacks.
**Body Font:** system sans stack with native platform fallbacks.
**Label/Mono Font:** no separate mono style is established yet.

**Character:** The type system is native, compact, and task-focused. It should
feel familiar to users of strong product tools while keeping graph and evidence
labels readable at dashboard density.

### Hierarchy

- **Display** (800, `2rem`, `1.15`): Page titles such as Dashboard, Catalog,
  Findings, and workspace entity names.
- **Headline** (800, `1.12rem`, `1.2`): Product brand in the sidebar and
  signature compact headings.
- **Title** (700, `1rem`, `1.25`): Panel titles, section headings, and table
  grouping labels.
- **Body** (400, `1rem`, `1.55`): Explanatory copy and evidence summaries.
  Prose should stay near 65 to 75 characters per line; data tables can run
  denser.
- **Label** (650, `0.75rem`, `0`): Definition-list labels, truth metadata,
  status strip terms, and compact data descriptors.

### Named Rules

**The Label Legibility Rule.** Graph labels, relationship verbs, and table
headers must stay readable before decoration is considered.

**The Plain Language First Rule.** Use plain-language summaries before exposing
technical identifiers, but keep the identifiers available for drilldown.

## 4. Elevation

The console is flat by default. Depth comes from tonal layering, borders,
sticky panels, selected states, and structured grids rather than shadows. This
keeps the UI calm enough for long sessions and prevents the surface from
turning into stacked decorative cards.

### Named Rules

**The Tonal Layer Rule.** Use `surface-paper`, `surface-raised`, and
`field-porcelain` to create hierarchy before introducing any shadow.

**The No Glass Rule.** Glassmorphism, decorative blur, and translucent panels
are forbidden unless a future interaction needs a true overlay.

## 5. Components

### Buttons

- **Shape:** Gently squared product corners (`7px`).
- **Primary:** No dedicated primary action exists yet because the console is
  read-only. Command-like buttons use `field-porcelain` with `line-warm`
  borders and `text-ink`.
- **Hover / Focus:** Hover shifts to `surface-raised`. Focus uses
  `accent-amber` border plus a soft OKLCH outline.
- **Pressed / Selected:** Selected graph buttons use `accent-amber` borders.
  Evidence rows use `relationship-blue` borders when selected.

### Chips

- **Style:** Status and truth chips should use the same squared `7px` shape,
  quiet borders, and semantic text. They must include readable labels, not only
  color.
- **State:** Selected or active chips may use warmer surface tinting, but the
  label remains the primary indicator.

### Cards / Containers

- **Corner Style:** Panels use `8px`; compact rows and controls use `7px`.
- **Background:** Major panels use `surface-paper`; compact inner rows use
  `field-porcelain`.
- **Shadow Strategy:** No default shadows. Use borders and tonal surfaces.
- **Border:** `line-warm` is the default border. Relationship-selected states
  use `relationship-blue`.
- **Internal Padding:** Main panels use `16px` to `18px`; compact rows use
  `10px` to `14px`.

### Inputs / Fields

- **Style:** Inputs use `field-porcelain`, `line-warm`, `7px` corners, and at
  least `42px` height.
- **Focus:** Focus shifts border to `accent-amber` and adds a visible outline.
- **Error / Disabled:** Error should use `danger-red` with text explanation.
  Disabled states should reduce contrast without losing label legibility.

### Navigation

- **Style:** A cool slate sidebar holds the console brand, primary nav, and
  environment strip. Active nav uses `sidebar-active`, `sidebar-active-line`,
  and `sidebar-title`.
- **Typography:** Nav labels use weight `650` and compact padding.
- **Mobile:** At narrow widths, the sidebar becomes a static top block and nav
  becomes a four-column grid.

### Relationship Graph

- **Style:** The graph sits on `field-porcelain` with `8px` corners and readable
  node labels. Relationship edges use `relationship-blue` mixed with line color.
- **Interaction:** Nodes and edges must support drilldown. Details belong next
  to the graph, not hidden in a modal.
- **Rule:** Never clip repository, workload, or relationship names when the
  graph is the main evidence surface.

### Evidence Rows

- **Style:** Evidence rows use compact two-column layout with source, verb, and
  path visible. Selected evidence uses `relationship-blue` border.
- **Purpose:** Evidence rows translate graph truth into a story users can audit.
  They must not devolve into ungrouped key/value dumps.

## 6. Do's and Don'ts

### Do:

- **Do** keep the console light, vibrant, and readable by default.
- **Do** use `relationship-blue` for graph relationships and selected evidence.
- **Do** use `accent-amber` for focus and rare high-value emphasis.
- **Do** show story and proof together: summary, graph, table, and drilldown.
- **Do** label status, freshness, truth, and missing evidence in text as well
  as color.
- **Do** keep charts and graphs readable for executives, support, finance, and
  engineers.

### Don't:

- **Don't** use generic SaaS dashboard patterns with oversized metric cards and
  decorative gradients.
- **Don't** use dark-card sameness where every panel looks equally important.
- **Don't** ship unreadable relationship graphs with clipped names or unlabeled
  edges.
- **Don't** create key/value evidence dumps that require users to
  reverse-engineer the story.
- **Don't** use mock data or demo phrasing on real-data surfaces.
- **Don't** use engineer-only terminology without plain-language context.
- **Don't** add decorative UI that makes status, risk, freshness, or evidence
  harder to read.
- **Don't** use side-stripe borders, gradient text, glassmorphism, nested cards,
  or modal-first drilldowns.
