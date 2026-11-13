// The problem-reports notice and switch. Needs a test copy started with: UIT_NOTICE=1 UIT=<dir> scripts/uitest/env.sh fresh
// Usage: node diag_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
import fs from 'node:fs';
const [port, url, fx, out] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const app = async b => (await fetch(url + 'api/app', b ? {method: 'POST', headers: H, body: JSON.stringify(b)} : {})).json();
const sleep = ms => new Promise(r => setTimeout(r, ms));
const results = [];
const check = (name, ok, extra) => { console.error((ok ? 'PASS ' : 'FAIL ') + name); results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra !== undefined ? '  (' + extra + ')' : ''}`); };
const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => ws.onopen = r);
let id = 0; const wait = new Map(); const errors = [];
ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && wait.has(m.id)) { wait.get(m.id)(m); wait.delete(m.id); return; }
  if (m.method === 'Runtime.exceptionThrown') errors.push('exception: ' + (m.params.exceptionDetails.exception?.description || m.params.exceptionDetails.text)); };
const send = (method, params = {}) => new Promise(r => { const i = ++id; wait.set(i, r); ws.send(JSON.stringify({id: i, method, params})); });
const js = async expr => (await send('Runtime.evaluate', {expression: expr, awaitPromise: true, returnByValue: true})).result?.result?.value;
await send('Runtime.enable');
await send('Emulation.setDeviceMetricsOverride', {width: 1180, height: 760, deviceScaleFactor: 1, mobile: false});
await send('Page.navigate', {url}); await sleep(2500);

const s0 = (await app()).diag;
check('a fresh copy has reports on and the notice not yet shown', s0.on === true && s0.noticed === false, JSON.stringify(s0));
check('the notice is in the window and says what goes where', await js(`!!document.querySelector('#diagBn') && /log goes to the developer/.test(document.querySelector('#diagBn').textContent) && /no links/i.test(document.querySelector('#diagBn').textContent)`));
await send('Page.captureScreenshot', {format: 'png'}).then(r => { fs.mkdirSync(out, {recursive: true}); fs.writeFileSync(out + '/diag_notice.png', Buffer.from(r.result.data, 'base64')); });
check('nothing is sent while the notice is open (the test copy never sends)', (await app()).diag.last === 0);

// Options, Advanced shows the switch, on
await js(`showOptions('advanced')`); await sleep(500);
check('Options, Advanced has the Send problem reports switch (on)', await js(`document.querySelector('#diagTog')?.getAttribute('aria-checked') === 'true' && /never has links/.test(document.querySelector('#optPanel').textContent) && /No report sent yet/.test(document.querySelector('#optPanel').textContent)`));
await send('Page.captureScreenshot', {format: 'png'}).then(r => fs.writeFileSync(out + '/diag_options.png', Buffer.from(r.result.data, 'base64')));
await js(`dlClose()`); await sleep(200);

// "Keep on" closes the notice and keeps it on
await js(`document.querySelector('[data-act="diagOk"]').click()`); await sleep(800);
let s1 = (await app()).diag;
check('Keep on records that the notice was seen and keeps reports on', s1.on === true && s1.noticed === true && !(await js(`!!document.querySelector('#diagBn')`)), JSON.stringify(s1));

// the switch turns it off and on
await js(`showOptions('advanced')`); await sleep(400);
await js(`document.querySelector('#diagTog').click()`); await sleep(800);
check('the switch turns reports off', (await app()).diag.on === false && await js(`document.querySelector('#diagTog').getAttribute('aria-checked') === 'false'`));
await js(`document.querySelector('#diagTog').click()`); await sleep(800);
check('and on again', (await app()).diag.on === true);
await js(`document.querySelector('#diagTog').click()`); await sleep(500); // leave it off, as the other suites expect

// Urdu
await js(`I18N.set('ur')`); await sleep(2500);
await js(`showOptions('advanced')`); await sleep(500);
check('Urdu: the switch row is translated', await js(`/مسائل کی رپورٹیں/.test(document.querySelector('#optPanel').textContent) && /ڈویلپر/.test(document.querySelector('#optPanel').textContent)`));
await send('Page.captureScreenshot', {format: 'png'}).then(r => fs.writeFileSync(out + '/diag_options_ur.png', Buffer.from(r.result.data, 'base64')));
await js(`I18N.set('en')`); await sleep(1500);
check('no script errors', errors.length === 0, errors.join(' | '));
fs.mkdirSync(out, {recursive: true}); fs.writeFileSync(out + '/diag_results.txt', results.join('\n') + '\n');
ws.close();
process.exit(results.some(r => r.startsWith('FAIL')) ? 1 : 0);
