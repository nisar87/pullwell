// The store icon (128x128, the artwork 96x96 inside 16 px of transparent margin) and the marquee promo tile (1400x560),
// made from the extension's own icon. Usage: node scripts/store/tiles.mjs <out-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
let playwright; for (const base of [import.meta.url, '/opt/']) { try { playwright = createRequire(base)('playwright'); break; } catch {} }
const {chromium} = playwright;
const out = process.argv[2];
const here = path.dirname(fileURLToPath(import.meta.url));
const icon = fs.readFileSync(path.join(here, '../../extension/icons/128.png')).toString('base64');
fs.mkdirSync(out, {recursive: true});
const b = await chromium.launch(); const p = await b.newPage();
await p.setViewportSize({width: 128, height: 128});
await p.setContent(`<body style="margin:0;background:transparent"><img src="data:image/png;base64,${icon}" style="position:absolute;left:16px;top:16px;width:96px;height:96px"></body>`);
await p.screenshot({path: out + '/store-icon-128x128.png', omitBackground: true});
await p.setViewportSize({width: 1400, height: 560});
await p.setContent(`<body style="margin:0;width:1400px;height:560px;display:flex;align-items:center;justify-content:center;gap:56px;background:linear-gradient(120deg,#0b2224,#14464a 60%,#1a5a5a);font-family:'Segoe UI',system-ui,sans-serif;color:#fff">
<img src="data:image/png;base64,${icon}" width="200" height="200" style="border-radius:44px;box-shadow:0 24px 60px rgba(0,0,0,.45)">
<div><div style="font-size:84px;font-weight:700;letter-spacing:-.02em;line-height:1">Pullwell Downloads</div>
<div style="margin-top:22px;font-size:34px;color:#cfe9e6;line-height:1.35;max-width:760px">Send big downloads and videos to Pullwell. Several connections, resume, and you confirm before anything starts.</div></div></body>`);
await p.screenshot({path: out + '/marquee-promo-1400x560.png'});
await b.close(); console.log('done');
