// pages/home/roles.ts
// The "I'm in…" lens on Home. A role only changes emphasis — which questions
// are suggested and which attention items come first. It never hides data.
import type { RoleId } from "./homeModel";
import type { InvestigationWorkflowsWire } from "../../api/contracts/wire/InvestigationWorkflowsWire";

export interface RoleLens {
  readonly id: RoleId;
  readonly label: string;
  readonly blurb: string;
  // Plain-language starter questions. {service} is replaced with a real
  // service from the catalog when one is available.
  readonly questions: readonly string[];
  // Guided-workflow domains whose server-provided starter prompts suit this role.
  readonly workflowDomains: readonly string[];
}

export const ROLE_LENSES: readonly RoleLens[] = [
  {
    id: "everyone",
    label: "Everyone",
    blurb: "Find any service, team or system and understand it in plain English.",
    questions: [
      "What does {service} do and who owns it?",
      "Which team should I talk to about payments?",
      "How does a customer order flow through our systems?",
    ],
    workflowDomains: [],
  },
  {
    id: "engineering",
    label: "Engineering",
    blurb: "See who calls what, what a change will touch, and how code reaches production.",
    questions: [
      "What depends on {service}, and what breaks if I change its API?",
      "Which repositories use our shared client library?",
      "How does {service} get from a merged PR to production?",
    ],
    workflowDomains: ["deployable_drift"],
  },
  {
    id: "security",
    label: "Security",
    blurb: "Separate vulnerabilities that actually reach running software from scanner noise.",
    questions: [
      "Which running services are affected by our critical vulnerabilities?",
      "Who can read production secrets, and through which roles?",
      "Is {service} exposed to the internet?",
    ],
    workflowDomains: ["supply_chain"],
  },
  {
    id: "sre",
    label: "On-call & SRE",
    blurb: "Know what changed before an alert and who owns what's broken.",
    questions: [
      "What changed in {service} in the last 24 hours?",
      "Who is on call for {service}, and where is its runbook?",
      "What else shares infrastructure with {service}?",
    ],
    workflowDomains: ["incident_context"],
  },
  {
    id: "platform",
    label: "Platform",
    blurb: "Find drift, unmanaged cloud resources and gaps in what Eshu can see.",
    questions: [
      "Which cloud resources aren't managed in Terraform?",
      "Where does what's running differ from what's declared?",
      "Which data sources is Eshu missing?",
    ],
    workflowDomains: ["deployable_drift"],
  },
  {
    id: "support",
    label: "Support",
    blurb: "Get a straight answer on who owns something and whether it's healthy.",
    questions: [
      "Who owns {service} and how do I reach them?",
      "Is {service} having problems right now?",
      "Where is the troubleshooting guide for {service}?",
    ],
    workflowDomains: ["incident_context"],
  },
  {
    id: "leadership",
    label: "Leadership",
    blurb: "Understand risk, coverage and ownership across the organization at a glance.",
    questions: [
      "What is our security risk across production right now?",
      "Which services have no clear owner?",
      "How much of our organization can Eshu answer questions about?",
    ],
    workflowDomains: ["supply_chain"],
  },
];

export function roleLens(id: RoleId): RoleLens {
  return ROLE_LENSES.find((lens) => lens.id === id) ?? ROLE_LENSES[0];
}

// suggestedQuestions fills {service} and appends the server's own starter
// prompts for the role's guided workflows.
export function suggestedQuestions(
  lens: RoleLens,
  service: string | null,
  workflows: InvestigationWorkflowsWire | null,
): string[] {
  const named = lens.questions.map((q) => (service ? q.replaceAll("{service}", service) : q.replaceAll("{service}", "our main service")));
  const starters = (workflows?.workflows ?? [])
    .filter((wf) => wf.domain !== undefined && lens.workflowDomains.includes(wf.domain))
    .flatMap((wf) => ("starter_prompts" in wf ? (wf.starter_prompts ?? []) : []));
  return [...new Set([...named, ...starters])].slice(0, 4);
}

const ROLE_STORAGE_KEY = "eshu.console.homeRole";

export function loadRole(): RoleId {
  try {
    const saved = window.localStorage.getItem(ROLE_STORAGE_KEY);
    if (saved && ROLE_LENSES.some((lens) => lens.id === saved)) return saved as RoleId;
  } catch {
    // Storage can be unavailable (private mode); fall back to the default lens.
  }
  return "everyone";
}

export function saveRole(role: RoleId): void {
  try {
    window.localStorage.setItem(ROLE_STORAGE_KEY, role);
  } catch {
    // Non-fatal: the lens just won't persist.
  }
}
