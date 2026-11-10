// Drives the menu bar in headless Chrome: mouse, hover, Alt, Alt+letter, arrows, Escape, and what each menu does.
// Usage: node menu_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (name, ok, extra) => { results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra !== undefined ? '  (' + extra + ')' : ''}`); };

for (const n of ['2MB', '1MB']) await call({action: 'add', url: `${fx}/file/${n}`, force: true});
await call({action: 'add', url: `${fx}/slow/30MB?bps=300000`, force: true});
await sleep(2500);

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

check('menu bar has five menus', (await js('[...document.querySelectorAll("#menubar button")].map(b => b.textContent).join(",")')) === 'Tasks,Downloads,View,Tools,Help');
check('no page scroll with the menu bar', await js('document.documentElement.scrollHeight <= innerHeight'));

// mouse
let p = await center('#menubar [data-mb="tasks"]'); await click(p[0], p[1]); await sleep(150);
let t = await menuText();
check('click opens Tasks', t && t[0].startsWith('New download'), t && t.join(' | '));
check('menu sits under its title', await js('(() => { const m = document.querySelector(".ctx").getBoundingClientRect(), b = document.querySelector("#menubar [data-mb=tasks]").getBoundingClientRect(); return Math.abs(m.top - b.bottom) < 2 && Math.abs(m.left - b.left) < 2; })()'));
await shot('m01-tasks');
p = await center('#menubar [data-mb="downloads"]'); await mouse('mouseMoved', p[0], p[1]); await sleep(150);
check('hover moves to Downloads while a menu is open', (await openName()) === 'downloads');
await click(p[0], p[1]); await sleep(150);
check('clicking the open title closes it', !(await js('!!document.querySelector(".ctx")')));
await mouse('mouseMoved', p[0], p[1] + 200); await sleep(100);
check('hover alone does not open a menu', !(await js('!!document.querySelector(".ctx")')));

// keyboard: Alt+letter, arrows, Escape
await key('d', 1); await sleep(150);
check('Alt+D opens Downloads', (await openName()) === 'downloads');
check('first enabled entry has the focus', await js('document.activeElement && document.activeElement.closest(".ctx") !== null'));
await key('ArrowRight'); await sleep(120);
check('Right arrow goes to View', (await openName()) === 'view');
await key('ArrowLeft'); await key('ArrowLeft'); await sleep(120);
check('Left arrow twice goes back to Tasks', (await openName()) === 'tasks');
await key('ArrowLeft'); await sleep(120);
check('Left from Tasks wraps to Help', (await openName()) === 'help');
await key('Escape'); await sleep(120);
check('Escape closes and leaves the focus on the title', !(await js('!!document.querySelector(".ctx")')) && (await focused()) === 'help');
await key('Escape'); await sleep(80);
check('second Escape leaves the menu bar', !(await js('!!(document.activeElement && document.activeElement.closest("#menubar"))')));
await key('Alt'); await sleep(120);
check('Alt alone goes to the menu bar', (await focused()) === 'tasks', await focused());
await key('ArrowRight'); await key('Enter'); await sleep(150);
check('arrow and Enter open Downloads', (await openName()) === 'downloads');
await key('Escape'); await key('Escape'); await sleep(80);
await key('F10'); await sleep(80);
check('F10 goes to the menu bar', (await focused()) === 'tasks');
await key('Escape');

// submenus with the keyboard
await key('v', 1); await sleep(120);
for (let i = 0; i < 6 && !(await js('document.activeElement && document.activeElement.textContent.startsWith("Columns")')); i++) await key('ArrowDown');
await key('ArrowRight'); await sleep(150);
check('Right arrow opens a submenu', await js('!!(document.activeElement && document.activeElement.closest(".ctx.subm"))'));
await key('ArrowLeft'); await sleep(100);
check('Left arrow closes it again', await js('!document.querySelector(".ctx.subm") && document.activeElement.textContent.startsWith("Columns")'));
await key('Escape'); await key('Escape');

// View
const sideW = () => js('document.querySelector("#side").getBoundingClientRect().width');
p = await center('#menubar [data-mb="view"]'); await click(p[0], p[1]); await sleep(120);
t = await menuText(); check('View lists its entries', t && t.includes('Categories panel') && t.includes('Theme'), t && t.join(' | '));
await clickEntry('Categories panel'); await sleep(150);
check('View → Categories panel hides the panel', (await sideW()) === 0);
await click(p[0], p[1]); await sleep(120); await clickEntry('Categories panel'); await sleep(150);
check('and shows it again', (await sideW()) > 100);
const cols = () => js('document.querySelectorAll("#th th").length');
const before = await cols();
await click(p[0], p[1]); await sleep(120);
const cp = await js(`(() => { const b = [...document.querySelectorAll('.ctx button')].find(b => b.textContent.startsWith('Columns')); const r = b.getBoundingClientRect(); return [r.left + 20, r.top + r.height/2]; })()`);
await mouse('mouseMoved', cp[0], cp[1]); await sleep(150);
await shot('m02-view-columns');
await clickEntry('Speed'); await sleep(200);
check('View → Columns → Speed hides a column', (await cols()) === before - 1, `${before} → ${await cols()}`);
check('rows have as many cells as the header', await js('[...document.querySelectorAll("#tb tr")].every(r => r.children.length === document.querySelectorAll("#th th").length)'));
await click(p[0], p[1]); await sleep(120); await mouse('mouseMoved', cp[0], cp[1]); await sleep(150); await clickEntry('Speed'); await sleep(200);
check('and back', (await cols()) === before);
await click(p[0], p[1]); await sleep(120); await clickEntry('Toolbar labels'); await sleep(150);
check('View → Toolbar labels hides the words', await js('getComputedStyle(document.querySelector(".tbtn:not(.pri) span")).display === "none"'));
await click(p[0], p[1]); await sleep(120); await clickEntry('Toolbar labels'); await sleep(150);

// Downloads, with a finished item selected
const doneRow = await js(`(() => { const r = [...document.querySelectorAll('#tb tr')].find(r => r.querySelector('.pt.done')); if (!r) return null; const b = r.children[0].getBoundingClientRect(); return [b.left + 30, b.top + b.height/2]; })()`);
await click(doneRow[0], doneRow[1]); await sleep(200);
p = await center('#menubar [data-mb="downloads"]'); await click(p[0], p[1]); await sleep(150);
const st = await js(`Object.fromEntries([...document.querySelectorAll('.ctx > button')].map(b => [b.textContent.replace(/(Enter|Space|Alt\\+Enter|Ctrl\\+A|Shift\\+Del|Del)$/, '').trim(), !b.disabled]))`);
check('Downloads: Open and Download again on for a finished file, Resume and Stop off', st.Open && st['Download again'] && !st.Resume && !st.Stop, JSON.stringify(st));
await shot('m03-downloads');
const n0 = await js('document.querySelectorAll("#tb tr").length');
await clickEntry('Download again'); await sleep(200);
check('Download again asks first', /Download .* again\?/.test(await js('document.querySelector("#dlDlg") && document.querySelector("#dlDlg").textContent')));
await js('document.querySelector("#askYes").click()'); await sleep(1800);
check('Download again adds a new download', (await js('document.querySelectorAll("#tb tr").length')) === n0 + 1);

// Tools and Help
p = await center('#menubar [data-mb="tools"]'); await click(p[0], p[1]); await sleep(120); await clickEntry('Options'); await sleep(300);
check('Tools → Options opens Options', /Start with Windows|Save downloads in/.test(await js('document.querySelector("#dlDlg") ? document.querySelector("#dlDlg").textContent : ""')));
await key('Escape'); await sleep(100);
p = await center('#menubar [data-mb="tools"]'); await click(p[0], p[1]); await sleep(120); await clickEntry('Open the download folder'); await sleep(300);
check('Tools → Open the download folder does not fail', !(await js('document.querySelector("#toast").classList.contains("bad")')), await js('document.querySelector("#toast").textContent'));
p = await center('#menubar [data-mb="help"]'); await click(p[0], p[1]); await sleep(120); await clickEntry('Keyboard shortcuts'); await sleep(200);
check('Help → Keyboard shortcuts', /Alt or F10/.test(await js('document.querySelector("#dlDlg") ? document.querySelector("#dlDlg").textContent : ""')));
await shot('m04-shortcuts');
await key('Escape'); await sleep(100);
p = await center('#menubar [data-mb="help"]'); await click(p[0], p[1]); await sleep(120); await clickEntry('About'); await sleep(200);
check('Help → About shows the version and the project page', /Version/.test(await js('document.querySelector("#dlDlg").textContent')) && await js('!!document.querySelector("#dlDlg a[href*=\'github.com/\']")'));
await key('Escape'); await sleep(100);
check('menus closed while a dialog is open', !(await js('!!document.querySelector(".ctx")')));

await send('Emulation.setEmulatedMedia', {features: [{name: 'prefers-color-scheme', value: 'dark'}]}); await sleep(300);
p = await center('#menubar [data-mb="downloads"]'); await click(p[0], p[1]); await sleep(150); await shot('m05-dark'); await key('Escape');
await send('Emulation.setDeviceMetricsOverride', {width: 820, height: 560, deviceScaleFactor: 1, mobile: false}); await sleep(300);
check('no page scroll at 820x560', await js('document.documentElement.scrollHeight <= innerHeight && document.documentElement.scrollWidth <= innerWidth'));

check('no script errors', errors.length === 0, errors.join(' ; '));
console.log(results.join('\n'));
console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
ws.close();
