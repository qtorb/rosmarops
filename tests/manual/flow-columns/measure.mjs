// measure.mjs (BV-141 visual check): load each example report, confirm the
// three `.flow` columns start at the same height at 1280 px (max − min top
// ≤ 2 px), and save screenshots at 1280 and 390 px. Manual helper, not CI.
//
//   node tests/manual/flow-columns/measure.mjs <htmlDir> [shotDir]
//
// Chromium is pre-installed in the build container; from anywhere,
// require('playwright') / chromium.launch() works with no options.
import { readdirSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { join, resolve } from 'node:path';
import { createRequire } from 'node:module';

// Chromium + a matching Playwright are pre-installed in the build container;
// require('playwright') resolves from any directory (ESM needs createRequire).
const { chromium } = createRequire(import.meta.url)('playwright');

const htmlDir = resolve(process.argv[2] || 'flowcols-out');
const shotDir = resolve(process.argv[3] || join(htmlDir, 'shots'));
import { mkdirSync } from 'node:fs';
mkdirSync(shotDir, { recursive: true });

const files = readdirSync(htmlDir).filter((f) => f.endsWith('.html')).sort();
const browser = await chromium.launch();
let failed = false;

for (const file of files) {
  const url = pathToFileURL(join(htmlDir, file)).href;

  // 1280 px: measure the three flow columns' top edge.
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await page.goto(url);
  const tops = await page.evaluate(() => {
    const flows = document.querySelectorAll('div.flow');
    if (flows.length !== 1) return { error: `flow count ${flows.length}` };
    const kids = flows[0].children; // direct children = the three steps
    return { tops: Array.from(kids).map((el) => Math.round(el.getBoundingClientRect().top)) };
  });
  if (tops.error) {
    console.log(`${file}: FAIL — ${tops.error}`);
    failed = true;
  } else {
    const spread = Math.max(...tops.tops) - Math.min(...tops.tops);
    const ok = tops.tops.length === 3 && spread <= 2;
    console.log(`${file} @1280: column tops = [${tops.tops.join(', ')}] px, spread ${spread}px — ${ok ? 'OK' : 'FAIL'}`);
    if (!ok) failed = true;
  }
  await page.screenshot({ path: join(shotDir, file.replace('.html', '.1280.png')), fullPage: true });
  await page.close();

  // 390 px: phone width (below the 1100 px breakpoint, columns stack).
  const narrow = await browser.newPage({ viewport: { width: 390, height: 900 } });
  await narrow.goto(url);
  await narrow.screenshot({ path: join(shotDir, file.replace('.html', '.390.png')), fullPage: true });
  await narrow.close();
}

await browser.close();
console.log(failed ? '\nRESULT: FAIL' : '\nRESULT: OK (all columns aligned within 2px)');
process.exit(failed ? 1 : 0);
