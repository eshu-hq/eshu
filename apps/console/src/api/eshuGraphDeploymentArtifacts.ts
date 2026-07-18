import type { EshuTruth } from "./envelope";
import {
  addOmissionSummary,
  artifactAdmissionStatus,
  artifactEvidence,
  artifactMethod,
  graphTruth,
  omissionContract,
  repoNode,
  repoRef,
  summaryNode,
  truthEvidence,
  truthIsCurrent,
} from "./eshuGraphDeploymentPresentation";
import * as deploymentProvenance from "./eshuGraphDeploymentProvenance";
import { uniqueDeploymentArtifacts } from "./eshuGraphDeploymentWire";
import type { DeploymentArtifactRecord } from "./eshuGraphDeploymentWire";
import { cleanText, layerFor } from "./eshuGraphShared";
import type { GraphEdge, GraphNode } from "../console/types";

interface ArtifactGraphOptions {
  readonly addEdge: (edge: GraphEdge) => void;
  readonly addNode: (node: GraphNode) => boolean;
  readonly contextArtifacts: readonly DeploymentArtifactRecord[];
  readonly contextTruth?: EshuTruth | null;
  readonly limit: number;
  readonly summaries: GraphNode[];
  readonly traceArtifacts: readonly DeploymentArtifactRecord[];
  readonly traceTruth?: EshuTruth | null;
}

export function addDeploymentArtifactGraph(options: ArtifactGraphOptions): void {
  const sources = {
    contextRows: options.contextArtifacts,
    contextTruth: options.contextTruth,
    traceRows: options.traceArtifacts,
    traceTruth: options.traceTruth,
  };
  const artifacts = deploymentProvenance.currentRecordsFirst(
    uniqueDeploymentArtifacts([...options.contextArtifacts, ...options.traceArtifacts]),
    (artifact) => deploymentProvenance.artifactRecordTruth(artifact, sources),
  );
  let notAdmitted = 0;
  artifacts.slice(0, options.limit).forEach((artifact) => {
    const artifactTruth = deploymentProvenance.artifactRecordTruth(artifact, sources);
    const status = truthIsCurrent(artifactTruth)
      ? artifactAdmissionStatus(artifact)
      : `Deployment evidence · ${artifactTruth?.freshness.state ?? "stale"} · relationship not admitted`;
    const source = repoRef(artifact.source_repo_id, artifact.source_repo_name);
    const target = repoRef(artifact.target_repo_id, artifact.target_repo_name);
    if (source) {
      options.addNode({
        ...repoNode(
          source,
          status === "admitted" ? "Deployment evidence" : status,
          cleanText(artifact.path) || undefined,
          artifact.start_line,
          artifact.end_line,
        ),
        truth: graphTruth(artifactTruth),
      });
    }
    if (target) {
      options.addNode({
        ...repoNode(target, "Deployment target"),
        truth: graphTruth(artifactTruth),
      });
    }
    if (status !== "admitted") {
      notAdmitted += 1;
      return;
    }
    const verb = cleanText(artifact.relationship_type).toUpperCase();
    if (!source || !target || verb === "") {
      notAdmitted += 1;
      return;
    }
    options.addEdge({
      evidence: [...artifactEvidence(artifact), ...truthEvidence(artifactTruth)],
      layer: layerFor(verb),
      method: artifactMethod(artifact),
      s: source.id,
      sourceFamily: cleanText(artifact.artifact_family) || undefined,
      t: target.id,
      truthState: "derived",
      verb,
    });
  });
  addOmissionSummary(
    options.summaries,
    "deployment artifacts",
    Math.max(0, artifacts.length - options.limit),
    "artifacts",
    omissionContract(
      options.limit,
      artifacts
        .slice(options.limit)
        .map(
          (artifact) => cleanText(artifact.artifact_family) || cleanText(artifact.evidence_kind),
        ),
    ),
  );
  if (notAdmitted > 0) {
    options.summaries.push(
      summaryNode("not_admitted", `${notAdmitted} deployment relationships not admitted`),
    );
  }
}
