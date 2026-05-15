import { curveBumpX, line, scalePoint } from "d3";
import { useMemo, useState } from "react";
import type { ServiceDeploymentLane, ServiceSpotlight } from "../api/serviceSpotlight";

interface ServiceDeploymentLaneMapProps {
  readonly spotlight: ServiceSpotlight;
}

interface LaneLayout {
  readonly lane: ServiceDeploymentLane;
  readonly pathToEnvironment: string;
  readonly pathToLane: string;
  readonly y: number;
}

const graphWidth = 840;
const graphHeight = 320;

export function ServiceDeploymentLaneMap({
  spotlight
}: ServiceDeploymentLaneMapProps): React.JSX.Element {
  const [selectedLabel, setSelectedLabel] = useState<string | undefined>(
    spotlight.lanes[0]?.label
  );
  const selectedLane =
    spotlight.lanes.find((lane) => lane.label === selectedLabel) ?? spotlight.lanes[0];
  const layout = useMemo(() => layoutLanes(spotlight.lanes), [spotlight.lanes]);

  return (
    <div aria-label={`${spotlight.name} deployment lane map`} className="service-lane-map">
      <svg
        aria-label={`${spotlight.name} deployment lanes`}
        className="service-lane-map-svg"
        role="img"
        viewBox={`0 0 ${graphWidth} ${graphHeight}`}
      >
        <ServiceNode name={spotlight.name} />
        {layout.map((lane) => (
          <g key={`${lane.lane.label}:links`}>
            <path className="service-lane-link" d={lane.pathToLane} />
            <path className="service-lane-link" d={lane.pathToEnvironment} />
          </g>
        ))}
        {layout.map((lane) => (
          <LaneRow
            isSelected={selectedLane?.label === lane.lane.label}
            key={lane.lane.label}
            lane={lane.lane}
            onSelect={() => setSelectedLabel(lane.lane.label)}
            y={lane.y}
          />
        ))}
      </svg>
      {selectedLane !== undefined ? <LaneDetail lane={selectedLane} /> : null}
    </div>
  );
}

function ServiceNode({ name }: { readonly name: string }): React.JSX.Element {
  return (
    <g className="service-lane-node service-lane-node-root">
      <rect height="62" rx="8" width="190" x="44" y="129" />
      <text x="139" y="166">
        {labelLines(name, 20).map((lineText, index) => (
          <tspan dy={index === 0 ? 0 : 17} key={lineText} x="139">
            {lineText}
          </tspan>
        ))}
      </text>
    </g>
  );
}

function LaneRow({
  isSelected,
  lane,
  onSelect,
  y
}: {
  readonly isSelected: boolean;
  readonly lane: ServiceDeploymentLane;
  readonly onSelect: () => void;
  readonly y: number;
}): React.JSX.Element {
  return (
    <g
      aria-label={`${lane.label} lane`}
      className={`service-lane-row${isSelected ? " service-lane-row-selected" : ""}`}
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
      role="button"
      tabIndex={0}
    >
      <rect height="58" rx="8" width="184" x="330" y={y - 29} />
      <text x="422" y={y + 5}>
        {lane.label}
      </text>
      <rect height="70" rx="8" width="220" x="582" y={y - 35} />
      <text x="692" y={y - labelYOffset(environmentLabel(lane))}>
        {labelLines(environmentLabel(lane), 22).map((lineText, index) => (
          <tspan dy={index === 0 ? 0 : 17} key={`${lane.label}:${lineText}`} x="692">
            {lineText}
          </tspan>
        ))}
      </text>
    </g>
  );
}

function LaneDetail({ lane }: { readonly lane: ServiceDeploymentLane }): React.JSX.Element {
  return (
    <aside aria-label="Selected deployment lane" className="service-lane-detail">
      <div>
        <h4>{lane.label}</h4>
        <p>{lane.environments.join(", ") || "No environment evidence yet"}</p>
      </div>
      <dl>
        <div>
          <dt>Sources</dt>
          <dd>{lane.sourceRepos.join(", ") || "not observed"}</dd>
        </div>
        <div>
          <dt>Evidence</dt>
          <dd>{`${lane.evidenceCount} items`}</dd>
        </div>
      </dl>
    </aside>
  );
}

function layoutLanes(lanes: readonly ServiceDeploymentLane[]): readonly LaneLayout[] {
  const pathLine = line<[number, number]>()
    .curve(curveBumpX)
    .x(([pointX]) => pointX)
    .y(([, pointY]) => pointY);
  const labels = lanes.map((lane) => lane.label);
  const yScale = scalePoint<string>()
    .domain(labels)
    .range([92, graphHeight - 92])
    .padding(0.4);

  return lanes.map((lane) => {
    const y = yScale(lane.label) ?? graphHeight / 2;
    return {
      lane,
      pathToEnvironment:
        pathLine([
          [514, y],
          [582, y]
        ]) ?? "",
      pathToLane:
        pathLine([
          [234, graphHeight / 2],
          [330, y]
        ]) ?? "",
      y
    };
  });
}

function environmentLabel(lane: ServiceDeploymentLane): string {
  if (lane.environments.length === 0) {
    return "environment pending";
  }
  return lane.environments.join(", ");
}

function labelLines(label: string, maxLength: number): readonly string[] {
  const words = label.split(/\s+/).flatMap((word) => splitLongWord(word));
  const lines: string[] = [];
  let current = "";
  for (const word of words) {
    const candidate = current.length === 0 ? word : `${current} ${word}`;
    if (candidate.length <= maxLength) {
      current = candidate;
      continue;
    }
    if (current.length > 0) {
      lines.push(current);
    }
    current = word;
  }
  if (current.length > 0) {
    lines.push(current);
  }
  return lines.length > 0 ? lines : [label];
}

function splitLongWord(word: string): readonly string[] {
  if (word.length <= 18) {
    return [word];
  }
  return word
    .replaceAll("-", "- ")
    .replaceAll("_", "_ ")
    .split(/\s+/)
    .filter((token) => token.length > 0);
}

function labelYOffset(label: string): number {
  return ((labelLines(label, 22).length - 1) * 17) / 2 - 5;
}
