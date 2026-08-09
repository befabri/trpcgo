// Generates public/og.png, the Open Graph / Twitter card for the site.
// Run from the docs/ directory:  node scripts/generate-og.mjs
//
// The card is authored as SVG using the landing page's dark tokens and the real
// logo paths, then rasterized with sharp — social platforms do not render SVG,
// so a PNG is required.

import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import sharp from 'sharp';

const docsDir = join(dirname(fileURLToPath(import.meta.url)), '..');

// Pull the glyph paths out of the real logo so the card can never drift from the mark.
const logo = readFileSync(join(docsDir, 'src/assets/logo.svg'), 'utf8');
const logoInner = logo.match(/<g transform="translate\(0 528\) scale\(\.1 -\.1\)">[\s\S]*<\/g>/)[0];

// Palette lifted verbatim from the landing page's dark tokens.
const BG = '#17181c'; // hsl(224 10% 10%)
const HEADING = '#ffffff';
const TEXT = '#c1c3c8'; // hsl(224 6% 77%)
const MUTED = '#8b8f99'; // hsl(224 6% 56%)
const BLUE = '#0898f0'; // logo blue
const TEAL = '#14bfc8'; // logo teal

const FONT = "'Liberation Sans', 'Helvetica Neue', Arial, sans-serif";

const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
  <defs>
    <linearGradient id="accent" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0" stop-color="${BLUE}"/>
      <stop offset="1" stop-color="${TEAL}"/>
    </linearGradient>
  </defs>

  <rect width="1200" height="630" fill="${BG}"/>

  <g transform="translate(80 70) scale(0.2247)">${logoInner}</g>

  <text x="80" y="330" fill="${HEADING}" font-family="${FONT}" font-size="76" font-weight="700" letter-spacing="-1.5">Go on the server.</text>
  <text x="80" y="422" fill="${HEADING}" font-family="${FONT}" font-size="76" font-weight="700" letter-spacing="-1.5">tRPC on the client.</text>

  <text x="80" y="496" fill="${TEXT}" font-family="${FONT}" font-size="30">Generated TypeScript contracts, Zod schemas, and runtime enums.</text>

  <text x="80" y="566" fill="${MUTED}" font-family="${FONT}" font-size="26">trpcgo.dev</text>

  <rect x="0" y="622" width="1200" height="8" fill="url(#accent)"/>
</svg>`;

const out = join(docsDir, 'public/og.png');
await sharp(Buffer.from(svg)).png({ compressionLevel: 9 }).toFile(out);

const meta = await sharp(out).metadata();
console.log(`wrote public/og.png — ${meta.width}x${meta.height}, ${(meta.size / 1024).toFixed(1)}KB`);
