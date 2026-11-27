// pages/home/KnowledgeHomePage.tsx
// Home: the organization's knowledge base front door for every employee.
// Search or ask first; then what needs attention, how to browse, who owns
// what, and how complete Eshu's knowledge is. Every panel reads a contract-
// typed endpoint (api/knowledgeHome.ts) and degrades on its own.
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";

import { HomeHero } from "./HomeHero";
import {
  attentionItems,
  exampleService,
  health,
  knowledgeSummary,
  orderForRole,
  ready,
  type RoleId,
} from "./homeModel";
import {
  AttentionPanel,
  BrowsePanel,
  CoverageList,
  GuidedList,
  Panel,
  SectionBody,
  TeamsPanel,
  TruthGuide,
} from "./HomePanels";
import { loadRole, roleLens, saveRole, suggestedQuestions } from "./roles";
import type { EshuApiClient } from "../../api/client";
import { loadKnowledgeHome, loadingHome, type KnowledgeHomeData } from "../../api/knowledgeHome";
import "./knowledgeHome.css";

export interface KnowledgeHomePageProps {
  readonly client: EshuApiClient | undefined;
  readonly onFind: (query: string) => void;
}

export function KnowledgeHomePage({ client, onFind }: KnowledgeHomePageProps): React.JSX.Element {
  const navigate = useNavigate();
  const [data, setData] = useState<KnowledgeHomeData>(loadingHome);
  const [role, setRole] = useState<RoleId>(loadRole);

  useEffect(() => {
    if (client === undefined) return;
    const controller = new AbortController();
    setData(loadingHome);
    void loadKnowledgeHome(
      client,
      (key, section) => setData((current) => ({ ...current, [key]: section })),
      controller.signal,
    );
    return () => controller.abort();
  }, [client]);

  const lens = roleLens(role);
  const questions = useMemo(
    () => suggestedQuestions(lens, exampleService(ready(data.catalog)), ready(data.workflows)),
    [lens, data.catalog, data.workflows],
  );
  const attention = useMemo(
    () => orderForRole(attentionItems(data), role).slice(0, 5),
    [data, role],
  );

  function changeRole(next: RoleId): void {
    setRole(next);
    saveRole(next);
  }

  return (
    <div className="page kh-page">
      <HomeHero
        summary={knowledgeSummary(data)}
        health={health(data)}
        role={role}
        blurb={lens.blurb}
        questions={questions}
        onRoleChange={changeRole}
        onAsk={(question) => navigate(`/ask?q=${encodeURIComponent(question)}`)}
        onFind={onFind}
      />

      <div className="kh-grid">
        <div className="kh-col">
          <AttentionPanel items={attention} />
          <BrowsePanel overview={ready(data.overview)} />
          <Panel
            id="kh-teams"
            title="Teams & ownership"
            subtitle="Who owns what, from your service catalog."
            action={{ to: "/catalog", label: "All services" }}
          >
            <SectionBody
              section={data.catalog}
              label="teams"
              render={(catalog) => <TeamsPanel catalog={catalog} />}
            />
          </Panel>
        </div>
        <div className="kh-col">
          <Panel
            id="kh-coverage"
            title="What Eshu can see"
            subtitle="The more sources are connected, the more questions Eshu can answer."
            action={{ to: "/collector-readiness", label: "Data sources" }}
          >
            <SectionBody
              section={data.collectors}
              label="data sources"
              render={(collectors) => <CoverageList collectors={collectors} />}
            />
          </Panel>
          <Panel
            id="kh-guided"
            title="Guided investigations"
            subtitle="Step-by-step help when you don't know where to start."
            action={{ to: "/guided-questions", label: "Start one" }}
          >
            <SectionBody
              section={data.workflows}
              label="guided investigations"
              render={(wf) => <GuidedList workflows={wf} />}
            />
          </Panel>
          <TruthGuide />
        </div>
      </div>
    </div>
  );
}
