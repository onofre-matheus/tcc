// Mascote: elefante de óculos, em SVG inline (sem asset externo — a CSP do
// Manifest V3 bloquearia recursos remotos de qualquer forma).
export const MASCOT_SVG = `
<svg class="mascot" viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
  <ellipse cx="32" cy="54" rx="17" ry="5" fill="currentColor" opacity="0.08"/>
  <path d="M14 30c0-11 8-19 18-19s18 8 18 19c0 7-3 12-3 16 0 3-3 4-6 4-2 0-3-1-3-3v-4c-2 1-4 1-6 1s-4 0-6-1v4c0 2-1 3-3 3-3 0-6-1-6-4 0-4-3-9-3-16z" fill="#9a8c98"/>
  <path d="M14 27c-4-1-8 1-8 6 0 4 3 6 7 5" stroke="#9a8c98" stroke-width="4" stroke-linecap="round"/>
  <path d="M50 27c4-1 8 1 8 6 0 4-3 6-7 5" stroke="#9a8c98" stroke-width="4" stroke-linecap="round"/>
  <path d="M31 40c0 4 0 9-3 12-2 2-5 2-6 0" stroke="#9a8c98" stroke-width="4" stroke-linecap="round" fill="none"/>
  <circle cx="25" cy="30" r="6" fill="#fff"/>
  <circle cx="39" cy="30" r="6" fill="#fff"/>
  <circle cx="25" cy="30" r="2.4" fill="#2b2a26"/>
  <circle cx="39" cy="30" r="2.4" fill="#2b2a26"/>
  <g stroke="#c96442" stroke-width="2" fill="none">
    <circle cx="25" cy="30" r="7.5"/>
    <circle cx="39" cy="30" r="7.5"/>
    <path d="M32.5 29h-1M17.5 29c-1.5-1-3-1-4 0M46.5 29c1.5-1 3-1 4 0"/>
  </g>
</svg>`;
