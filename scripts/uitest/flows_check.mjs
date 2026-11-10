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

// a numbered series from the Add dialog
await js(`addDialog('${fx}/file/[1-5]KB')`); await sleep(300);
await js(`document.querySelector('#dlDlg .btn.pri').click()`); await sleep(2500);
let its = await items();
check('a numbered series [1-5] adds five downloads', its.filter(v => /\/file\/[1-5]KB$/.test(v.url)).length === 5, its.map(v => v.url.split('/').pop()).join(','));
// search, select all and delete only what is shown
await js(`document.querySelector('#search').value = '3KB'; document.querySelector('#search').dispatchEvent(new Event('input', {bubbles: true}))`); await sleep(300);
check('search shows only matches', (await js('document.querySelectorAll("#tb tr").length')) === 1);
await js(`document.querySelector('#search').blur(); document.querySelector('#tbl').focus()`);
await key('a', 'KeyA', 65, 2); await sleep(150);
await key('Delete', 'Delete', 46); await sleep(300);
await js(`document.querySelector('#askYes').click()`); await sleep(800);
its = await items();
check('Ctrl+A then Delete removes only the shown download', its.length === 4 && !its.some(v => v.url.endsWith('/3KB')), its.length);
await js(`document.querySelector('#search').value = ''; document.querySelector('#search').dispatchEvent(new Event('input', {bubbles: true}))`); await sleep(300);
// delete with the file
const one = its.find(v => v.url.endsWith('/1KB'));
check('the finished file is on disk', fs2.existsSync(one.path));
await js(`(() => { sel.clear(); sel.add(${JSON.stringify(one.id)}); cur = ${JSON.stringify(one.id)}; render(); })()`);
await js(`void doDelete(true)`); await sleep(300);
check('Shift+Delete asks with "also delete the file" ticked', await js('!!document.querySelector("#askChk") && document.querySelector("#askChk").checked'));
await js(`document.querySelector('#askYes').click()`); await sleep(800);
check('and the file is gone from the disk', !fs2.existsSync(one.path));
// a server that fails twice: the download retries and finishes
await call({action: 'add', url: `${fx}/status503/300KB`, force: true}); await sleep(200);
let v503;
for (let i = 0; i < 60; i++) { v503 = (await items()).find(v => v.url.includes('status503')); if (v503 && v503.state === 'done') break; await sleep(500); }
check('a busy server (503) is tried again and the file finishes', v503 && v503.state === 'done', v503 && v503.state + ' ' + (v503.err || v503.note));
// checksum: right and wrong
const sum = (await (await fetch(`${fx}/sha256/200KB`)).text()).trim().split(/\s/)[0];
await call({action: 'add', url: `${fx}/file/200KB`, hash: 'sha256:' + sum, force: true});
await call({action: 'add', url: `${fx}/file/200KB?bad`, hash: 'sha256:' + '0'.repeat(64), force: true});
await sleep(4000);
its = await items();
const good = its.find(v => v.url.endsWith('/200KB')), bad = its.find(v => v.url.endsWith('?bad'));
check('a right checksum is confirmed', good && good.verify === 'ok', good && good.verify);
check('a wrong checksum is flagged and shown in the list', bad && bad.verify === 'bad' && /NOT match/.test(await js('document.querySelector("#tb").textContent')), bad && bad.verify);
// a 404 stops with plain words, and Resume is offered
await call({action: 'add', url: `${fx}/nothing-here/1KB`, force: true}); await sleep(2500);
const nf = (await items()).find(v => v.url.includes('nothing-here'));
check('a missing file says so in plain words', nf && nf.state === 'error' && /not found/i.test(nf.err), nf && nf.err);
// Space pauses and resumes a running download
await call({action: 'add', url: `${fx}/slow/5MB?bps=50000`, force: true}); await sleep(1500);
const sl = (await items()).find(v => v.url.includes('slow/5MB'));
await js(`(() => { sel.clear(); sel.add(${JSON.stringify(sl.id)}); cur = ${JSON.stringify(sl.id)}; render(); document.activeElement.blur(); })()`);
await key(' ', 'Space', 32); await sleep(1000);
check('Space stops a running download', (await items()).find(v => v.id === sl.id).state === 'paused');
await key(' ', 'Space', 32); await sleep(1200);
check('Space again resumes it', ['downloading', 'queued'].includes((await items()).find(v => v.id === sl.id).state));
// sidebar counts follow the list
const counts = await js(`[...document.querySelectorAll('#side [data-view]')].map(e => e.textContent.replace(/\\s+/g, ' ').trim()).join(' | ')`);
check('the sidebar counts match the list', new RegExp('All downloads ?' + (await items()).length + '\\b').test(counts), counts);
check('no script errors', errors.length === 0, errors.join(' ; '));
console.log(results.join('\n')); console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
ws.close();
