// Screenshots of every screen of the window for a visual review: both themes, four window sizes, three text sizes,
// compact rows, the menus, the right-click menu, the progress window and every dialog. It also records which of the
// window's fonts are really there (fonts.json), so a review knows whether it is looking at the Windows fonts.
// Works against headless Chrome and against the real WebView2 window (started with a remote debugging port).
// Usage: node shots.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
fs.mkdirSync(out, {recursive: true});
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));

// the same realistic list every time: finished, running, paused and failed downloads
const add = async (path, name) => call({action: 'add', url: `${fx}${path}`, name, force: true});
await add('/file/66MB', 'go1.27.3.windows-amd64.msi');
await add('/file/8MB', 'annual-report-2026.pdf');
await add('/file/14MB', 'holiday-photos.zip');
await add('/file/3MB', 'podcast-episode-112.mp3');
await sleep(5000);
await add('/slow/400MB?bps=900000', 'ubuntu-26.04-desktop-amd64.iso');
await add('/slow/300MB?bps=500000', 'big-buck-bunny-1080p.mp4');
await sleep(2000);
const L = (await (await fetch(url + 'api/downloads')).json()).items;
await call({action: 'pause', id: L.find(v => /bunny/.test(v.name)).id});
await add('/status404x/1MB', 'old-driver-setup.exe');
await sleep(4000);

const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const page = targets.find(t => t.type === 'page' && t.url.startsWith(url)) || targets.find(t => t.type === 'page');
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise(r => ws.onopen = r);
let id = 0; const wait = new Map(); const errors = [];
ws.onmessage = ev => {
  const m = JSON.parse(ev.data);
  if (m.id && wait.has(m.id)) { wait.get(m.id)(m); wait.delete(m.id); return; }
  if (m.method === 'Runtime.exceptionThrown') errors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text);
};
const send = (method, params = {}) => new Promise(r => { const i = ++id; wait.set(i, r); ws.send(JSON.stringify({id: i, method, params})); });
const js = async expr => (await send('Runtime.evaluate', {expression: expr, awaitPromise: true, returnByValue: true})).result?.result?.value;
const shots = [];
const shot = async name => {
  await js(`document.querySelectorAll('#toast').forEach(t => t.style.display = 'none')`);
  const r = await send('Page.captureScreenshot', {format: 'png'});
  fs.writeFileSync(`${out}/${name}.png`, Buffer.from(r.result.data, 'base64')); shots.push(name);
};
const size = async (w, h) => send('Emulation.setDeviceMetricsOverride', {width: w, height: h, deviceScaleFactor: 1, mobile: false});
const theme = async t => send('Emulation.setEmulatedMedia', {features: [{name: 'prefers-color-scheme', value: t}]});
const pick = async () => { await js(`(() => { const r = [...document.querySelectorAll('#tb tr')].find(r => /ubuntu/.test(r.textContent)); if (r) { r.dispatchEvent(new MouseEvent('mousedown', {bubbles: true})); r.click(); } })()`); await sleep(300); };
const U = n => `DL.items.find(v => /${n}/.test(v.name))`;
await send('Runtime.enable'); await send('Page.enable');
if (!page.url.startsWith(url)) { await send('Page.navigate', {url}); await sleep(2500); } // headless Chrome starts on about:blank

// which fonts are really there: a font that is missing falls back, so its text measures like the fallback
const fonts = await js(`(() => {
  const c = document.createElement('canvas').getContext('2d'), s = 'Pullwell 0123456789 MB/s Wgiyl';
  const w = f => { c.font = '32px ' + f; return c.measureText(s).width; };
  const has = f => ['monospace', 'serif'].some(b => w('"' + f + '", ' + b) !== w(b));
  const cs = getComputedStyle(document.body);
  return {ua: navigator.userAgent, sans: cs.getPropertyValue('--sans').trim(), num: cs.getPropertyValue('--num').trim(),
    available: Object.fromEntries(['Segoe UI Variable Text', 'Segoe UI Variable Display', 'Segoe UI', 'Bahnschrift', 'Cascadia Mono', 'Consolas'].map(f => [f, has(f)]))};
})()`);
fs.writeFileSync(`${out}/fonts.json`, JSON.stringify(fonts, null, 2));

const dialogs = [
  ['add', `addDialog('')`],
  ['probe', `(async () => { const p = await api('downloads', {action:'probe', url:'${fx}/file/8MB'}).catch(() => ({})); showProbe('${fx}/file/8MB', p); })()`],
  ['props', `dlProps(${U('ubuntu')}.id)`],
  ['rename', `renameBox(${U('holiday')})`], ['move', `moveBox(${U('holiday')})`], ['checksum', `checksumBox(${U('holiday')})`],
  ['confirm', `askBox({title:'Delete 2 downloads?', text:'The files stay on your disk.', ok:'Delete', danger:true, check:'Also delete the files'})`],
  ['queues', `dlQueues()`], ['grab', `dlGrab()`], ['folders', `dlFolders()`], ['browser', `extConnect()`],
  ['options', `showOptions('general')`], ['options-connection', `showOptions('connection')`], ['options-updates', `showOptions('updates')`],
  ['about', `aboutBox()`], ['shortcuts', `shortcutsBox()`],
];
for (const t of ['light', 'dark']) {
  await theme(t); await size(1280, 720); await send('Page.reload'); await sleep(3000); await pick();
  await shot(`main-${t}-1280x720`);
  for (const [w, h] of [[980, 560], [1366, 768], [1920, 1080]]) { await size(w, h); await sleep(500); await shot(`main-${t}-${w}x${h}`); }
  await size(1280, 720); await sleep(300);
  for (const m of ['tasks', 'downloads', 'view']) { await js(`document.querySelector('#menubar [data-mb="${m}"]').click()`); await sleep(400); await shot(`menu-${m}-${t}`); await js(`closeMenu()`); }
  const row = await js(`(() => { const r = [...document.querySelectorAll('#tb tr')].find(r => /holiday/.test(r.textContent)).getBoundingClientRect(); return [r.left + 120, r.top + r.height / 2]; })()`);
  for (const type of ['mousePressed', 'mouseReleased']) await send('Input.dispatchMouseEvent', {type, x: row[0], y: row[1], button: 'right', clickCount: 1});
  await sleep(400); await shot(`context-menu-${t}`); await js(`closeMenu()`);
  await js(`progressBox(${U('ubuntu')}.id)`); await sleep(3000); await shot(`progress-${t}`); await sleep(2000); await shot(`progress-${t}-later`); await js(`dlClose()`);
  for (const [n, e] of dialogs) { await js(`void (${e}); 1`); await sleep(900); await shot(`dialog-${n}-${t}`); await js(`dlClose()`); await sleep(200); }
}
await theme('light');
for (const z of [0.9, 1.15, 1.3]) { await js(`store.set('zoom', ${z}); applyZoom()`); await sleep(600); await shot(`text-${Math.round(z * 100)}`); }
await js(`store.set('zoom', 1); applyZoom()`);
await js(`uiPrefs.dense = false; setUiPref('dense')`); await sleep(400); await shot('rows-compact'); await js(`setUiPref('dense')`);
// high-DPI rendering: the page rasterised at a 150 % and 200 % display scale (the page's own layout is unchanged;
// this exercises hairlines, badges and the flow line at the scales most Windows laptops use)
for (const dpr of [1.5, 2]) {
  await send('Emulation.setDeviceMetricsOverride', {width: 1280, height: 720, deviceScaleFactor: dpr, mobile: false});
  await sleep(400); await shot(`scale-${Math.round(dpr * 100)}`);
}
await size(1280, 720);
await js(`store.set('theme', 'auto')`);
fs.writeFileSync(`${out}/shots.json`, JSON.stringify({shots, errors, fonts}, null, 2));
console.log(`${shots.length} screenshots; script errors: ${errors.length ? errors.join(' | ') : 'none'}`);
console.log('fonts: ' + JSON.stringify(fonts.available));
ws.close();
