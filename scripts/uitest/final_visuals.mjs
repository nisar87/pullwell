// Final visual pass: main window, separate progress window and Options in both themes, hover states and short frame
// sequences of a live download, its segment map and a completion. Works against headless Chrome and the real window
// (WebView2 with a remote debugging port) alike.
// Usage: node final_visuals.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
fs.mkdirSync(out + '/frames', {recursive: true});
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const list = async () => (await (await fetch(url + 'api/downloads', {headers: H})).json()).items || [];
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (n, ok, x) => results.push(`${ok ? 'PASS' : 'FAIL'}  ${n}${x !== undefined ? '  (' + x + ')' : ''}`);

async function connect(wsUrl) {
  const ws = new WebSocket(wsUrl); await new Promise(r => ws.onopen = r);
  let id = 0; const w = new Map(); const errors = [];
  ws.onmessage = e => { const m = JSON.parse(e.data);
    if (m.id && w.has(m.id)) { w.get(m.id)(m); w.delete(m.id); }
    if (m.method === 'Runtime.exceptionThrown') errors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text); };
  const send = (method, params = {}) => new Promise(r => { const i = ++id; w.set(i, r); ws.send(JSON.stringify({id: i, method, params})); });
  await send('Runtime.enable'); await send('Page.enable');
  const js = async e => { const r = await send('Runtime.evaluate', {expression: e, awaitPromise: true, returnByValue: true}); if (r.result?.exceptionDetails) errors.push('eval: ' + e.slice(0, 60)); return r.result?.result?.value; };
  const size = (width, height) => send('Emulation.setDeviceMetricsOverride', {width, height, deviceScaleFactor: 1, mobile: false});
  const shot = async (name, clip) => { const r = await send('Page.captureScreenshot', {format: 'png', ...(clip ? {clip: {...clip, scale: 1}} : {})}); fs.writeFileSync(`${out}/${name}.png`, Buffer.from(r.result.data, 'base64')); };
  const theme = t => js(`document.documentElement.dataset.theme='${t}'`);
  const mouse = (x, y) => send('Input.dispatchMouseEvent', {type: 'mouseMoved', x, y, button: 'none'});
  return {ws, send, js, size, shot, theme, mouse, errors};
}

for (const it of await list()) await call({action: 'remove', id: it.id, deleteFiles: true});
await call({action: 'add', url: `${fx}/file/3MB`, name: 'annual-report-2026.pdf', force: true});
await call({action: 'add', url: `${fx}/file/1MB`, name: 'holiday-photos.zip', force: true});
await call({action: 'add', url: `${fx}/slow/70MB?bps=200000`, name: 'big-buck-bunny-1080p.mp4', start: false, force: true});
await call({action: 'add', url: `${fx}/slow/400MB?bps=900000`, name: 'ubuntu-26.04-desktop-amd64.iso', force: true});
await sleep(5000);

const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const main = await connect((pages.find(t => t.type === 'page' && t.url.startsWith(url)) || pages.find(t => t.type === 'page')).webSocketDebuggerUrl);
await main.send('Page.navigate', {url}); await sleep(2000);
await main.size(1280, 720);
for (const t of ['light', 'dark']) { await main.theme(t); await sleep(400); await main.shot(`main-${t}`); }

// hover states (dark): a row, the primary button, a sidebar entry
await main.theme('dark');
const pt = async sel => JSON.parse(await main.js(`(()=>{const r=document.querySelector(${JSON.stringify(sel)}).getBoundingClientRect();return JSON.stringify([r.left+r.width/2,r.top+r.height/2])})()`));
for (const [name, sel] of [['hover-row', '#tb tr:nth-child(2)'], ['hover-button', '.tbtn.pri'], ['hover-sidebar', '#side [data-view]:nth-of-type(3)']]) {
  const [x, y] = await pt(sel); await main.mouse(x, y); await sleep(300); await main.shot(name);
}
await main.mouse(5, 5);

// frames of the live row: the bar, the segment map and the glow, one every 400 ms
const clip = {x: 233, y: 217, width: 1030, height: 330};
for (let i = 0; i < 12; i++) { await main.shot(`frames/live-${String(i).padStart(2, '0')}`, clip); await sleep(400); }
const grow = await main.js(`(()=>{const i=document.querySelector('tr.run .st .pb i');return i? i.style.transform : null})()`);
check('a live row shows a real, scaled progress bar', !!grow && /scaleX/.test(grow), grow);
const glow = await main.js(`getComputedStyle(document.querySelector('tr.run .st .pb i')).boxShadow`);
check('only the live row glows', glow !== 'none' && await main.js(`getComputedStyle(document.querySelector('tr:not(.run) .st .pb i')).boxShadow`) === 'none', glow);
check('the paused row is still', await main.js(`(()=>{const r=[...document.querySelectorAll('#tb tr')].find(r=>/Paused/.test(r.textContent));return !!r&&getComputedStyle(r.querySelector('.pb i')).animationName==='none'})()`));

// completion: a short download finishing while the list is watched
await call({action: 'add', url: `${fx}/slow/12MB?bps=2500000`, name: 'podcast-episode-112.mp3', force: true});
let finSeen = false;
for (let i = 0; i < 28; i++) {
  await main.shot(`frames/finish-${String(i).padStart(2, '0')}`, clip);
  if (await main.js(`!!document.querySelector('tr.fin')`)) finSeen = true;
  await sleep(300);
}
check('a finishing download gets the completion pulse', finSeen);

// the separate progress window of the live download
const live = (await list()).find(v => v.name.startsWith('ubuntu'));
const popUrl = `${url}?progress=${live.id}`;
let popTarget = null;
try { popTarget = await (await fetch(`http://127.0.0.1:${port}/json/new?${encodeURIComponent(popUrl)}`, {method: 'PUT'})).json(); } catch (e) {}
if (!popTarget?.webSocketDebuggerUrl) { await main.js(`ProgressWindow.open(${JSON.stringify(live.id)})`); await sleep(2500); popTarget = (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find(t => t.url.includes('progress=')); }
const pop = await connect(popTarget.webSocketDebuggerUrl);
await pop.size(660, 650); await sleep(2500);
for (const t of ['light', 'dark']) { await pop.theme(t); await sleep(400); await pop.shot(`popout-${t}`); }
for (let i = 0; i < 6; i++) { await pop.shot(`frames/popout-${i}`, {x: 0, y: 120, width: 660, height: 200}); await sleep(700); }
check('the progress window shows the download', /ubuntu/.test(await pop.js(`document.querySelector('#pgTitle').textContent`)));
// closing the window must not stop the download
await pop.ws.close(); await fetch(`http://127.0.0.1:${port}/json/close/${popTarget.id}`).catch(() => {});
await sleep(3000);
const after = (await list()).find(v => v.id === live.id);
check('closing the progress window leaves the download running', after.state === 'downloading', after.state);

// Options
for (const t of ['light', 'dark']) { await main.theme(t); await main.js(`showOptions('general')`); await sleep(500); await main.shot(`options-${t}`); await main.js(`document.querySelector('.dialog-close,#optClose')&&document.querySelector('.dialog-close,#optClose').click()`); await sleep(300); }
check('no script errors', main.errors.length === 0, main.errors.join(' | ').slice(0, 200));
fs.writeFileSync(out + '/final_visuals.txt', results.join('\n') + '\n'); console.log(results.join('\n'));
main.ws.close();
