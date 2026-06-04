/* Eshu Console — icon set (stroke SVG). Exports window.Icon = {name: component}. */
function mkIcon(paths, opts) {
  return function (props) {
    const s = (props && props.size) || 18;
    return (
      <svg width={s} height={s} viewBox="0 0 24 24" fill="none" stroke="currentColor"
        strokeWidth={(opts && opts.sw) || 1.7} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {paths}
      </svg>
    );
  };
}

const Icon = {
  dashboard: mkIcon(<><rect x="3" y="3" width="7" height="9" rx="1.5" /><rect x="14" y="3" width="7" height="5" rx="1.5" /><rect x="14" y="12" width="7" height="9" rx="1.5" /><rect x="3" y="16" width="7" height="5" rx="1.5" /></>),
  graph: mkIcon(<><circle cx="5" cy="6" r="2.4" /><circle cx="19" cy="6" r="2.4" /><circle cx="12" cy="18" r="2.4" /><path d="M7.1 7.1 10.6 16M16.9 7.1 13.4 16M7.4 6h9.2" /></>),
  catalog: mkIcon(<><path d="M4 5.5C4 4.7 4.7 4 5.5 4H10l2 2h6.5c.8 0 1.5.7 1.5 1.5V18c0 .8-.7 1.5-1.5 1.5h-13C4.7 19.5 4 18.8 4 18z" /><path d="M4 9h16" /></>),
  findings: mkIcon(<><path d="M12 3 3 7v5c0 5 3.8 8 9 9 5.2-1 9-4 9-9V7z" /><path d="M12 8v4M12 15.5v.5" /></>),
  vuln: mkIcon(<><path d="M12 3 3 7v5c0 5 3.8 8 9 9 5.2-1 9-4 9-9V7z" /><path d="m9.5 11.5 1.7 1.7 3.3-3.4" /></>),
  admin: mkIcon(<><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.6 1.6 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.6 1.6 0 0 0-1.8-.3 1.6 1.6 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.6 1.6 0 0 0-1-1.5 1.6 1.6 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.6 1.6 0 0 0 .3-1.8 1.6 1.6 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.6 1.6 0 0 0 1.5-1 1.6 1.6 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.6 1.6 0 0 0 1.8.3H9a1.6 1.6 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.6 1.6 0 0 0 1 1.5 1.6 1.6 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.6 1.6 0 0 0-.3 1.8V9a1.6 1.6 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.6 1.6 0 0 0-1.5 1z" /></>),
  search: mkIcon(<><circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" /></>),
  bell: mkIcon(<><path d="M18 8a6 6 0 1 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" /><path d="M13.7 21a2 2 0 0 1-3.4 0" /></>),
  pulse: mkIcon(<><path d="M3 12h4l2-6 4 12 2-6h6" /></>),
  layers: mkIcon(<><path d="m12 3 9 5-9 5-9-5z" /><path d="m3 13 9 5 9-5M3 17l9 5 9-5" /></>),
  box: mkIcon(<><path d="m12 3 8 4.5v9L12 21l-8-4.5v-9z" /><path d="m12 21V12M4 7.5 12 12l8-4.5" /></>),
  shield: mkIcon(<><path d="M12 3 4 6.5V12c0 4.5 3.2 7.5 8 9 4.8-1.5 8-4.5 8-9V6.5z" /></>),
  cloud: mkIcon(<><path d="M6.5 18a4.5 4.5 0 0 1-.4-9 6 6 0 0 1 11.5 1.5 3.5 3.5 0 0 1-.6 7z" /></>),
  branch: mkIcon(<><circle cx="6" cy="6" r="2.2" /><circle cx="6" cy="18" r="2.2" /><circle cx="18" cy="8" r="2.2" /><path d="M6 8.2v7.6M8.2 6.6c6 .6 8 1.6 8 4.4v-1.6" /></>),
  clock: mkIcon(<><circle cx="12" cy="12" r="8.5" /><path d="M12 7v5l3 2" /></>),
  external: mkIcon(<><path d="M14 4h6v6M20 4l-9 9M19 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h5" /></>),
  close: mkIcon(<><path d="m6 6 12 12M18 6 6 18" /></>),
  filter: mkIcon(<><path d="M3 5h18l-7 8v6l-4-2v-4z" /></>),
  arrow: mkIcon(<><path d="M5 12h14M13 6l6 6-6 6" /></>),
  db: mkIcon(<><ellipse cx="12" cy="5.5" rx="8" ry="3" /><path d="M4 5.5v13c0 1.6 3.6 3 8 3s8-1.4 8-3v-13M4 12c0 1.6 3.6 3 8 3s8-1.4 8-3" /></>),
  spark: mkIcon(<><path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5 18 18M18 6l-2.5 2.5M8.5 15.5 6 18" /></>),
  bolt: mkIcon(<><path d="M13 3 4 14h7l-1 7 9-11h-7z" /></>)
};

window.Icon = Icon;
