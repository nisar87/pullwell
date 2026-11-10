// Drives the menu bar in headless Chrome: mouse, hover, Alt, Alt+letter, arrows, Escape, and what each menu does.
// Usage: node menu_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (name, ok, extra) => { results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra !== undefined ? '  (' + extra + ')' : ''}`); };


await call({action: 'add', url: `${fx}/file/1MB`, force: true});
await call({action: 'add', url: `${fx}/file/2MB`, force: true, start: false});
await sleep(2500);
const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.onopen = r);
let id = 0; const wait = new Map(); const errors = [];
ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && wait.has(m.id)) { wait.get(m.id)(m); wait.delete(m.id); return; }
  if (m.method === 'Runtime.exceptionThrown') errors.push('exception: ' + (m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text)); };
const send = (method, params = {}) => new Promise(r => { const i = ++id; wait.set(i, r); ws.send(JSON.stringify({id: i, method, params})); });
const js = async expr => (await send('Runtime.evaluate', {expression: expr, awaitPromise: true, returnByValue: true})).result?.result?.value;
const key = async (k, code, vk) => { await send('Input.dispatchKeyEvent', {type: 'rawKeyDown', key: k, code, windowsVirtualKeyCode: vk}); await send('Input.dispatchKeyEvent', {type: 'keyUp', key: k, code, windowsVirtualKeyCode: vk}); };
await send('Runtime.enable');
await send('Emulation.setDeviceMetricsOverride', {width: 1180, height: 760, deviceScaleFactor: 1, mobile: false});
await send('Page.navigate', {url}); await sleep(2500);
const items = async () => (await (await fetch(url + 'api/downloads')).json()).items;

// rename a finished file with F2
const done = (await items()).find(v => v.state === 'done');
await js(`(() => { sel.clear(); sel.add(${JSON.stringify(done.id)}); cur = ${JSON.stringify(done.id)}; render(); document.activeElement.blur(); })()`);
await key('F2', 'F2', 113); await sleep(300);
check('F2 opens Rename with the name selected (not the type)', await js('!!document.querySelector("#rnName") && document.querySelector("#rnName").selectionEnd === document.querySelector("#rnName").value.lastIndexOf(".")'));
await js(`document.querySelector('#rnName').value = 'renamed-file.bin'; document.querySelector('#rnYes').click()`); await sleep(800);
let v = (await items()).find(x => x.id === done.id);
check('the finished file is renamed on disk', v.name === 'renamed-file.bin' && v.path.endsWith('renamed-file.bin'), v.path);
// rename one that has not started
const later = (await items()).find(x => x.state === 'paused');
await js(`renameBox(DL.items.find(x => x.id === ${JSON.stringify(later.id)}))`); await sleep(200);
await js(`document.querySelector('#rnName').value = 'my-chosen.bin'; document.querySelector('#rnYes').click()`); await sleep(500);
await call({action: 'resume', id: later.id}); await sleep(2500);
v = (await items()).find(x => x.id === later.id);
check('a download renamed before it starts is saved under that name', v.state === 'done' && v.path.endsWith('my-chosen.bin'), v.path);

// quick speed limit from the status bar
await js(`document.querySelector('#stLimit').click()`); await sleep(200);
const lm = await js(`[...document.querySelectorAll('.ctx > button')].map(b => b.textContent).join('|')`);
check('clicking the speed in the status bar offers limits', /No limit.*1 MB\/s/.test(lm), lm);
await js(`[...document.querySelectorAll('.ctx > button')].find(b => b.textContent.startsWith('2 MB/s')).click()`); await sleep(600);
check('choosing one limits all downloads', Math.abs((await (await fetch(url + 'api/downloads')).json()).config.limitAll - 2 * 1048576) < 1 && /limit 2/.test(await js('document.querySelector("#status").textContent')));
await js(`document.querySelector('#stLimit').click()`); await sleep(200);
await js(`[...document.querySelectorAll('.ctx > button')].find(b => b.textContent.startsWith('No limit')).click()`); await sleep(600);
check('and No limit removes it', (await (await fetch(url + 'api/downloads')).json()).config.limitAll === 0);

// proxy in Options → Connection
await js(`showOptions('connection')`); await sleep(700);
check('Options → Connection has the proxy choice', await js('!!document.querySelector("#pxMode") && document.querySelector("#pxMode").value === "system"'));
await js(`{ const s = document.querySelector('#pxMode'); s.value = 'manual'; s.dispatchEvent(new Event('change', {bubbles: true})) }`); await sleep(300);
check('"This proxy" shows the address field', await js('getComputedStyle(document.querySelector("#pxRow")).display !== "none"'));
await js(`document.querySelector('#pxAddr').value = 'nonsense'; document.querySelector('#pxSave').click()`); await sleep(500);
check('a bad address is refused with a reason', /port|host/.test(await js('document.querySelector("#toast").textContent')), await js('document.querySelector("#toast").textContent'));
await js(`document.querySelector('#pxAddr').value = '127.0.0.1:3128'; document.querySelector('#pxSave').click()`); await sleep(500);
let app = await (await fetch(url + 'api/app')).json();
check('a good one is saved', app.proxy.mode === 'manual' && app.proxy.addr === '127.0.0.1:3128');
await js(`showOptions('connection')`); await sleep(400);
await js(`{ const s = document.querySelector('#pxMode'); s.value = 'none'; s.dispatchEvent(new Event('change', {bubbles: true})) }`); await sleep(500);
app = await (await fetch(url + 'api/app')).json();
check('"No proxy" is saved', app.proxy.mode === 'none');
await js('dlClose()');
// queue delete asks in the window (no browser box)
await js(`dlQueues()`); await sleep(300); await js(`document.querySelector('[data-q="add"]').click()`); await sleep(200);
await js(`document.querySelector('[data-q="del"]').click()`); await sleep(300);
check('deleting a queue asks in a Pullwell dialog', /Delete the queue/.test(await js('document.querySelector("#dlDlg").textContent')));
await js(`document.querySelector('#askYes').click()`); await sleep(300);
check('and then shows the queues again without it', /No queues yet/.test(await js('document.querySelector("#dlDlg").textContent')));
await js('dlClose()');
await js(`showOptions('schedule')`); await sleep(500);
check('Options → Schedule has "Keep the PC awake while downloading", on', await js('!!document.querySelector("#awakeTog")') && (await (await fetch(url + 'api/app')).json()).keepAwake === true);
await js(`document.querySelector('#awakeTog').click()`); await sleep(700);
check('switching it off is saved', (await (await fetch(url + 'api/app')).json()).keepAwake === false);
await js(`document.querySelector('#awakeTog').click()`); await sleep(700);
await js('dlClose()');
await js(`showOptions('general')`); await sleep(500);
await js(`document.querySelector('#doneTog').click()`); await sleep(700);
check('"Show a finished download" can be switched on', (await (await fetch(url + 'api/app')).json()).showDone === true);
await js('dlClose()');
await call({action: 'add', url: `${fx}/file/300KB`, force: true});
for (let i = 0; i < 20 && !(await js('!!document.querySelector("#pgTitle")')); i++) await sleep(500);
check('when a download finishes its window comes up, Complete, with Open', /Complete · 300KB/.test(await js('document.querySelector("#pgTitle") ? document.querySelector("#pgTitle").textContent : ""')) && (await js('document.querySelector("#pgMain").textContent')) === 'Open');
await js('dlClose()');
await fetch(url + 'api/app', {method: 'POST', headers: H, body: '{"showDone":false}'});
// text size: menus stay under their titles and the window still fits
await js(`store.set('zoom', 1.3); applyZoom()`); await sleep(200);
await js(`document.querySelector('#menubar [data-mb="view"]').click()`); await sleep(250);
check('with larger text the menu sits under its title', await js(`(() => { const m = document.querySelector('.ctx').getBoundingClientRect(), b = document.querySelector('#menubar [data-mb=view]').getBoundingClientRect(); return Math.abs(m.top - b.bottom) < 2 && Math.abs(m.left - b.left) < 2; })()`));
await js('closeMenu()');
check('and the window still fits (status bar visible)', await js(`document.querySelector('#status').getBoundingClientRect().bottom <= innerHeight + 1`));
await js(`store.set('zoom', 1); applyZoom()`);
// Website column can be switched on
await js(`hideCols = []; store.set('hideCols', hideCols); render()`); await sleep(200);
check('the Website column shows the site', /127\.0\.0\.1/.test(await js(`document.querySelector('#tb').textContent`)));
await js(`hideCols = ['site']; store.set('hideCols', hideCols); render()`);
// a site that does not allow resuming: Stop asks first
await call({action: 'add', url: `${fx}/norange/8MB?bps=200000`, force: true}); await sleep(2500);
const nr = (await items()).find(v => v.url.includes('/norange/8MB'));
check('the engine says it cannot resume', nr && nr.noResume === true, nr && JSON.stringify({s: nr.state, n: nr.noResume}));
await js(`(() => { sel.clear(); sel.add(${JSON.stringify(nr && nr.id)}); cur = ${JSON.stringify(nr && nr.id)}; render(); })()`);
await js(`void doStop()`); await sleep(300);
check('Stop asks before throwing away what was downloaded', /does not allow resuming/.test(await js('document.querySelector("#dlDlg") ? document.querySelector("#dlDlg").textContent : ""')));
await js(`document.querySelector('#askNo').click()`); await sleep(500);
check('Cancel keeps it running', (await items()).find(v => v.id === nr.id).state === 'downloading');
await js(`progressBox(${JSON.stringify(nr && nr.id)})`); await sleep(400);
check('the progress window says it cannot resume', /Can resume\s*No/.test(await js('document.querySelector("#pgGrid").textContent')));
await js('dlClose()');
await call({action: 'remove', id: nr.id});
check('no script errors', errors.length === 0, errors.join(' ; '));
console.log(results.join('\n')); console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
ws.close();
