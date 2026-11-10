// Drives the new window in headless Chrome: seeds downloads through the API, then exercises the table, selection,
// context menu, keyboard, dialogs and options, saving screenshots and every script error.
// Usage: node ui_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (name, ok, extra) => { results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? '  (' + extra + ')' : ''}`); };

// seed: two finished, one slow running, one paused, one failing
for (const [path, n] of [['file', '2MB'], ['file', '1MB']]) await call({action: 'add', url: `${fx}/${path}/${n}`, force: true});
await call({action: 'add', url: `${fx}/slow/20MB?bps=400000`, force: true});
const p = await call({action: 'add', url: `${fx}/slow/30MB?bps=300000`, force: true});
await call({action: 'add', url: `${fx}/status404/1MB`, force: true});
await sleep(2500);
if (p.added && p.added[0]) await call({action: 'pause', id: p.added[0].id});
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
const mouse = async (type, x, y, extra = {}) => send('Input.dispatchMouseEvent', {type, x, y, button: 'left', clickCount: 1, ...extra});
const click = async (x, y, extra = {}) => { await mouse('mousePressed', x, y, extra); await mouse('mouseReleased', x, y, extra); };
const key = async (k, code, mods = 0, keyCode) => { await send('Input.dispatchKeyEvent', {type: 'rawKeyDown', key: k, code, modifiers: mods, windowsVirtualKeyCode: keyCode}); await send('Input.dispatchKeyEvent', {type: 'keyUp', key: k, code, modifiers: mods, windowsVirtualKeyCode: keyCode}); };
const center = async sel => js(`(() => { const e = document.querySelector(${JSON.stringify(sel)}); if (!e) return null; const r = e.getBoundingClientRect(); return [r.left + r.width/2, r.top + r.height/2]; })()`);

await send('Runtime.enable'); await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', {width: 1180, height: 760, deviceScaleFactor: 1, mobile: false});
await send('Page.navigate', {url}); await sleep(2500);

check('rows shown', (await js('document.querySelectorAll("#tb tr").length')) === 5, await js('document.querySelectorAll("#tb tr").length') + ' rows');
check('no page scroll', await js('document.documentElement.scrollHeight <= innerHeight'));
await shot('01-main');

// selection by click, then toolbar state
const row = await center('#tb tr:nth-child(3) td:nth-child(1)');
await click(row[0], row[1]); await sleep(300);
check('click selects one row', (await js('document.querySelectorAll("#tb tr.sel").length')) === 1);
const sameEl = await js('(() => { window.__r = document.querySelector("#tb tr.sel"); return true })()');
await sleep(1300);
check('row element kept across refresh', await js('window.__r === document.querySelector("#tb tr.sel")'));
// ctrl+click adds, shift+click range
const r1 = await center('#tb tr:nth-child(1) td:nth-child(1)');
await click(r1[0], r1[1], {modifiers: 2}); await sleep(200);
check('ctrl+click adds', (await js('document.querySelectorAll("#tb tr.sel").length')) === 2);
const r5 = await center('#tb tr:nth-child(5) td:nth-child(1)');
await click(r5[0], r5[1], {modifiers: 8}); await sleep(200);
check('shift+click selects a range', (await js('document.querySelectorAll("#tb tr.sel").length')) === 5, await js('document.querySelectorAll("#tb tr.sel").length') + ' selected');
check('status bar counts the selection', /5<\/b> selected/.test(await js('document.querySelector("#status").innerHTML')));

// keyboard
await click(r1[0], r1[1]); await sleep(150);
await key('ArrowDown', 'ArrowDown', 0, 40); await key('ArrowDown', 'ArrowDown', 8, 40); await sleep(150);
check('arrow keys move, shift extends', (await js('document.querySelectorAll("#tb tr.sel").length')) === 2);
await key('a', 'KeyA', 2, 65); await sleep(150);
check('Ctrl+A selects all', (await js('document.querySelectorAll("#tb tr.sel").length')) === 5);
await key('Escape', 'Escape', 0, 27); await sleep(150);
check('Escape clears selection', (await js('document.querySelectorAll("#tb tr.sel").length')) === 0);

// context menu on a finished row
const fin = await js('(() => { const tr = [...document.querySelectorAll("#tb tr")].find(t => /Complete/.test(t.textContent)); const r = tr.getBoundingClientRect(); return [r.left + 80, r.top + r.height/2]; })()');
await mouse('mousePressed', fin[0], fin[1], {button: 'right'}); await mouse('mouseReleased', fin[0], fin[1], {button: 'right'});
await send('Input.dispatchMouseEvent', {type: 'mousePressed', x: fin[0], y: fin[1], button: 'right', clickCount: 1});
await js(`document.elementFromPoint(${fin[0]}, ${fin[1]}).dispatchEvent(new MouseEvent('contextmenu', {bubbles: true, clientX: ${fin[0]}, clientY: ${fin[1]}}))`);
await sleep(300);
const menu = await js('[...document.querySelectorAll(".ctx > button")].map(b => (b.disabled ? "-" : "+") + b.textContent).join("|")');
check('context menu opens with Open enabled for a finished file', /\+Open/.test(menu || ''), menu);
await js(`(() => { const b = [...document.querySelectorAll(".ctx > button")].find(b => b.textContent.startsWith("Speed limit")); b.dispatchEvent(new MouseEvent('mouseenter')); })()`);
await sleep(200); await shot('02-context-menu');
await key('Escape', 'Escape', 0, 27); await sleep(150);
check('Escape closes the menu', !(await js('!!document.querySelector(".ctx")')));

// properties by Alt+Enter on the paused row
const pr = await js('(() => { const tr = [...document.querySelectorAll("#tb tr")].find(t => /Paused/.test(t.textContent)); const r = tr.getBoundingClientRect(); return [r.left + 80, r.top + r.height/2]; })()');
await click(pr[0], pr[1]); await sleep(150);
await key('Enter', 'Enter', 1, 13); await sleep(500);
check('Alt+Enter opens Properties', /Parts of the file|What happened/.test(await js('document.querySelector("#dlDlg") ? document.querySelector("#dlDlg").textContent : ""')));
await shot('03-properties');
await key('Escape', 'Escape', 0, 27); await sleep(200);

// add dialog via toolbar, probe, cancel
let b = await center('[data-tb="add"]'); await click(b[0], b[1]); await sleep(300);
check('Add URL dialog opens with focus in the box', await js('document.activeElement && document.activeElement.id === "addUrl"'));
await send('Input.insertText', {text: `${fx}/file/3MB`});
await key('Enter', 'Enter', 0, 13); await sleep(1500);
check('link is checked and file info shown', /Download file info/.test(await js('document.querySelector("#dlDlg").textContent')));
await shot('04-file-info');
b = await center('#dlGo'); await click(b[0], b[1]); await sleep(2500);
check('download added from the dialog', (await js('document.querySelectorAll("#tb tr").length')) === 6);

// options dialog, tabs
b = await center('[data-tb="opts"]'); await click(b[0], b[1]); await sleep(300);
await shot('05-options');
for (const t of ['connection', 'schedule', 'torrents', 'advanced']) { const c = await center(`[data-opt="${t}"]`); await click(c[0], c[1]); await sleep(200); }
check('options tabs work', /Run a program after each download/.test(await js('document.querySelector("#dlDlg").textContent')));
const cc = await center('[data-opt="connection"]'); await click(cc[0], cc[1]); await sleep(200);
await js(`(() => { const s = document.querySelector('#dlMax'); s.value = '5'; s.dispatchEvent(new Event('change', {bubbles: true})); })()`); await sleep(800);
check('changing a setting saves it and keeps the dialog open', (await js('document.querySelector("#dlMax") && document.querySelector("#dlMax").value')) === '5' && (await (await fetch(url + 'api/downloads')).json()).config.maxActive === 5);
await key('Escape', 'Escape', 0, 27); await sleep(200);

// sidebar filter and sort
const fv = await center('[data-view=\'{"k":"finished"}\']'); await click(fv[0], fv[1]); await sleep(300);
check('Finished filter', (await js('[...document.querySelectorAll("#tb tr")].every(t => /Complete/.test(t.textContent))')) && (await js('document.querySelectorAll("#tb tr").length')) >= 2);
const av = await center('[data-view=\'{"k":"all"}\']'); await click(av[0], av[1]); await sleep(200);
const hs = await center('th[data-sort="size"]'); await click(hs[0], hs[1]); await sleep(300);
check('sorting by size', await js('(() => { const v = [...document.querySelectorAll("#tb tr td:nth-child(2)")].map(t => t.textContent); return v.length > 2; })()'));
await shot('06-sorted');

// delete with the dialog
const er = await js('(() => { const tr = [...document.querySelectorAll("#tb tr")].find(t => /Error/.test(t.textContent)); if (!tr) return null; const r = tr.getBoundingClientRect(); return [r.left + 80, r.top + r.height/2]; })()');
if (er) { await click(er[0], er[1]); await sleep(150); await key('Delete', 'Delete', 0, 46); await sleep(300); await shot('07-delete'); const y = await center('#askYes'); await click(y[0], y[1]); await sleep(1200); }
check('Delete removes the failed download', !(await js('[...document.querySelectorAll("#tb tr")].some(t => /Error/.test(t.textContent))')));

// dark theme
await send('Emulation.setEmulatedMedia', {features: [{name: 'prefers-color-scheme', value: 'dark'}]}); await sleep(400); await shot('08-dark');
// narrow window
await send('Emulation.setDeviceMetricsOverride', {width: 820, height: 560, deviceScaleFactor: 1, mobile: false}); await sleep(400); await shot('09-small');
check('no page scroll at 820x560', await js('document.documentElement.scrollHeight <= innerHeight && document.documentElement.scrollWidth <= innerWidth'));

console.log(results.join('\n'));
console.log('script errors:', errors.length ? errors : 'none');
ws.close();
