// The notice that a newer browser extension is available: a banner and a button in Options when a connected extension is older than the one that ships with the program (or says nothing, as before 0.15.0), the dialog with the steps, "Later", and Urdu.
// Usage: node extupdate_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const call = async b => (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (name, ok, extra) => { results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra !== undefined ? '  (' + extra + ')' : ''}`); };


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
await send('Runtime.enable'); await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', {width: 1180, height: 760, deviceScaleFactor: 1, mobile: false});
const ORIGIN = 'chrome-extension://abcdefghijklmnopabcdefghijklmnop';
const ext = (p, o = {}) => fetch(new URL(p, url), {...o, headers: {Origin: ORIGIN, 'Content-Type': 'application/json', ...(o.headers || {})}});
const reload = async lang => { await send('Page.navigate', {url}); await sleep(900); await js(`localStorage.setItem('pw.lang', JSON.stringify(${JSON.stringify(lang)})); localStorage.removeItem('pw.extBn')`); await send('Page.navigate', {url}); await sleep(2200); };
const bnText = () => js(`(document.getElementById('extBn') || {}).textContent || ''`);
const jsClick = sel => js(`document.querySelector(${JSON.stringify(sel)}).click()`);

// pair a made-up browser: it never says which extension version it runs, like the extensions before 0.15.0
const code = (await call({action: 'extCode'})).code;
const tok = (await (await ext('ext/pair', {method: 'POST', body: JSON.stringify({code, name: 'Test browser'})})).json()).token;
check('a browser is paired', !!tok);
const auth = {Authorization: 'Bearer ' + tok};

await reload('en');
check('an extension that reports no version gets the banner', (await bnText()).includes('A newer browser extension is available.'), await bnText());
check('the banner has a button to get it', await js(`!!document.querySelector('#extBn [data-act=extUpdate]')`));
await shot('banner-en');
await jsClick('#extBn [data-act=extUpdate]'); await sleep(500);
check('the button opens the steps', (await js(`document.querySelector('#dlDlg h3')?.textContent`)) === 'Update the browser extension');
check('they offer the zip for Chrome and for Firefox', await js(`document.querySelectorAll('#dlDlg [data-saveext]').length === 2`));
check('and name the version that is ready', (await js(`document.querySelector('#dlDlg .hint2').textContent`)).includes('0.15.0'));
await shot('dialog-en');
await jsClick('#extLater2'); await sleep(400);
check('"Later" closes the steps and hides the banner', !(await js(`!!document.getElementById('dlDlg')`)) && !(await js(`!!document.getElementById('extBn')`)));
await send('Page.navigate', {url}); await sleep(2200);
check('and it stays hidden after a restart of the page', !(await js(`!!document.getElementById('extBn')`)));

// Options → Browser still says it, with the button
await js(`localStorage.removeItem('pw.extBn')`); await send('Page.navigate', {url}); await sleep(2200);
await js(`document.querySelector('#menubar [data-mb=tools]') && 0`);
await js(`showOptions('browser')`); await sleep(500);
const opt = await js(`document.querySelector('.opt').innerText`);
check('Options → Browser says a newer extension is available', opt.includes('A newer extension is available.'), opt.slice(-160).replace(/\n/g, ' | '));
check('and has the button there too', await js(`!!document.querySelector('.opt [data-act=extUpdate]')`));
await shot('options-en');

// Urdu
await reload('ur');
const ur = await bnText();
check('Urdu: the banner is translated', ur.includes('براؤزر ایکسٹینشن') && !/available|newer/i.test(ur), ur);
await jsClick('#extBn [data-act=extUpdate]'); await sleep(500);
const dlg = await js(`document.querySelector('#dlDlg .card').innerText`);
check('Urdu: the steps have no leftover English sentences', !/(Unzip it|Load it again|If you installed|is ready\.)/.test(dlg), dlg.slice(0, 200).replace(/\n/g, ' | '));
await shot('dialog-ur');
await jsClick('#extLater2'); await sleep(300);

// the Connect a browser dialog: one click opens the store page; the manual steps stay under a fold; Urdu has no English left
await js(`window.__opened = []; window.open = (u) => { window.__opened.push(u); return null; }`);
await js(`extConnect()`); await sleep(700);
const cdlg = await js(`document.querySelector('#dlDlg .card').innerText`);
check('Urdu: Connect a browser has no leftover English sentences', !/(Add the extension|Chrome Web Store|Come back here|Another browser)/.test(cdlg), cdlg.slice(0, 260).replace(/\n/g, ' | '));
await shot('connect-ur');
await jsClick('#extStore');
const opened = await js(`window.__opened`);
check('the store button opens the Chrome Web Store page', opened.length === 1 && opened[0] === 'https://chromewebstore.google.com/detail/pullwell-downloads/afmpagjigeklekbjmenipkphlcfijagi', JSON.stringify(opened));
await jsClick('#extStoreEdge');
const opened2 = await js(`window.__opened`);
check('the Edge button opens the Edge Add-ons page', opened2.length === 2 && opened2[1] === 'https://microsoftedge.microsoft.com/addons/detail/pullwell-downloads/agnnnoegcdohnfnpagclhppflhfjcelo', JSON.stringify(opened2));
check('the by-hand steps are still there', await js(`!!document.querySelector('#dlDlg details [data-saveext=firefox]') && !!document.querySelector('#extCopy')`));
await jsClick('#extDone'); await sleep(300);


// an extension that reports the current version: no banner, and Options names the version
await js(`localStorage.setItem('pw.lang', JSON.stringify('en')); localStorage.removeItem('pw.extBn')`);
const cur = (await (await ext('ext/status?ev=0.15.0', {headers: auth})).json());
check('the program accepts the version from the extension', cur.version !== undefined);
await send('Page.navigate', {url}); await sleep(2200);
check('a current extension gets no banner', !(await js(`!!document.getElementById('extBn')`)));
await js(`showOptions('browser')`); await sleep(500);
check('Options → Browser shows its version', (await js(`document.querySelector('.opt').innerText`)).includes('Extension 0.15.0'));
await shot('options-current-en');
await js(`localStorage.removeItem('pw.lang')`);
check('no script errors', errors.length === 0, errors.join(' ; '));
console.log(results.join('\n'));
console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
ws.close();
