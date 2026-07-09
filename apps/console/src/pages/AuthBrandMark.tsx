// AuthBrandMark.tsx — the elevated node-graph brand mark used on the auth
// surfaces (LoginPage, SetupPage). Distinct from the sidebar's plain
// three-bar glyph (App.tsx's `.brand-mark.brand-glyph`): the auth screens are
// an operator's first impression of the product, so the approved mockup uses
// a more detailed mark there. Colors are inline SVG fills matching the
// existing --teal/--ember/--bone token values (SVG attributes cannot
// reference CSS custom properties portably across every renderer this shell
// runs in, so the hexes mirror styles.css's :root exactly).
export function AuthBrandMark({ size = 44 }: { readonly size?: number }): React.JSX.Element {
  return (
    <svg
      className="auth-brand-mark"
      width={size}
      height={size}
      viewBox="0 0 44 44"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
    >
      <rect
        x="1.5"
        y="1.5"
        width="41"
        height="41"
        rx="11"
        fill="#10171f"
        stroke="rgba(243,235,221,.14)"
      />
      <path d="M13 11 V33" stroke="#14b8a6" strokeWidth="2" strokeLinecap="round" />
      <path d="M31 11 V33" stroke="#ff8a00" strokeWidth="2" strokeLinecap="round" />
      <path
        d="M13 13 L31 22 M13 22 L31 31 M13 31 L31 13"
        stroke="rgba(243,235,221,.28)"
        strokeWidth="1.4"
        strokeLinecap="round"
      />
      <circle cx="13" cy="13" r="3.2" fill="#2dd4bf" />
      <circle cx="13" cy="22" r="3.2" fill="#2dd4bf" />
      <circle cx="13" cy="31" r="3.2" fill="#2dd4bf" />
      <circle cx="31" cy="13" r="3.2" fill="#ff8a00" />
      <circle cx="31" cy="22" r="3.2" fill="#ff8a00" />
      <circle cx="31" cy="31" r="3.2" fill="#ff8a00" />
    </svg>
  );
}
