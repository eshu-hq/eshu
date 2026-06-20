/** Navigation link rendered in the top-level site header. */
export interface NavItem {
  readonly label: string;
  readonly href: string;
}

/** Short launch-page card for a product capability or layer. */
export interface Capability {
  readonly title: string;
  readonly description: string;
}

/** One newly shipped surface highlighted near the top of the page. */
export interface WhatsNewItem {
  readonly title: string;
  readonly summary: string;
  readonly detail: string;
}

/** One step in the source-to-runtime evidence pipeline. */
export interface PipelineStep {
  readonly label: string;
  readonly detail: string;
}

/** Representative question and answer pair for the use-case grid. */
export interface UseCase {
  readonly question: string;
  readonly answer: string;
}

/** One channel where users can reach Eshu's graph-backed data. */
export interface Surface {
  readonly title: string;
  readonly description: string;
}

/** First prompt suggestion for one engineering role. */
export interface RolePrompt {
  readonly role: string;
  readonly prompt: string;
}

/** Organization-wide proof point shown on the launch page. */
export interface ProofPoint {
  readonly value: string;
  readonly title: string;
  readonly description: string;
}

/** Node in the rendered source-to-runtime graph illustration. */
export interface DemoNode {
  readonly id: string;
  readonly label: string;
  readonly detail: string;
}

/** Interactive CLI or MCP command demo. */
export interface CommandDemo {
  readonly command: string;
  readonly summary: string;
  readonly output: readonly string[];
  readonly activeNodeId: string;
}

/** Representative persona tab in the role examples section. */
export interface PersonaDemo {
  readonly role: string;
  readonly context: string;
  readonly question: string;
  readonly answer: string;
  readonly primaryTool: string;
}

/** One cleanup investigation mode and sample findings. */
export interface CleanupMode {
  readonly label: string;
  readonly summary: string;
  readonly findings: readonly string[];
}
