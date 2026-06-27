export type BundleReportInputFile = {
  name: string;
  bytes: number;
};

export type BundleReportRow = {
  chunk: string;
  dependency: string;
  bytes: number;
  firstLoad: boolean;
  key: string;
};

export type BundleReport = {
  ok: boolean;
  rows: BundleReportRow[];
  missingAnchor: string | null;
};

export function evaluateBundleReport(files: BundleReportInputFile[]): BundleReport;

export function formatBundleReportMarkdown(report: BundleReport): string;
