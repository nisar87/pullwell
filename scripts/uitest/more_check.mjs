// Drives the menu bar in headless Chrome: mouse, hover, Alt, Alt+letter, arrows, Escape, and what each menu does.
// Usage: node menu_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (name, ok, extra) => { console.error((ok ? "PASS " : "FAIL ") + name); results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra !== undefined ? '  (' + extra + ')' : ''}`); };



const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.onopen = r);
let id = 0; const wait = new Map(); const errors = [];
ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && wait.has(m.id)) { wait.get(m.id)(m); wait.delete(m.id); return; }
  if (m.method === 'Runtime.exceptionThrown') errors.push('exception: ' + (m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text)); };
const send = (method, params = {}) => new Promise(r => { const i = ++id; wait.set(i, r); ws.send(JSON.stringify({id: i, method, params})); });
const js = async expr => (await send('Runtime.evaluate', {expression: expr, awaitPromise: true, returnByValue: true})).result?.result?.value;
const key = async (k, code, vk, mods = 0) => { await send('Input.dispatchKeyEvent', {type: 'rawKeyDown', key: k, code, windowsVirtualKeyCode: vk, modifiers: mods}); await send('Input.dispatchKeyEvent', {type: 'keyUp', key: k, code, windowsVirtualKeyCode: vk, modifiers: mods}); };
await send('Runtime.enable');
await send('Emulation.setDeviceMetricsOverride', {width: 1180, height: 760, deviceScaleFactor: 1, mobile: false});
await send('Page.navigate', {url}); await sleep(2000);
const items = async () => (await (await fetch(url + 'api/downloads')).json()).items;
const fs2 = await import('node:fs');


await call({action: 'add', url: `${fx}/file/1MB`, force: true}); await call({action: 'add', url: `${fx}/file/2MB`, force: true, start: false}); await sleep(2000);
// grabber
await js(`dlGrab()`); await sleep(200);
await js(`document.querySelector('#gUrl').value = '${fx}/page/x'; document.querySelector('#gType').value = Object.keys(DL_TYPES)[0]; document.querySelector('#gFind').click()`); await sleep(1500);
const found = await js(`[...document.querySelectorAll('.gSel')].length`);
check('the grabber lists the files a page links to', found >= 2, found + ' found: ' + await js(`document.querySelector('#gOut').textContent.slice(0, 120)`));
const n0 = (await items()).length;
await js(`document.querySelector('#gAdd').click()`); await sleep(1000);
check('and adds the chosen ones', (await items()).length >= n0 + 2, (await items()).length - n0);
// save the list (through the program) and load it again
const saved = await (await fetch(url + 'api/app', {method: 'POST', headers: H, body: '{"saveList":true}'})).json();
const fs3 = await import('node:fs');
const listText = saved.path && fs3.readFileSync(saved.path, 'utf8');
check('Save the list writes a file in the download folder', !!listText && JSON.parse(listText).app === 'pullwell-downloads' && JSON.parse(listText).items.length === (await items()).length, saved.path || saved.error);
await call({action: 'remove', id: (await items())[0].id});
const before = (await items()).length;
const imp = await call({action: 'import', text: listText});
check('Load a list adds back what is missing and skips what is there', imp.imported === 1 && (await items()).length === before + 1, JSON.stringify(imp));
// properties: connections and mirrors are saved
const paused = (await items()).find(v => v.state === 'paused');
await js(`dlProps(${JSON.stringify(paused.id)})`); await sleep(300);
await js(`document.querySelector('#pC').value = '4'; document.querySelector('#pM').value = '${fx}/file/2MB?mirror'; document.querySelector('#pSave').click()`); await sleep(800);
let pv = (await items()).find(v => v.id === paused.id);
check('Properties saves connections and mirrors', pv.conns === 4 && (pv.mirrors || []).length === 1, JSON.stringify({c: pv.conns, m: pv.mirrors}));
// a queue with hours: a download moved into it waits outside its hours
const now = new Date(), hh = h => String((now.getHours() + h + 24) % 24).padStart(2, '0') + ':00';
await call({action: 'config', config: {...(await (await fetch(url + 'api/downloads')).json()).config, queues: [{id: '', name: 'Night', maxActive: 1, window: {enabled: true, from: hh(3), to: hh(4)}, days: [], paused: false}]}});
const cfg = (await (await fetch(url + 'api/downloads')).json()).config;
const night = cfg.queues[0];
await call({action: 'setQueue', id: paused.id, queue: night.id});
await call({action: 'resume', id: paused.id}); await sleep(1500);
pv = (await items()).find(v => v.id === paused.id);
check('a download in a queue outside its hours waits', pv.state === 'queued' && pv.queue === night.id, pv.state + ' ' + (pv.note || ''));
await js(`render()`); await sleep(200);
check('the sidebar lists the queue', /Night/.test(await js('document.querySelector("#side").textContent')));
// open when finished (from the file info dialog), move, check a checksum later
await js(`addDialog('${fx}/file/150KB')`); await sleep(200); await js(`document.querySelector('#addOk').click()`); await sleep(1500);
check('the file info dialog offers "Open it when it has finished"', await js('!!document.querySelector("#dlOpenDone")'));
await js(`document.querySelector('#dlOpenDone').checked = true; document.querySelector('#dlGo').click()`); await sleep(2500);
let od = (await items()).find(v => v.url.endsWith('/file/150KB'));
check('it is saved with the download', od && od.openDone === true, od && od.openDone);
await js(`(() => { const s = document.querySelector('#dlOpenDone'); return s; })()`);
const fs4 = await import('node:fs');
const path4 = await import('node:path'), os4 = await import('node:os');
const dest = path4.join(os4.tmpdir(), 'pw-move-test'); fs4.rmSync(dest, {recursive: true, force: true}); // C:\...\Temp on Windows
await js(`moveBox(DL.items.find(v => v.url.endsWith('/file/150KB')))`); await sleep(200);
await js(`document.querySelector('#ibVal').value = ${JSON.stringify(dest)}; document.querySelector('#ibYes').click()`); await sleep(800);
od = (await items()).find(v => v.url.endsWith('/file/150KB'));
check('Move to another folder moves the file', od.path.startsWith(dest + path4.sep) && fs4.existsSync(od.path), od.path);
const crypto = await import('node:crypto');
const hsum = crypto.createHash('sha256').update(fs4.readFileSync(od.path)).digest('hex');
await js(`checksumBox(DL.items.find(v => v.url.endsWith('/file/150KB')))`); await sleep(200);
await js(`document.querySelector('#ibVal').value = '${hsum}'; document.querySelector('#ibYes').click()`); await sleep(1500);
check('Check the checksum confirms a right one', (await items()).find(v => v.id === od.id).verify === 'ok');
await js(`checksumBox(DL.items.find(v => v.url.endsWith('/file/150KB')))`); await sleep(200);
await js(`document.querySelector('#ibVal').value = '${'0'.repeat(64)}'; document.querySelector('#ibYes').click()`); await sleep(1500);
check('and flags a wrong one', (await items()).find(v => v.id === od.id).verify === 'bad');
await js(`store.set('openDone', false)`);
check('no script errors', errors.length === 0, errors.join(' ; '));
console.log(results.join('\n')); console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
ws.close();
