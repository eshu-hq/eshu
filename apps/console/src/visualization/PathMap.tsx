interface PathMapProps {
  readonly nodes: readonly string[];
}

export function PathMap({ nodes }: PathMapProps): React.JSX.Element {
  return (
    <ol className="path-map">
      {nodes.map((node) => (
        <li key={node}>{node}</li>
      ))}
    </ol>
  );
}
