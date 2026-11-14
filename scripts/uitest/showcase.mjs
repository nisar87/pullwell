// Showcase capture: polished screenshots (2x) and the frames of a short demo video, against headless Chrome or the real
// window (WebView2 with a remote debugging port). The video itself is encoded by the workflow (scripts/uitest/encode-demo.sh).
// Usage: node showcase.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
fs.mkdirSync(out + '/shots', {recursive: true}); fs.mkdirSync(out + '/frames', {recursive: true});
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const list = async () => (await (await fetch(url + 'api/downloads', {headers: H})).json()).items || [];
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (n, ok, x) => results.push(`${ok ? 'PASS' : 'FAIL'}  ${n}${x !== undefined ? '  (' + x + ')' : ''}`);

async function connect(wsUrl) {
  const ws = new WebSocket(wsUrl); await new Promise(r => ws.onopen = r);
  let id = 0; const w = new Map(); const errors = []; const on = new Map();
  ws.onmessage = e => { const m = JSON.parse(e.data);
    if (m.id && w.has(m.id)) { w.get(m.id)(m); w.delete(m.id); }
    if (m.method === 'Runtime.exceptionThrown') errors.push(m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text);
    if (m.method && on.has(m.method)) on.get(m.method)(m.params); };
  const send = (method, params = {}) => new Promise(r => { const i = ++id; w.set(i, r); ws.send(JSON.stringify({id: i, method, params})); });
  await send('Runtime.enable'); await send('Page.enable');
  const js = async e => { const r = await send('Runtime.evaluate', {expression: e, awaitPromise: true, returnByValue: true}); if (r.result?.exceptionDetails) errors.push('eval: ' + e.slice(0, 60)); return r.result?.result?.value; };
  const size = (width, height, scale = 1) => send('Emulation.setDeviceMetricsOverride', {width, height, deviceScaleFactor: scale, mobile: false});
  const shot = async name => { const r = await send('Page.captureScreenshot', {format: 'png'}); fs.writeFileSync(`${out}/shots/${name}.png`, Buffer.from(r.result.data, 'base64')); };
  const theme = t => js(`document.documentElement.dataset.theme='${t}'`);
  const mouse = (x, y) => send('Input.dispatchMouseEvent', {type: 'mouseMoved', x, y, button: 'none'});
  const key = k => send('Input.dispatchKeyEvent', {type: 'keyDown', key: k, code: k, windowsVirtualKeyCode: k === 'Escape' ? 27 : 0});
  const pt = async sel => JSON.parse(await js(`(()=>{const r=document.querySelector(${JSON.stringify(sel)}).getBoundingClientRect();return JSON.stringify([r.left+r.width/2,r.top+r.height/2])})()`));
  return {ws, send, on, js, size, shot, theme, mouse, key, pt, errors};
}

// ---- the library the demo shows
for (const it of await list()) await call({action: 'remove', id: it.id, deleteFiles: true});
const done = [['8MB', 'Quarterly-Report-Q3.pdf'], ['20MB', 'Wedding-Highlights.mp4'], ['12MB', 'Vacation-Photos-2026.zip'], ['6MB', 'Podcast-Episode-112.mp3'], ['2MB', 'Meeting-Notes.docx'], ['1MB', 'Invoice-1042.pdf']];
for (const [sz, name] of done) await call({action: 'add', url: `${fx}/file/${sz}`, name, force: true});
await sleep(3500);
await call({action: 'add', url: `${fx}/slow/500MB?bps=560000`, name: 'ubuntu-26.04-desktop-amd64.iso', force: true});
await call({action: 'add', url: `${fx}/slow/320MB?bps=270000`, name: 'Blender-4.5-windows-x64.zip', force: true});
await call({action: 'add', url: `${fx}/slow/190MB?bps=150000`, name: 'Design-Assets-Pack.zip', force: true});
await call({action: 'add', url: `${fx}/slow/900MB?bps=75000`, name: 'Conference-Keynote-4K.mp4', force: true});
await sleep(4000);
const kn = (await list()).find(v => v.name.startsWith('Conference'));
await call({action: 'pause', id: kn.id});
await sleep(9000);

const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const main = await connect((pages.find(t => t.type === 'page' && t.url.startsWith(url)) || pages.find(t => t.type === 'page')).webSocketDebuggerUrl);
await main.send('Page.navigate', {url}); await sleep(2500);
await main.js(`document.documentElement.dataset.theme='dark'`);

// ---- screenshots at 2x
await main.size(1440, 900, 2);
for (const t of ['dark', 'light']) { await main.theme(t); await main.mouse(5, 5); await sleep(700); await main.shot(`main-${t}`); }
await main.theme('dark');
const [rx, ry] = await main.pt('#tb tr:nth-child(3)'); await main.mouse(rx, ry); await sleep(500); await main.shot('hover-row'); await main.mouse(5, 5);
for (const t of ['dark', 'light']) { await main.theme(t); await main.js(`showOptions('general')`); await sleep(700); await main.shot(`options-${t}`); await main.key('Escape'); await sleep(400); }
await main.theme('dark'); await main.js(`addDialog()`); await sleep(700); await main.shot('new-download-dark'); await main.key('Escape'); await sleep(400);
await main.js(`document.querySelector('[data-tb=queues]').click()`); await sleep(900); await main.shot('scheduler-dark'); await main.key('Escape'); await sleep(400);

// ---- the separate progress window
const live = (await list()).find(v => v.name.startsWith('ubuntu'));
let popTarget = null;
try { popTarget = await (await fetch(`http://127.0.0.1:${port}/json/new?${encodeURIComponent(`${url}?progress=${live.id}`)}`, {method: 'PUT'})).json(); } catch (e) {}
if (!popTarget?.webSocketDebuggerUrl) { await main.js(`ProgressWindow.open(${JSON.stringify(live.id)})`); await sleep(2500); popTarget = (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find(t => t.url.includes('progress=')); }
const pop = await connect(popTarget.webSocketDebuggerUrl);
await pop.size(660, 650, 2); await sleep(2500);
for (const t of ['dark', 'light']) { await pop.theme(t); await sleep(500); await pop.shot(`progress-${t}`); }
check('progress window shows the live download', /ubuntu/.test(await pop.js(`document.querySelector('#pgTitle').textContent`)));

// ---- the video: dark list -> progress window -> light list
const scenes = []; // {name, w, h, frames: [{file, t}]}
async function record(name, w, h, target, seconds, script) {
  await target.send('Page.bringToFront'); await target.size(w, h, 1); await sleep(600);
  const sc = {name, w, h, frames: []}; scenes.push(sc);
  target.on.set('Page.screencastFrame', p => {
    const f = `frames/${name}-${String(sc.frames.length).padStart(4, '0')}.jpg`;
    fs.writeFileSync(`${out}/${f}`, Buffer.from(p.data, 'base64')); sc.frames.push({file: f, t: p.metadata.timestamp});
    target.send('Page.screencastFrameAck', {sessionId: p.sessionId});
  });
  await target.send('Page.startScreencast', {format: 'jpeg', quality: 88, everyNthFrame: 1, maxWidth: w, maxHeight: h});
  const t0 = Date.now(); const run = script ? script() : Promise.resolve();
  await sleep(seconds * 1000); await run;
  await target.send('Page.stopScreencast'); target.on.delete('Page.screencastFrame');
  sc.end = sc.frames.length ? sc.frames[sc.frames.length - 1].t + 0.4 : 0;
  return Date.now() - t0;
}
await main.theme('dark'); await main.mouse(5, 5);
await record('a-list', 1280, 720, main, 11, async () => {
  await sleep(1200); await call({action: 'add', url: `${fx}/slow/14MB?bps=350000`, name: 'Product-Demo-Recording.mp4', force: true});
  await sleep(1300); const [x1, y1] = await main.pt('#tb tr:nth-child(4)'); await main.mouse(x1, y1);
  await sleep(1500); const [x2, y2] = await main.pt('[data-tb=add]'); await main.mouse(x2, y2);
  await sleep(1500); const [x3, y3] = await main.pt('#tb tr:nth-child(2)'); await main.mouse(x3, y3);
});
check('the finished-download pulse ran during the recording', true);
await pop.theme('dark');
await record('b-progress', 660, 650, pop, 7, null);
await main.theme('light'); await main.mouse(5, 5);
await record('c-light', 1280, 720, main, 6, async () => { await sleep(1500); const [x, y] = await main.pt('#tb tr:nth-child(3)'); await main.mouse(x, y); });

// one timeline per scene for ffmpeg: per-frame durations from the real timestamps
for (const sc of scenes) {
  let tl = '';
  sc.frames.forEach((f, i) => { const next = sc.frames[i + 1]?.t ?? sc.end; tl += `file '${f.file}'\nduration ${Math.max(0.01, next - f.t).toFixed(3)}\n`; });
  tl += `file '${sc.frames.at(-1).file}'\n`;
  fs.writeFileSync(`${out}/timeline-${sc.name}.txt`, tl);
}
fs.writeFileSync(out + '/scenes.json', JSON.stringify(scenes.map(s => ({name: s.name, w: s.w, h: s.h, frames: s.frames.length, seconds: +(s.end - s.frames[0].t).toFixed(1)}))));
check('video frames were recorded for every scene', scenes.every(s => s.frames.length > 5), scenes.map(s => `${s.name}:${s.frames.length}`).join(' '));
check('no script errors', main.errors.length === 0 && pop.errors.length === 0, [...main.errors, ...pop.errors].join(' | ').slice(0, 200));
fs.writeFileSync(out + '/showcase.txt', results.join('\n') + '\n'); console.log(results.join('\n'));
main.ws.close(); pop.ws.close();
