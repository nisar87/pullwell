// The extension's popup in a REAL popup window (not a tab): Chrome sizes a popup from its content, so a width that
// depends on the window ("100vw") shrinks it to a sliver. A tab cannot show that. This opens the popup the way the
// toolbar button does, in each state and both colour schemes, measures it and keeps a picture of each.
// Needs a display (a Windows runner has one; on Linux run it under xvfb-run).
// Usage: node popup_window_check.mjs <built-program> <work-dir> <shots-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {execSync, spawn} from 'node:child_process';
let playwright;
for (const base of [import.meta.url, '/opt/']) { try { playwright = createRequire(base)('playwright'); break; } catch {} }
if (!playwright) { console.error('playwright is not installed'); process.exit(2); }
const {chromium} = playwright;
const WIN = process.platform === 'win32', EXE = WIN ? 'pw.exe' : 'pw';
const [program0, work0, shots0] = process.argv.slice(2);
const program = path.resolve(program0), work = path.resolve(work0).replace(/\\/g, '/'), shots = path.resolve(shots0);
const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const URL0 = 'http://127.0.0.1:9197/', H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const results = []; const check = (n, ok, x) => results.push(`${ok ? 'PASS' : 'FAIL'}  ${n}${x !== undefined ? '  (' + x + ')' : ''}`);
const sleep = ms => new Promise(r => setTimeout(r, ms));
const up = async () => { try { return (await (await fetch(URL0 + 'ext/ping')).json()).app === 'pullwell'; } catch { return false; } };
const closed = async () => { try { await fetch(URL0 + 'ext/ping'); return false; } catch { return true; } };
const waitFor = async (f, ms) => { for (let t = 0; t < ms; t += 200) { if (await f()) return true; await sleep(200); } return false; };
const post = (p, b) => fetch(URL0 + p, {method: 'POST', headers: H, body: JSON.stringify(b)}).then(r => r.json());
if (await up()) { console.error('a program already answers on port 9197: stop it first'); process.exit(2); }

fs.rmSync(work, {recursive: true, force: true}); fs.mkdirSync(work + '/app/data', {recursive: true}); fs.mkdirSync(shots, {recursive: true});
fs.copyFileSync(program, work + '/app/' + EXE); if (!WIN) fs.chmodSync(work + '/app/' + EXE, 0o755);
fs.writeFileSync(work + '/app/data/app.json', '{"diagNoticed":true,"noDiag":true}');
const py = WIN ? 'python' : 'python3';
execSync(`${py} extension/build.py`, {cwd: repo, stdio: 'ignore'});
fs.mkdirSync(work + '/ext', {recursive: true});
execSync(`${py} -c "import zipfile;zipfile.ZipFile('${repo.replace(/\\/g, '/')}/extension/dist/Pullwell-extension-chrome-edge.zip').extractall('${work}/ext')"`);
const startApp = () => { spawn(work + '/app/' + EXE, ['-tray'], {detached: true, stdio: 'ignore'}).unref(); return waitFor(up, 30000); };
const quit = async () => { try { await post('api/app', {quit: true}); } catch {} await waitFor(closed, 15000); };
await startApp();

const PORT = 9444;
const ctx = await chromium.launchPersistentContext(work + '/profile', {headless: false, channel: 'chromium', acceptDownloads: true,
  args: [`--disable-extensions-except=${work}/ext`, `--load-extension=${work}/ext`, '--window-size=1200,800', `--remote-debugging-port=${PORT}`]});
const sw = ctx.serviceWorkers()[0] || await ctx.waitForEvent('serviceworker', {timeout: 15000});
const extId = sw.url().split('/')[2];
const tab = await ctx.newPage(); await tab.goto('about:blank');

// the popup, opened like the toolbar button: returns a function to talk to it
async function openPopup() {
  await tab.bringToFront();
  await sw.evaluate(() => chrome.action.openPopup());
  let t;
  for (let i = 0; i < 40 && !t; i++) { await sleep(250); t = (await (await fetch(`http://127.0.0.1:${PORT}/json/list`)).json()).find(x => x.url.includes('popup.html')); }
  if (!t) throw new Error('the popup did not open');
  const ws = new WebSocket(t.webSocketDebuggerUrl); await new Promise(r => ws.addEventListener('open', r));
  let n = 0; const pend = {};
  ws.addEventListener('message', m => { const j = JSON.parse(m.data); if (pend[j.id]) pend[j.id](j.result); });
  const send = (method, params = {}) => new Promise(r => { pend[++n] = r; ws.send(JSON.stringify({id: n, method, params})); });
  return {send, id: t.id, close: async () => { ws.close(); try { await fetch(`http://127.0.0.1:${PORT}/json/close/${t.id}`); } catch {} await sleep(400); }};
}
const evalIn = async (p, expr) => (await p.send('Runtime.evaluate', {expression: expr, returnByValue: true, awaitPromise: true})).result.value;

// pair once (in a tab: only the pairing needs typing), then look at each state in the real popup
const pop = await ctx.newPage(); await pop.goto(`chrome-extension://${extId}/popup.html`);
// the starter pairs it by itself; the code is only the way in for a browser without a starter
await pop.waitForSelector('#v-on:not([hidden]), #v-pair:not([hidden])', {timeout: 20000});
if (await pop.locator('#v-pair').isVisible()) { const code = (await post('api/downloads', {action: 'extCode'})).code; await pop.fill('#code', String(code)); await pop.click('#pair'); await sleep(2000); }
await pop.close();
check('the test browser is paired', await sw.evaluate(async () => !!(await chrome.storage.local.get('token')).token));

async function state(name, ready, schemes = ['dark', 'light']) {
  for (const scheme of schemes) {
    const p = await openPopup();
    await p.send('Emulation.setEmulatedMedia', {features: [{name: 'prefers-color-scheme', value: scheme}]});
    // wait until the popup shows the view that this state is about
    for (let i = 0; i < 40 && !(await evalIn(p, ready)); i++) await sleep(250);
    await sleep(400);
    const m = JSON.parse(await evalIn(p, `JSON.stringify((() => { const h = document.querySelector('h1'), cs = getComputedStyle(h); const labels = [...document.querySelectorAll('label.row')].filter(l => l.offsetParent).map(l => l.getBoundingClientRect().width);
      return { iw: innerWidth, ih: innerHeight, sw: document.documentElement.scrollWidth, h1: h.getBoundingClientRect().height, lh: parseFloat(cs.lineHeight) || parseFloat(cs.fontSize) * 1.3, labels, shown: [...document.querySelectorAll('.v')].filter(v => !v.hidden).map(v => v.id) }; })())`));
    const tag = `${name} (${scheme})`;
    check(`${tag}: the popup is 350 px wide`, m.iw >= 340 && m.iw <= 360, m.iw);
    check(`${tag}: nothing sticks out sideways`, m.sw <= m.iw, `${m.sw} in ${m.iw}`);
    check(`${tag}: the title fits on one line`, m.h1 < m.lh * 1.6, `${Math.round(m.h1)} px high`);
    check(`${tag}: the options are not squeezed`, m.labels.every(w => w >= 250), m.labels.map(Math.round).join(','));
    check(`${tag}: the popup is not cut off`, m.ih >= 150 && m.ih <= 620, m.ih);
    const s = await p.send('Page.captureScreenshot', {format: 'png'});
    fs.writeFileSync(`${shots}/popup-${name.replace(/ /g, '-')}-${scheme}.png`, Buffer.from(s.data, 'base64'));
    await p.close();
  }
}

await state('connected', `!document.getElementById('v-on').hidden`);
await quit();
await state('program closed', `!document.getElementById('v-down').hidden`, ['dark', 'light']);
await startApp();
await sw.evaluate(() => chrome.storage.local.remove(['token']));
// no way to connect by itself (the starter cannot read the key): the popup asks for the code
const keyFile = work + '/app/data/host.key'; fs.renameSync(keyFile, keyFile + '.off');
await state('not paired', `!document.getElementById('v-pair').hidden`, ['dark', 'light']);

fs.renameSync(keyFile + '.off', keyFile);
await quit(); await ctx.close();
console.log(results.join('\n'));
const fail = results.filter(r => r.startsWith('FAIL')).length;
console.log(`${results.length - fail}/${results.length} passed`);
process.exit(fail ? 1 : 0);
