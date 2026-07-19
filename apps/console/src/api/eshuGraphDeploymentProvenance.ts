import type { EshuTruth } from "./envelope";
import {
  deploymentArtifactKey,
  deploymentPlatformKey,
  namedDeploymentRecordKey,
  networkPathKey,
} from "./eshuGraphDeploymentWire";
import type {
  DeploymentArtifactRecord,
  DeploymentInstanceRecord,
  DeploymentPlatformRecord,
  NamedDeploymentRecord,
  NetworkPathRecord,
} from "./eshuGraphDeploymentWire";

export interface SourceRows<T> {
  readonly contextRows: readonly T[];
  readonly contextTruth?: EshuTruth | null;
  readonly traceRows: readonly T[];
  readonly traceTruth?: EshuTruth | null;
}

export function preferredSourceRows<T>(sources: SourceRows<T>): T[] {
  if (sources.traceRows.length > 0 && isCurrent(sources.traceTruth)) {
    return [...sources.traceRows, ...sources.contextRows];
  }
  if (sources.contextRows.length > 0 && isCurrent(sources.contextTruth)) {
    return [...sources.contextRows, ...sources.traceRows];
  }
  return [...sources.traceRows, ...sources.contextRows];
}

export function artifactRecordTruth(
  record: DeploymentArtifactRecord,
  sources: SourceRows<DeploymentArtifactRecord>,
): EshuTruth | null | undefined {
  return matchingRecordTruth(record, sources, deploymentArtifactKey);
}

export function instanceRecordTruth(
  instanceID: string,
  sources: SourceRows<DeploymentInstanceRecord>,
): EshuTruth | null | undefined {
  return selectSourceTruth(
    sources.contextRows.some((row) => row.instance_id?.trim() === instanceID),
    sources.traceRows.some((row) => row.instance_id?.trim() === instanceID),
    sources.contextTruth,
    sources.traceTruth,
  );
}

export function platformRecordTruth(
  instanceID: string,
  platform: DeploymentPlatformRecord,
  sources: SourceRows<DeploymentInstanceRecord>,
): EshuTruth | null | undefined {
  const key = deploymentPlatformKey(platform);
  const contains = (rows: readonly DeploymentInstanceRecord[]) =>
    rows.some(
      (instance) =>
        instance.instance_id?.trim() === instanceID &&
        (instance.platforms ?? []).some((candidate) => deploymentPlatformKey(candidate) === key),
    );
  return selectSourceTruth(
    contains(sources.contextRows),
    contains(sources.traceRows),
    sources.contextTruth,
    sources.traceTruth,
  );
}

export function networkPathRecordTruth(
  record: NetworkPathRecord,
  sources: SourceRows<NetworkPathRecord>,
): EshuTruth | null | undefined {
  return matchingRecordTruth(record, sources, networkPathKey);
}

export function namedRecordTruth(
  record: NamedDeploymentRecord,
  sources: SourceRows<NamedDeploymentRecord>,
): EshuTruth | null | undefined {
  return matchingRecordTruth(record, sources, namedDeploymentRecordKey);
}

export function currentRecordsFirst<T>(
  rows: readonly T[],
  truthFor: (row: T) => EshuTruth | null | undefined,
): T[] {
  const current: T[] = [];
  const nonCurrent: T[] = [];
  rows.forEach((row) => {
    (isCurrent(truthFor(row)) ? current : nonCurrent).push(row);
  });
  return [...current, ...nonCurrent];
}

function matchingRecordTruth<T>(
  record: T,
  sources: SourceRows<T>,
  keyFor: (row: T) => string,
): EshuTruth | null | undefined {
  const key = keyFor(record);
  return selectSourceTruth(
    sources.contextRows.some((candidate) => keyFor(candidate) === key),
    sources.traceRows.some((candidate) => keyFor(candidate) === key),
    sources.contextTruth,
    sources.traceTruth,
  );
}

function selectSourceTruth(
  inContext: boolean,
  inTrace: boolean,
  contextTruth: EshuTruth | null | undefined,
  traceTruth: EshuTruth | null | undefined,
): EshuTruth | null | undefined {
  if (inTrace && isCurrent(traceTruth)) return traceTruth;
  if (inContext && isCurrent(contextTruth)) return contextTruth;
  if (inTrace && traceTruth) return traceTruth;
  if (inContext && contextTruth) return contextTruth;
  if (inTrace) return traceTruth;
  if (inContext) return contextTruth;
  return undefined;
}

function isCurrent(truth: EshuTruth | null | undefined): boolean {
  return truth?.freshness.state === "fresh" || truth == null;
}
