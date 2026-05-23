export interface NavItem {
  readonly label: string;
  readonly href: string;
}

export interface Capability {
  readonly title: string;
  readonly description: string;
}

export interface PipelineStep {
  readonly label: string;
  readonly detail: string;
}

export interface UseCase {
  readonly question: string;
  readonly answer: string;
}

export interface Surface {
  readonly title: string;
  readonly description: string;
}

export interface RolePrompt {
  readonly role: string;
  readonly prompt: string;
}

export interface ProofPoint {
  readonly value: string;
  readonly title: string;
  readonly description: string;
}

export interface DemoNode {
  readonly id: string;
  readonly label: string;
  readonly detail: string;
}

export interface CommandDemo {
  readonly command: string;
  readonly summary: string;
  readonly output: readonly string[];
  readonly activeNodeId: string;
}

export interface PersonaDemo {
  readonly role: string;
  readonly question: string;
  readonly answer: string;
}

export interface CleanupMode {
  readonly label: string;
  readonly summary: string;
  readonly findings: readonly string[];
}

const docsHref = "https://github.com/eshu-hq/eshu/tree/main/docs/public";

export const siteContent = {
  nav: [
    { label: "Product", href: "#product" },
    { label: "How it works", href: "#how-it-works" },
    { label: "Scale", href: "#scale" },
    { label: "CLI", href: "#cli" },
    { label: "Use cases", href: "#use-cases" },
    { label: "Docs", href: docsHref }
  ] satisfies readonly NavItem[],
  hero: {
    coreLine: "One Graph. Every Layer. True Path.",
    heading: "Find the true path through your stack.",
    description:
      "Eshu builds a living, organization-wide code-to-cloud graph across source, SQL, Terraform, Kubernetes, cloud, runtime, and docs, so teams can see what actually exists and where it leads.",
    primaryCta: { label: "View on GitHub", href: "https://github.com/eshu-hq/eshu" },
    secondaryCta: { label: "Read the docs", href: docsHref }
  },
  capabilities: [
    {
      title: "Trace code to cloud",
      description:
        "Follow a service from source files through Terraform, Kubernetes, cloud resources, and the runtime that serves it."
    },
    {
      title: "Keep docs honest",
      description:
        "Verify checkable Markdown claims against CLI, API, environment, path, image, and Terraform truth before stale notes drift quietly."
    },
    {
      title: "Find dead code and dead IaC",
      description:
        "Find unused code paths, Terraform modules, Helm charts, Kustomize overlays, Compose services, and other stale infrastructure definitions."
    },
    {
      title: "Map dependencies",
      description:
        "See which services, resources, repos, and deployment paths depend on each other before a change moves."
    },
    {
      title: "Give humans and AI the same source of truth",
      description:
        "Expose the graph through CLI, API, MCP, and docs so assistants and engineers work from the same context."
    }
  ] satisfies readonly Capability[],
  pipeline: [
    { label: "Git repositories", detail: "code, imports, symbols" },
    { label: "IaC and deployment metadata", detail: "Terraform, Kubernetes, Helm" },
    { label: "Cloud/runtime evidence", detail: "resources, workloads, status" },
    { label: "Eshu graph", detail: "relationships and ownership" },
    { label: "CLI/API/MCP/docs", detail: "answers where teams work" }
  ] satisfies readonly PipelineStep[],
  terminalCommands: [
    "eshu scan",
    "eshu trace service checkout",
    "eshu map --from terraform/aws_lb.main",
    "eshu docs verify"
  ] as const,
  demoTrace: {
    service: "checkout-service",
    nodes: [
      { id: "code", label: "Code", detail: "services/checkout" },
      { id: "sql", label: "SQL", detail: "orders, payments" },
      { id: "terraform", label: "Terraform", detail: "aws_lb.checkout" },
      { id: "kubernetes", label: "Kubernetes", detail: "payments namespace" },
      { id: "cloud", label: "Cloud", detail: "prod us-east-1" },
      { id: "runtime", label: "Runtime", detail: "checkout-api pods" },
      { id: "docs", label: "Docs", detail: "runbook drift found" }
    ] satisfies readonly DemoNode[]
  },
  commandDemos: [
    {
      command: "eshu scan --json",
      summary: "Graph ready for organization-wide questions.",
      activeNodeId: "code",
      output: [
        "\"status\": \"ready\",",
        "\"succeeded\": 8347,",
        "\"queue_zero_ms\": 853600,",
        "\"freshness\": \"current\""
      ]
    },
    {
      command: "eshu trace service checkout",
      summary: "Trace checkout from source to the runtime that serves it.",
      activeNodeId: "kubernetes",
      output: [
        "Service: checkout-service",
        "Repository: repo-checkout (checkout-service)",
        "Truth freshness: fresh",
        "Code to runtime:",
        "Trace status: partial",
        "- source: exact (2 evidence)",
        "- deployment: derived (3 evidence)",
        "Missing evidence: runtime"
      ]
    },
    {
      command: "eshu map --from terraform/aws_lb.main",
      summary: "Start from IaC and find the services behind the resource.",
      activeNodeId: "terraform",
      output: [
        "Map: terraform/aws_lb.main",
        "Resolved: TerraformResource tfstate:aws_lb.main (aws_lb.main)",
        "Defined by:",
        "- DEFINES infra-repo",
        "Depends on:",
        "- PROVISIONS_DEPENDENCY_FOR checkout",
        "Evidence: 2 relationships"
      ]
    },
    {
      command: "eshu docs verify",
      summary: "Compare written claims against bounded local truth.",
      activeNodeId: "docs",
      output: [
        "Docs verify: documents=3 claims=6 valid=4 contradicted=1 missing_evidence=1 unsupported=0 truncated=false",
        "- contradicted terraform_address aws_sqs_queue.missing",
        "- missing_evidence image_ref ghcr.io/acme/checkout:1.2.3"
      ]
    }
  ] satisfies readonly CommandDemo[],
  personaDemos: [
    {
      role: "App Eng",
      question: "Who calls this service and what breaks if I move it?",
      answer:
        "Checkout has callers in cart, orders, and billing. The graph shows the repo paths, SQL tables, and deployment targets before the PR lands."
    },
    {
      role: "Platform",
      question: "Which Kubernetes workloads does this Terraform feed?",
      answer:
        "The map command resolves a typed Terraform resource, shows bounded graph neighborhoods, and marks ambiguous selectors instead of guessing."
    },
    {
      role: "SRE",
      question: "What is the blast radius during an incident?",
      answer:
        "Start from the degraded runtime and walk back to services, owners, docs, and infrastructure definitions without opening five tools."
    },
    {
      role: "Security",
      question: "What owns this exposed resource?",
      answer:
        "Eshu traces the cloud resource back to Terraform, Kubernetes ingress, service code, and the team that owns the path."
    },
    {
      role: "Docs",
      question: "Which runbooks disagree with production?",
      answer:
        "Docs verification checks explicit local Markdown claims first, then persists finding and evidence packets for API and MCP readers when configured."
    },
    {
      role: "Leadership",
      question: "How much of the engineering estate is covered?",
      answer:
        "Eshu has drained 896 repositories and 8,347 fact queue rows in 14m13.6s, with code, IaC, Kubernetes, SQL, runtime evidence, and docs in one organization-wide graph."
    }
  ] satisfies readonly PersonaDemo[],
  cleanupModes: [
    {
      label: "Dead code",
      summary: "Find code that is no longer reachable from live services.",
      findings: [
        "services/checkout/internal/legacy_coupon.go",
        "handlers/payment_retry_v1.ts",
        "jobs/reconcile_old_gateway.py"
      ]
    },
    {
      label: "Dead IaC",
      summary: "Apply the same reachability model to stale infrastructure.",
      findings: [
        "terraform/modules/legacy-cache",
        "helm/values/checkout-canary.yaml",
        "kustomize/overlays/old-payments"
      ]
    }
  ] satisfies readonly CleanupMode[],
  surfaces: [
    {
      title: "CLI",
      description:
        "Run local scans, trace a service, and map a resource without opening five consoles."
    },
    {
      title: "API",
      description:
        "Let internal tools query ownership, dependency paths, and runtime context from the same graph."
    },
    {
      title: "MCP",
      description:
        "Give AI assistants graph-backed context instead of asking them to guess from one checkout."
    },
    {
      title: "Docs checks",
      description:
        "Compare written architecture and runbooks against the system evidence Eshu indexed."
    }
  ] satisfies readonly Surface[],
  coverage:
    "Eshu indexes code and IaC together: SQL, Terraform, Kubernetes, Helm, Kustomize, Argo CD, Crossplane, CloudFormation, Terragrunt, Docker Compose, and language parsers for Go, TypeScript, Python, Java, Rust, PHP, Ruby, C#, Swift, Kotlin, and more. Capability truth is profile-aware: some answers are exact and derived from indexed facts, while dead code and dead IaC cleanup remain candidate findings until reachability roots and runtime evidence prove them.",
  proofPoints: [
    {
      value: "Kubernetes",
      title: "Deployment paths are in the graph",
      description:
        "Eshu follows services through Kubernetes manifests, Helm values, workloads, and runtime evidence so teams can see where software runs."
    },
    {
      value: "896 repos",
      title: "Indexed in under 15 minutes",
      description:
        "Eshu has drained 896 repositories and 8,347 fact queue rows in 14m13.6s, which makes whole-organization coverage practical."
    },
    {
      value: "organization-wide",
      title: "One graph for every team",
      description:
        "Platform, SRE, application, security, docs, and AI-assisted workflows can use the same graph across the whole organization."
    }
  ] satisfies readonly ProofPoint[],
  rolePrompts: [
    {
      role: "Software engineering",
      prompt: "Who calls `process_payment` across all indexed repos?"
    },
    {
      role: "Platform engineering",
      prompt: "Trace this service from Helm values to Kubernetes resources."
    },
    {
      role: "SRE and incident response",
      prompt: "What is the blast radius if this database is degraded in prod?"
    },
    {
      role: "Documentation and support",
      prompt: "Show me the source and docs evidence behind this runbook."
    }
  ] satisfies readonly RolePrompt[],
  useCases: [
    {
      question: "What owns this resource?",
      answer: "Trace the cloud object back to the repo, module, and service that define it."
    },
    {
      question: "Is this Terraform still used?",
      answer: "Check whether a module still connects to live workloads or deployment paths."
    },
    {
      question: "Where does this service run?",
      answer: "Follow the service through its deployment metadata and runtime evidence."
    },
    {
      question: "Which docs are stale?",
      answer: "Verify written claims against the graph instead of trusting a last-edited date."
    },
    {
      question: "What changes if this dependency moves?",
      answer: "Map callers, infrastructure edges, and downstream systems before the PR lands."
    }
  ] satisfies readonly UseCase[],
  closing: {
    heading: "Start with the graph. Follow the path.",
    description:
      "Start small with one repo, then let Eshu connect the code, infrastructure, and runtime evidence around it."
  }
} as const;
