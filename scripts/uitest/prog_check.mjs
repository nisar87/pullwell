// Drives the menu bar in headless Chrome: mouse, hover, Alt, Alt+letter, arrows, Escape, and what each menu does.
// Usage: node menu_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (name, ok, extra) => { results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra !== undefined ? '  (' + extra + ')' : ''}`); };

await call({action: 'add', url: `${fx}/slow/6MB?bps=60000`, force: true});
await sleep(1500);

const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.onopen = r);
let id = 0; const wait = new Map(); const errors = [];
ws.onmessage = ev => {
  const m = JSON.parse(ev.data);
  if (m.id && wait.has(m.id)) { wait.get(m.id)(m); wait.delete(m.id); return; }
  if (m.method === 'Runtime.exceptionThrown') errors.push('exception: ' + (m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text));
  if (m.method === 'Runtime.consoleAPICalled' && ['error', 'warning'].includes(m.params.type)) errors.push('console.' + m.params.type + ': ' + m.params.args.map(a => a.value ?? a.description).join(' '));
};
const send = (method, params = {}) => new Promise(r => { const i = ++id; wait.set(i, r); ws.send(JSON.stringify({id: i, method, params})); });
const js = async expr => { const r = await send('Runtime.evaluate', {expression: expr, awaitPromise: true, returnByValue: true}); if (r.result?.exceptionDetails) errors.push('eval: ' + expr.slice(0, 60) + ' -> ' + r.result.exceptionDetails.exception?.description); return r.result?.result?.value; };
const shot = async name => { const r = await send('Page.captureScreenshot', {format: 'png'}); fs.writeFileSync(`${out}/${name}.png`, Buffer.from(r.result.data, 'base64')); };
const mouse = async (type, x, y, extra = {}) => send('Input.dispatchMouseEvent', {type, x, y, button: type === 'mouseMoved' ? 'none' : 'left', clickCount: 1, ...extra});
const click = async (x, y, extra = {}) => { await mouse('mouseMoved', x, y); await mouse('mousePressed', x, y, extra); await mouse('mouseReleased', x, y, extra); };
const VK = {Alt: 18, Escape: 27, Enter: 13, ArrowDown: 40, ArrowUp: 38, ArrowLeft: 37, ArrowRight: 39, F10: 121};
const key = async (k, mods = 0) => {
  const code = k.length === 1 ? 'Key' + k.toUpperCase() : k === 'Alt' ? 'AltLeft' : k;
  const vk = VK[k] || k.toUpperCase().charCodeAt(0);
  await send('Input.dispatchKeyEvent', {type: 'rawKeyDown', key: k, code, modifiers: mods, windowsVirtualKeyCode: vk});
  if (k.length === 1 && !mods) await send('Input.dispatchKeyEvent', {type: 'char', text: k, key: k, code});
  await send('Input.dispatchKeyEvent', {type: 'keyUp', key: k, code, modifiers: k === 'Alt' ? 0 : mods, windowsVirtualKeyCode: vk});
};
const center = async sel => js(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e) return null; const r = e.getBoundingClientRect(); return [r.left + r.width/2, r.top + r.height/2]; })()`);
const menuText = () => js(`(() => { const m = document.querySelector('.ctx'); return m ? [...m.querySelectorAll(':scope > button')].map(b => b.textContent.replace(/\\s+/g, ' ').trim()) : null; })()`);
const openName = () => js(`(() => { const b = document.querySelector('#menubar button.open'); return b ? b.dataset.mb : null; })()`);
const focused = () => js(`(() => { const a = document.activeElement; return a ? (a.dataset.mb || a.textContent.trim()) : null; })()`);
const clickEntry = async text => { const p = await js(`(() => { const b = [...document.querySelectorAll('.ctx button')].find(b => b.textContent.trim().startsWith(${JSON.stringify(text)})); if (!b) return null; const r = b.getBoundingClientRect(); return [r.left + r.width/2, r.top + r.height/2]; })()`); if (p) await click(p[0], p[1]); return !!p; };


await send('Runtime.enable'); await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', {width: 1180, height: 760, deviceScaleFactor: 1, mobile: false});
await send('Page.navigate', {url}); await sleep(2500);

// progress dialog: double-click on the unfinished download
let p = await js(`(() => { const r = [...document.querySelectorAll('#tb tr')].find(r => /6MB/.test(r.textContent)); const b = r.children[0].getBoundingClientRect(); return [b.left + 40, b.top + b.height/2]; })()`);
await mouse('mouseMoved', p[0], p[1]); await mouse('mousePressed', p[0], p[1]); await mouse('mouseReleased', p[0], p[1]);
await mouse('mousePressed', p[0], p[1], {clickCount: 2}); await mouse('mouseReleased', p[0], p[1], {clickCount: 2}); await sleep(400);
check('double-click on an unfinished download opens its progress', await js('!!document.querySelector("#pgGrid")'));
const t1 = await js('document.querySelector(".prog .hero").textContent'), d1 = await js('document.querySelector("#pgGrid").textContent');
check('it shows speed, time left, connections', /Speed.*\/s.*Time left.*Connections/.test(d1), d1);
await sleep(2500);
const t2 = await js('document.querySelector(".prog .hero").textContent');
check('it refreshes while open', t1 !== t2, `${t1} → ${t2}`);
await shot('p01-progress');
await js('document.querySelector("#pgMain").click()'); await sleep(1200);
check('Pause from the dialog pauses it', /Paused/.test(await js('document.querySelector("#pgGrid").textContent')) && (await js('document.querySelector("#pgMain").textContent')) === 'Resume');
await js('document.querySelector("#pgMain").click()'); await sleep(1500);
check('Resume continues it', /Downloading|Waiting/.test(await js('document.querySelector("#pgGrid").textContent')));
await call({action: 'limit', id: (await (await fetch(url + 'api/downloads')).json()).items[0].id, limitBytes: 0});
for (let i = 0; i < 120 && !/Complete/.test(await js('document.querySelector("#pgState").textContent')); i++) await sleep(500);
check('when it finishes the dialog says Complete and offers Open', /Complete/.test(await js('document.querySelector("#pgState").textContent')) && (await js('document.querySelector("#pgMain").textContent')) === 'Open');
await shot('p02-complete');
await key('Escape'); await sleep(150);
check('Escape closes it', !(await js('!!document.querySelector("#dlDlg")')));

// copied links option
await js(`showOptions('general')`); await sleep(500);
check('Options shows "Offer links I copy", off', await js('!!document.querySelector("#clipTog") && document.querySelector("#clipTog").getAttribute("aria-checked") !== "true" && !document.querySelector("#clipTog").checked'));
await js('document.querySelector("#clipTog").click()'); await sleep(700);
let st = await (await fetch(url + 'api/app')).json();
check('switching it on is saved', st.clipboard === true);
await js('document.querySelector("#clipTog").click()'); await sleep(700);
st = await (await fetch(url + 'api/app')).json();
check('and off', st.clipboard === false);
await key('Escape'); await sleep(200);
const r = await fetch(url + 'api/app', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{"take":"add"}'});
check('handing over a link needs the program header', r.status === 403 || r.status === 400, r.status);

check('no script errors', errors.length === 0, errors.join(' ; '));
console.log(results.join('\n'));
console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
ws.close();
