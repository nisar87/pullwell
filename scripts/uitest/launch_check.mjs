// The browser starting the program by itself (native messaging), in a real Chromium: the extension finds the program
// closed and asks the browser to start it, a big download is then taken over, and with no starter registered the
// browser keeps the download as usual.
// Usage: node launch_check.mjs <built-program> <work-dir> [fixtures-url]
// The program is copied into <work-dir>/app and started only by the browser. Needs the fixtures server (scripts/dmtest/fixtures.py).
import {createRequire} from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import http from 'node:http';
import {execSync, spawn} from 'node:child_process';
const WIN = process.platform === 'win32';
const EXE = WIN ? 'pw.exe' : 'pw';
let playwright;
for (const base of [import.meta.url, '/opt/']) { try { playwright = createRequire(base)('playwright'); break; } catch {} }
if (!playwright) { console.error('playwright is not installed'); process.exit(2); }
const {chromium} = playwright;
const [program0, work0, fixtures = 'http://127.0.0.1:8765'] = process.argv.slice(2);
const program = path.resolve(program0);
const work = path.resolve(work0).replace(/\\/g, '/');
const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const URL0 = 'http://127.0.0.1:9197/', H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const EXT_ID = 'lkhdafbofbljmkmcpgmbeopfifpgedff';
const results = []; const check = (n, ok, x) => results.push(`${ok ? 'PASS' : 'FAIL'}  ${n}${x !== undefined ? '  (' + x + ')' : ''}`);
const sleep = ms => new Promise(r => setTimeout(r, ms));
const up = async () => { try { const j = await (await fetch(URL0 + 'ext/ping')).json(); return j.app === 'pullwell'; } catch { return false; } };
const waitFor = async (f, ms) => { for (let t = 0; t < ms; t += 200) { if (await f()) return true; await sleep(200); } return false; };
const post = (p, b) => fetch(URL0 + p, {method: 'POST', headers: H, body: JSON.stringify(b)}).then(r => r.json());
const items = async () => { try { return (await (await fetch(URL0 + 'api/downloads')).json()).items || []; } catch { return []; } };
// a program that is shutting down still answers 404 for a moment, so wait until the port refuses connections
const closed = async () => { try { await fetch(URL0 + 'ext/ping'); return false; } catch { return true; } };
const quit = async () => { try { await post('api/app', {quit: true}); } catch {} await waitFor(closed, 15000); };
// how many copies of the program run (on Windows only the starter-made ones matter: the test starts none itself after the first)
const count = () => {
  try {
    if (WIN) return execSync('tasklist /FI "IMAGENAME eq pw.exe" /NH').toString().split('\n').filter(l => /^pw\.exe/i.test(l.trim())).length;
    return Number(execSync(`pgrep -fc '${work}/app/[p]w -tray'`).toString()); // [p]: the search does not find itself
  } catch { return 0; }
};
// where the browsers look for the starter on Windows (the program writes these keys itself)
const regKeys = ['Google\\Chrome', 'Microsoft\\Edge', 'Chromium', 'Mozilla'].map(k => `HKCU\\Software\\${k}\\NativeMessagingHosts\\pullwell.launcher`);

if (await up()) { console.error('a program already answers on port 9197: stop it first'); process.exit(2); }
fs.rmSync(work, {recursive: true, force: true}); fs.mkdirSync(work + '/app/data', {recursive: true}); fs.mkdirSync(work + '/dl', {recursive: true});
fs.copyFileSync(program, work + '/app/' + EXE); if (!WIN) fs.chmodSync(work + '/app/' + EXE, 0o755);
fs.writeFileSync(work + '/app/data/app.json', '{"diagNoticed":true,"noDiag":true}');
// the extension, built from the repository (its manifest carries the fixed key)
execSync(`${WIN ? 'python' : 'python3'} extension/build.py`, {cwd: repo});
fs.mkdirSync(work + '/ext', {recursive: true});
execSync(`${WIN ? 'python' : 'python3'} -c "import zipfile;zipfile.ZipFile('${repo.replace(/\\/g, '/')}/extension/dist/Pullwell-extension-chrome-edge.zip').extractall('${work}/ext')"`);
// the test proxy: the program and the browser both reach the fixtures server through the made-up host name
const fixtureHost = '192.0.2.123';
const proxy = http.createServer((rq, rs) => {
  let t; try { t = new URL(rq.url); } catch { rs.writeHead(400).end(); return; }
  if (t.hostname !== fixtureHost) { rs.writeHead(403).end(); return; }
  const u = new URL(t.pathname + t.search, fixtures);
  const f = http.request(u, {method: rq.method, headers: {...rq.headers, host: u.host}}, r => { rs.writeHead(r.statusCode, r.headers); r.pipe(rs); });
  f.on('error', () => rs.writeHead(502).end()); rq.pipe(f);
});
await new Promise(r => proxy.listen(0, '127.0.0.1', r));
const proxyURL = `http://127.0.0.1:${proxy.address().port}`;

// the starter: on Windows the program registers it itself when it runs (keys under HKCU, checked below); elsewhere the
// test writes the manifest the browser reads from its profile
const hostDir = work + '/profile/NativeMessagingHosts', hostFile = hostDir + '/pullwell.launcher.json';
fs.mkdirSync(hostDir, {recursive: true});
const manifest = JSON.stringify({name: 'pullwell.launcher', description: 'test', path: work + '/app/' + EXE, type: 'stdio', allowed_origins: [`chrome-extension://${EXT_ID}/`]});
const regQuery = k => { try { return execSync(`reg query "${k}" /ve`, {stdio: ['ignore', 'pipe', 'ignore']}).toString(); } catch { return ''; } };
const starterOn = () => { if (!WIN) fs.writeFileSync(hostFile, manifest); else for (const k of regKeys) execSync(`reg add "${k}" /ve /t REG_SZ /d "${(work + '/app/data/browser-host-chrome.json').replace(/\//g, '\\')}" /f`, {stdio: 'ignore'}); };
const starterOff = () => { if (!WIN) fs.rmSync(hostFile); else for (const k of regKeys) try { execSync(`reg delete "${k}" /f`, {stdio: 'ignore'}); } catch {} };
if (WIN) {
  // the person opens the program once: it writes the starter's manifest and registers it for the browsers
  for (const k of regKeys) try { execSync(`reg delete "${k}" /f`, {stdio: 'ignore'}); } catch {}
  spawn(work + '/app/' + EXE, ['-tray'], {detached: true, stdio: 'ignore'}).unref();
  check('the program comes up when opened', await waitFor(up, 30000));
  const man = fs.existsSync(work + '/app/data/browser-host-chrome.json') ? fs.readFileSync(work + '/app/data/browser-host-chrome.json', 'utf8') : '';
  check('it wrote the starter\'s manifest', man.includes(EXT_ID) && man.toLowerCase().includes('pw.exe'), man.replace(/\s+/g, ' ').slice(0, 160));
  check('and registered it for Chrome, Edge, Chromium and Firefox', regKeys.every(k => regQuery(k).includes('browser-host')), regKeys.map(k => regQuery(k).trim().split('\n').pop()).join(' | '));
  await quit();
} else starterOn();

const ctx = await chromium.launchPersistentContext(work + '/profile', {headless: true, channel: 'chromium', acceptDownloads: true,
  args: [`--disable-extensions-except=${work}/ext`, `--load-extension=${work}/ext`, `--proxy-server=${proxyURL}`]});
let sw = ctx.serviceWorkers()[0] || await ctx.waitForEvent('serviceworker', {timeout: 10000});
const extId = sw.url().split('/')[2];
check('the extension has the fixed address the starter allows', extId === EXT_ID, extId);
await sleep(1500);
await quit();
check('the program is not running before the test', !(await up()));

// 1. opening the popup: the extension finds the program closed, the browser starts it
const pop = await ctx.newPage();
await pop.goto(`chrome-extension://${extId}/popup.html`);
check('opening the popup starts the program', await waitFor(up, 20000));
check('exactly one copy runs', count() === 1, 'copies: ' + count());
await pop.waitForSelector('#v-pair:not([hidden])', {timeout: 10000}).catch(() => {});
check('the popup then asks for the pairing code', await pop.locator('#v-pair').isVisible());

// 2. pair, and make the program use the test proxy and a download folder in /tmp
const code = (await post('api/downloads', {action: 'extCode'})).code;
await pop.fill('#code', String(code)); await pop.click('#pair'); await sleep(1500);
check('pairing works', await sw.evaluate(async () => !!(await chrome.storage.local.get('token')).token));
const cfg = (await (await fetch(URL0 + 'api/downloads', {headers: H})).json()).config; cfg.dir = work + '/dl';
await post('api/downloads', {action: 'config', config: cfg});
await fetch(URL0 + 'api/app', {method: 'POST', headers: H, body: JSON.stringify({proxy: {mode: 'manual', addr: proxyURL}})});
await post('api/downloads', {action: 'extConfig', extMinMB: 5, extOff: false, extNoAsk: true});

// 3. the program is closed; the person clicks a big download: the browser starts the program and it takes the file
await quit();
check('the program was closed', !(await up()));
const page = await ctx.newPage();
await page.setContent(`<a id="big" href="http://${fixtureHost}/file/24MB">big</a><a id="big2" href="http://${fixtureHost}/file/26MB">big2</a>`);
await page.click('#big');
const started = await waitFor(up, 25000);
check('a download starts the program again', started);
await sleep(2500);
let its = await items();
check('and the program takes the download over', its.some(v => v.url.includes('/file/24MB')), its.map(v => v.url).join(' | '));
const bdl = await sw.evaluate(async () => (await chrome.downloads.search({})).map(d => d.url + ' ' + d.state));
check('the browser does not keep it', !bdl.some(s => s.includes('24MB') && !s.includes('interrupted')), bdl.join(' | '));
check('still exactly one copy', count() === 1, 'copies: ' + count());

// 4. no starter registered (the program was never opened on this computer): the browser downloads as usual
await quit();
// a check in the background (the timer) must not bring back a program the person quit
await sw.evaluate(() => refresh(true)); await sleep(1500);
check('a background check does not start a program that was quit', !(await up()) && count() === 0);
starterOff();
await sw.evaluate(() => { noLaunchUntil = 0; helperMissing = false; });
await page.click('#big2');
await sleep(6000);
check('without a starter the program stays closed', !(await up()));
const got = await sw.evaluate(async () => (await chrome.downloads.search({})).filter(d => d.url.includes('26MB')).map(d => d.state));
check('and the browser keeps the download', got.length > 0 && got.some(s => s === 'complete' || s === 'in_progress'), got.join(','));
await pop.goto(`chrome-extension://${extId}/popup.html`);
await pop.waitForSelector('#v-down:not([hidden])', {timeout: 10000}).catch(() => {});
check('the popup tells to open the program once', await pop.locator('#hint').isVisible());

// 5. the starter is there again (the program was opened): "Try again" starts it
starterOn();
await pop.click('#retry');
check('"Try again" starts the program', await waitFor(up, 25000));

await quit(); await ctx.close(); proxy.closeAllConnections(); proxy.close();
console.log(results.join('\n'));
const fail = results.filter(r => r.startsWith('FAIL')).length;
console.log(`${results.length - fail}/${results.length} passed`);
process.exit(fail ? 1 : 0);
