import type { LucideIcon } from "lucide-react";
import {
  Activity,
  Boxes,
  Cloud,
  Code2,
  Compass,
  FileText,
  FolderGit2,
  Gauge,
  GitBranch,
  Hexagon,
  History,
  Images,
  KeyRound,
  Layers,
  LayoutDashboard,
  ListChecks,
  Network,
  PackageSearch,
  Route as RouteIcon,
  Search,
  ServerCog,
  Share2,
  ShieldCheck,
  TriangleAlert,
  User,
  UserCog,
  Waves,
  Waypoints,
  Workflow,
} from "lucide-react";

import type { MessageId } from "./messages";
import type { ConsoleModel } from "../console/types";

export type NavItem = {
  readonly to: string;
  readonly messageId: MessageId;
  readonly icon: LucideIcon;
  readonly count?: (model: ConsoleModel) => number | string | null;
  readonly alert?: boolean;
};

export type NavGroup = {
  readonly messageId: MessageId;
  readonly items: readonly NavItem[];
};

export const NAV_GROUPS: readonly NavGroup[] = [
  {
    messageId: "app.nav.group.overview",
    items: [
      { to: "/status", messageId: "app.nav.item.status", icon: Gauge },
      { to: "/dashboard", messageId: "app.nav.item.dashboard", icon: LayoutDashboard },
      { to: "/ask", messageId: "app.nav.item.ask", icon: Search },
      { to: "/guided-questions", messageId: "app.nav.item.guidedQuestions", icon: Compass },
      { to: "/impact", messageId: "app.nav.item.impact", icon: Network },
      { to: "/exposure", messageId: "app.nav.item.exposurePath", icon: RouteIcon },
      { to: "/changed-since", messageId: "app.nav.item.changedSince", icon: History },
      { to: "/explorer", messageId: "app.nav.item.graphExplorer", icon: GitBranch },
      { to: "/relationships", messageId: "app.nav.item.relationships", icon: Share2 },
      { to: "/service-story", messageId: "app.nav.item.serviceStory", icon: Waypoints },
      { to: "/service-report", messageId: "app.nav.item.serviceReport", icon: FileText },
      { to: "/nodes", messageId: "app.nav.item.nodes", icon: Hexagon },
    ],
  },
  {
    messageId: "app.nav.group.inventory",
    items: [
      {
        to: "/repositories",
        messageId: "app.nav.item.repositories",
        icon: FolderGit2,
        count: (m) => nonZero(m.runtime.repositories),
      },
      {
        to: "/catalog",
        messageId: "app.nav.item.catalog",
        icon: Boxes,
        count: (m) => nonZero(m.services?.length ?? 0),
      },
      {
        to: "/findings",
        messageId: "app.nav.item.findings",
        icon: TriangleAlert,
        count: (m) => nonZero((m.findings?.length ?? 0) + (m.vulnerabilities?.length ?? 0)),
        alert: true,
      },
      {
        to: "/images",
        messageId: "app.nav.item.images",
        icon: Images,
        count: (m) => nonZero(m.images?.length ?? 0),
      },
      {
        to: "/iac",
        messageId: "app.nav.item.iac",
        icon: Network,
        count: (m) => nonZero(m.iacResources?.length ?? 0),
      },
      { to: "/replatforming", messageId: "app.nav.item.replatforming", icon: Network },
      {
        to: "/vulnerabilities",
        messageId: "app.nav.item.vulnerabilities",
        icon: ShieldCheck,
        count: (m) => nonZero(m.vulnerabilities?.length ?? 0),
        alert: true,
      },
    ],
  },
  {
    messageId: "app.nav.group.code",
    items: [
      {
        to: "/dead-code",
        messageId: "app.nav.item.deadCode",
        icon: TriangleAlert,
        count: (m) => nonZero(m.findings.filter((finding) => finding.type === "Dead code").length),
      },
      { to: "/code-graph", messageId: "app.nav.item.codeGraph", icon: Code2 },
    ],
  },
  {
    messageId: "app.nav.group.cloudTelemetry",
    items: [
      { to: "/topology", messageId: "app.nav.item.topology", icon: GitBranch },
      { to: "/cloud", messageId: "app.nav.item.cloud", icon: Cloud },
      { to: "/secrets-iam", messageId: "app.nav.item.secretsIam", icon: KeyRound },
      { to: "/incidents", messageId: "app.nav.item.incidents", icon: TriangleAlert },
      { to: "/ci-cd/run-correlations", messageId: "app.nav.item.ciCd", icon: Workflow },
      {
        to: "/cloud-drift",
        messageId: "app.nav.item.cloudDrift",
        icon: TriangleAlert,
        alert: true,
      },
      { to: "/observability", messageId: "app.nav.item.observability", icon: Waves },
      {
        to: "/sbom",
        messageId: "app.nav.item.sbom",
        icon: PackageSearch,
        count: (m) => nonZero(m.sbom?.total ?? 0),
      },
      {
        to: "/dependencies",
        messageId: "app.nav.item.dependencies",
        icon: Boxes,
        count: (m) => nonZero(m.dependencies?.length ?? 0),
      },
    ],
  },
  {
    messageId: "app.nav.group.system",
    items: [
      { to: "/capabilities", messageId: "app.nav.item.capabilities", icon: ListChecks },
      {
        to: "/collector-readiness",
        messageId: "app.nav.item.collectorReadiness",
        icon: ShieldCheck,
        count: (m) => nonZero(m.collectorReadiness?.length ?? 0),
      },
      { to: "/surface-inventory", messageId: "app.nav.item.surfaceInventory", icon: Layers },
      { to: "/operations", messageId: "app.nav.item.operations", icon: ServerCog },
      { to: "/freshness-causality", messageId: "app.nav.item.freshness", icon: Activity },
      { to: "/profile", messageId: "app.nav.item.profile", icon: User },
      { to: "/admin", messageId: "app.nav.item.admin", icon: UserCog },
    ],
  },
];

export const NAV_ITEMS = NAV_GROUPS.flatMap((group) => group.items);

function nonZero(value: number): number | null {
  return value > 0 ? value : null;
}
