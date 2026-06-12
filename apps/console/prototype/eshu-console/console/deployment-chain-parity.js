(function () {
  function text(value) {
    return typeof value === "string" ? value.trim() : "";
  }

  function repoFromArtifact(artifact, side) {
    const id = text(side === "source" ? artifact.source_repo_id : artifact.target_repo_id);
    const name = text(side === "source" ? artifact.source_repo_name : artifact.target_repo_name);
    if (!id && !name) return null;
    return { id: id || "repository:" + name, name: name || id };
  }

  function uniqueRepos(repos) {
    const seen = new Set();
    return repos.filter((repo) => {
      if (!repo || seen.has(repo.id)) return false;
      seen.add(repo.id);
      return true;
    });
  }

  function isHelmArtifact(artifact) {
    const family = text(artifact.artifact_family).toLowerCase();
    const path = text(artifact.path).toLowerCase();
    const sourceRepo = text(artifact.source_repo_name).toLowerCase();
    return family === "helm" && (path.endsWith("/chart.yaml") || sourceRepo.indexOf("helm") >= 0 || sourceRepo.indexOf("chart") >= 0);
  }

  function isControllerArtifact(artifact) {
    const family = text(artifact.artifact_family).toLowerCase();
    return family === "argocd" || family === "kustomize";
  }

  function artifactEdgeEvidence(artifact) {
    return [
      text(artifact.artifact_family) ? "artifact family: " + text(artifact.artifact_family) : "",
      text(artifact.evidence_kind) ? "evidence kind: " + text(artifact.evidence_kind) : "",
      text(artifact.path) ? "path: " + text(artifact.path) : "",
      text(artifact.environment) ? "environment: " + text(artifact.environment) : ""
    ].filter(Boolean);
  }

  function liveDeploymentChainGraph(artifacts, repoName, serviceName) {
    // Mirrors the live console Topology deployment_evidence.artifacts mapper.
    const deployArtifacts = (Array.isArray(artifacts) ? artifacts : []).filter((artifact) =>
      text(artifact.relationship_type).toUpperCase() === "DEPLOYS_FROM"
    );
    if (!deployArtifacts.length) return null;

    const sourceRepo = deployArtifacts
      .map((artifact) => repoFromArtifact(artifact, "target"))
      .find((repo) => repo && (repo.name === repoName || repo.name === serviceName)) ||
      { id: "repository:" + repoName, name: repoName };
    const charts = uniqueRepos(deployArtifacts
      .filter(isHelmArtifact)
      .map((artifact) => repoFromArtifact(artifact, "source"))
      .filter((repo) => repo && repo.id !== sourceRepo.id));
    const chartIds = new Set(charts.map((repo) => repo.id));
    const controllers = uniqueRepos(deployArtifacts
      .filter(isControllerArtifact)
      .map((artifact) => repoFromArtifact(artifact, "source"))
      .filter((repo) => repo && repo.id !== sourceRepo.id && !chartIds.has(repo.id)));

    const nodes = [{ id: sourceRepo.id, kind: "repo", label: sourceRepo.name, sub: "source repository", col: 2 }];
    const edges = [{ s: sourceRepo.id, t: "workload", verb: "DEPLOYS_FROM", layer: "deploy", evidence: artifactEdgeEvidence(deployArtifacts[0]) }];
    const chartArtifacts = deployArtifacts.filter(isHelmArtifact);
    chartArtifacts.forEach((artifact) => {
      const repo = repoFromArtifact(artifact, "source");
      if (!repo || repo.id === sourceRepo.id) return;
      nodes.push({ id: repo.id, kind: "repo", label: repo.name, sub: "Helm chart", col: 1 });
      edges.push({ s: repo.id, t: sourceRepo.id, verb: "PACKAGES", layer: "deploy", evidence: artifactEdgeEvidence(artifact) });
    });
    deployArtifacts.filter(isControllerArtifact).forEach((artifact) => {
      const repo = repoFromArtifact(artifact, "source");
      if (!repo || repo.id === sourceRepo.id || chartIds.has(repo.id)) return;
      nodes.push({ id: repo.id, kind: "repo", label: repo.name, sub: "Deployment controller", col: 0 });
      if (!charts.length) edges.push({ s: repo.id, t: sourceRepo.id, verb: "DEPLOYS_FROM", layer: "deploy", evidence: artifactEdgeEvidence(artifact) });
      charts.forEach((chart) => edges.push({ s: repo.id, t: chart.id, verb: "DEPLOYS_HELM", layer: "deploy", evidence: artifactEdgeEvidence(artifact) }));
    });
    return { nodes, edges };
  }

  Object.assign(window, { liveDeploymentChainGraph, artifactEdgeEvidence });
})();
