// Makes the Chrome Web Store pictures (1280x800 screenshots and the 440x280 tile) from the real extension and the real
// window, against a running program on 127.0.0.1:9197 and the test fixtures on :8765.
// Usage: node scripts/store/shots.mjs <work-dir> <out-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
import {execSync} from 'node:child_process';
let playwright; for (const base of [import.meta.url, '/opt/']) { try { playwright = createRequire(base)('playwright'); break; } catch {} }
const {chromium} = playwright;
const [work, out] = process.argv.slice(2);
const app = 'http://127.0.0.1:9197/', fx = 'http://127.0.0.1:8765';
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const post = b => fetch(app + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)}).then(r => r.json());
const sleep = ms => new Promise(r => setTimeout(r, ms));
fs.rmSync(work, {recursive: true, force: true}); fs.mkdirSync(work + '/ext', {recursive: true}); fs.mkdirSync(out, {recursive: true});
fs.writeFileSync(work + '/ext.zip', Buffer.from(await (await fetch(app + 'extension/Pullwell-extension-chrome-edge.zip')).arrayBuffer()));
execSync(`python3 -c "import zipfile;zipfile.ZipFile('../ext.zip').extractall('.')"`, {cwd: work + '/ext'});
const mf = JSON.parse(fs.readFileSync(work + '/ext/manifest.json', 'utf8')); mf.host_permissions.push('<all_urls>'); delete mf.optional_host_permissions;
fs.writeFileSync(work + '/ext/manifest.json', JSON.stringify(mf));
const ctx = await chromium.launchPersistentContext(work + '/profile', {headless: true, channel: 'chromium', viewport: {width: 1280, height: 800}, deviceScaleFactor: 1,
  args: [`--disable-extensions-except=${work}/ext`, `--load-extension=${work}/ext`, '--host-resolver-rules=MAP files.example.test 127.0.0.1:8765']});
const sw = ctx.serviceWorkers()[0] || await ctx.waitForEvent('serviceworker');
const extId = sw.url().split('/')[2];
// pair
const code = (await post({action: 'extCode'})).code;
const pop = await ctx.newPage(); await pop.goto(`chrome-extension://${extId}/popup.html`); await sleep(800);
await pop.fill('#code', code); await pop.click('#pair'); await sleep(1500);
await sw.evaluate(() => refresh());
// 1. the popup, on a plain backdrop
await pop.setViewportSize({width: 340, height: 520}); await pop.reload(); await sleep(1200);
const popPng = await pop.screenshot({fullPage: true});
const card = await ctx.newPage(); await card.setViewportSize({width: 1280, height: 800});
const back = (title, sub, img) => `<body style="margin:0;width:1280px;height:800px;display:flex;align-items:center;justify-content:center;gap:70px;background:linear-gradient(135deg,#0e2a2c,#16474a);font-family:'Segoe UI',system-ui,sans-serif;color:#fff">${img}<div style="max-width:520px"><div style="font-size:46px;font-weight:700;line-height:1.15;letter-spacing:-.01em">${title}</div><div style="margin-top:18px;font-size:22px;line-height:1.45;color:#cfe9e6">${sub}</div></div></body>`;
await card.setContent(back('Send big downloads to Pullwell', 'Connect once with a code. Big files go to Pullwell, which splits them into parts and never loses a file. Small ones stay in the browser.',
  `<img src="data:image/png;base64,${popPng.toString('base64')}" style="border-radius:10px;box-shadow:0 20px 60px rgba(0,0,0,.5);max-height:680px">`));
await card.screenshot({path: out + '/screenshot-1-connect.png'});
// 2. the video button
const vp = await ctx.newPage(); await vp.setViewportSize({width: 1280, height: 800});
await vp.route('http://files.example.test/watch', r => r.fulfill({contentType: 'text/html', body: `<body style="margin:0;background:#101418;font-family:'Segoe UI',system-ui,sans-serif;color:#e8eef2"><div style="max-width:960px;margin:60px auto"><div style="font-size:26px;font-weight:600;margin-bottom:14px">Mountain lake, 4K timelapse</div><video controls poster="" src="http://files.example.test/file/23MB" style="width:960px;height:540px;display:block;border-radius:8px;background:linear-gradient(160deg,#2f5d73,#1b2b3a 55%,#3a2f4d)"></video></div></body>`}));
await vp.goto('http://files.example.test/watch'); await sleep(2500);
await vp.mouse.move(500, 300); await sleep(400); await vp.mouse.move(520, 320); await sleep(700);
await vp.screenshot({path: out + '/screenshot-2-video-button.png'});
// 3. the box, in the program's window (a sample file from the test server; the test copy's badge and folder are tidied)
await post({action: 'add', url: fx + '/file/66MB', name: 'go1.27.3.windows-amd64.msi', force: true});
await post({action: 'add', url: fx + '/file/14MB', name: 'holiday-photos.zip', force: true});
const win = await ctx.newPage(); await win.setViewportSize({width: 1280, height: 800}); await win.goto(app); await sleep(4000);
await win.evaluate((u) => openAsk({url: u, name: 'ubuntu-26.04-desktop-amd64.iso', header: {}}), fx + '/file/70MB'); await sleep(2000);
await win.evaluate(() => { const b = document.querySelector('.br .tag, .brand .tag, .copytag'); document.querySelectorAll('*').forEach(e => { if (!e.children.length && e.textContent.trim() === 'Separate copy') e.remove(); }); const f = document.querySelector('#dlFolder'); if (f) f.value = 'C:\\Users\\You\\Downloads'; document.querySelectorAll('#status *, .status *').forEach(e => { if (/Saving to/.test(e.textContent) && !e.children.length) e.textContent = 'Saving to C:\\Users\\You\\Downloads'; }); });
await win.screenshot({path: out + '/screenshot-3-confirm.png'});
// the small tile
const icon = fs.readFileSync(work + '/ext/icons/128.png').toString('base64');
const tile = await ctx.newPage(); await tile.setViewportSize({width: 440, height: 280});
await tile.setContent(`<body style="margin:0;width:440px;height:280px;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:14px;background:linear-gradient(135deg,#0e2a2c,#16474a);font-family:'Segoe UI',system-ui,sans-serif;color:#fff"><img src="data:image/png;base64,${icon}" width="84" height="84"><div style="font-size:30px;font-weight:700">Pullwell Downloads</div><div style="font-size:16px;color:#cfe9e6">Big downloads, split into parts</div></body>`);
await tile.screenshot({path: out + '/promo-small-440x280.png'});
await ctx.close(); console.log('done'); process.exit(0);
