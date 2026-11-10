// The browser extension, as the program hands it out, in a real Chromium: pairing with the code from the program,
// a big download taken over, a small one left to the browser, the right-click menu, and the browser keeping the
// download when the program is not running.
// Usage: node ext_check.mjs <app-url> <work-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
import {execSync} from 'node:child_process';
const require = createRequire('/opt/');
const {chromium} = require('playwright');
const [url, work] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const results = []; const check = (n, ok, x) => results.push(`${ok ? 'PASS' : 'FAIL'}  ${n}${x !== undefined ? '  (' + x + ')' : ''}`);
const sleep = ms => new Promise(r => setTimeout(r, ms));
const items = async () => (await (await fetch(url + 'api/downloads')).json()).items || [];

const zip = Buffer.from(await (await fetch(url + 'extension/Pullwell-extension-chrome-edge.zip')).arrayBuffer());
check('the program hands out the extension', zip.length > 1000 && zip[0] === 0x50, zip.length + ' bytes');
fs.rmSync(work, {recursive: true, force: true}); fs.mkdirSync(work + '/ext', {recursive: true});
fs.writeFileSync(work + '/ext.zip', zip); execSync(`cd ${work}/ext && python3 -c "import zipfile;zipfile.ZipFile('../ext.zip').extractall('.')"`);
const ctx = await chromium.launchPersistentContext(work + '/profile', {headless: true, channel: 'chromium', acceptDownloads: true,
  args: [`--disable-extensions-except=${work}/ext`, `--load-extension=${work}/ext`, '--host-resolver-rules=MAP files.example.test 127.0.0.1:8765']});
let sw = ctx.serviceWorkers()[0] || await ctx.waitForEvent('serviceworker', {timeout: 10000});
const extId = sw.url().split('/')[2];
check('the extension loads', !!extId, extId);

const code = (await (await fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify({action: 'extCode'})})).json()).code;
check('the program gives a pairing code', /^\w{4,}/.test(code || ''), code);
const pop = await ctx.newPage();
await pop.goto(`chrome-extension://${extId}/popup.html`); await sleep(800);
await pop.fill('#code', String(code)); await pop.click('#pair'); await sleep(1500);
const paired = await sw.evaluate(async () => !!(await chrome.storage.local.get('token')).token);
check('pairing with the code works', paired);
const dl = await (await fetch(url + 'api/downloads')).json();
check('the program lists the connected browser', JSON.stringify(dl).includes('Chrome'));
await sw.evaluate(() => refresh()); await sleep(300);

// a big download is taken over
const n0 = (await items()).length;
const page = await ctx.newPage();
const browserGot = [];
ctx.on('download', d => browserGot.push(d));
page.on('download', d => browserGot.push(d));
await page.setContent(`<a id="big" href="http://files.example.test/file/24MB">big</a><a id="small" href="http://files.example.test/file/1MB">small</a>`);
await page.click('#big'); await sleep(3000);
let its = await items();
check('a big download goes to the program', its.length === n0 + 1 && its.some(v => v.url.includes('files.example.test/file/24MB')), its.map(v => v.url).join(' | '));
const bdl = await sw.evaluate(async () => (await chrome.downloads.search({})).map(d => d.url + ' ' + d.state));
check('the browser does not keep it', !bdl.some(s => s.includes('24MB') && !s.includes('interrupted')), bdl.join(' | '));
// a small one stays in the browser
await page.click('#small'); await sleep(2500);
its = await items();
check('a small download stays in the browser', !its.some(v => v.url.includes('/file/1MB') && v.url.includes('example.test')));
// the right-click menu exists (clicking it is the same "send" as above)
const menu = await sw.evaluate(() => new Promise(r => chrome.contextMenus.update('nm', {}, () => r(!chrome.runtime.lastError))));
check('"Download with ..." is in the right-click menu', menu);

// the program is not reachable: the browser keeps the download
process.stdout.write(''); fs.writeFileSync(work + '/stop', '1');
console.error('STOP_PROGRAM'); // the shell stops the program, then touches work/stopped
for (let i = 0; i < 40 && !fs.existsSync(work + '/stopped'); i++) await sleep(250);
await sw.evaluate(() => chrome.storage.local.set({port: 9}));
await page.setContent(`<a id="big2" href="http://files.example.test/file/20MB">big</a>`);
await page.click('#big2'); await sleep(7000);
const after = await sw.evaluate(async () => (await chrome.downloads.search({})).filter(d => d.url.includes('20MB')).map(d => d.state + ' ' + d.bytesReceived));
check('with the program closed the browser downloads it itself', after.some(s => /complete|in_progress/.test(s) && !/^interrupted/.test(s)), after.join(' | '));

console.log(results.join('\n')); console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
await ctx.close();
