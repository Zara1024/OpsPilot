// OpsPilot brand mark: a control-plane P with a connected route node.
// Flat colours keep it legible at favicon size.

type Props = {
  size?: number;
  className?: string;
  title?: string;
};

export function OpsPilotLogo({ size = 28, className, title = 'OpsPilot' }: Props) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 512 512"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      className={className}
      role="img"
      aria-label={title}
    >
      <title>{title}</title>
      <rect x="28" y="28" width="456" height="456" rx="112" fill="#17252B" />
      <path d="M146 392V120H266C323 120 358 151 358 202C358 253 323 284 266 284H203" stroke="#42D3B2" strokeWidth="48" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M358 202H407" stroke="#42D3B2" strokeWidth="20" strokeLinecap="round" />
      <circle cx="424" cy="202" r="25" fill="#FFB454" />
      <circle cx="146" cy="392" r="16" fill="#D8FFF5" />
    </svg>
  );
}
