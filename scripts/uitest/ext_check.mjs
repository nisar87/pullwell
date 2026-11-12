// The browser extension, as the program hands it out, in a real Chromium: pairing with the code from the program,
// a big download taken over, a small one left to the browser, the right-click menu, and the browser keeping the
// download when the program is not running.
// Usage: node ext_check.mjs <app-url> <work-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
import {execSync} from 'node:child_process';
// playwright comes from the nearest node_modules (next to this repository), or the machine-wide /opt install
let playwright;
for (const base of [import.meta.url, '/opt/']) {
  try { playwright = createRequire(base)('playwright'); break; } catch {}
}
if (!playwright) { console.error('playwright is not installed (npm i playwright, npx playwright install chromium)'); process.exit(2); }
const {chromium} = playwright;
const [url, work] = process.argv.slice(2);
const H = {'Content-Type': 'application/json', 'X-Pullwell': '1'};
const results = []; const check = (n, ok, x) => results.push(`${ok ? 'PASS' : 'FAIL'}  ${n}${x !== undefined ? '  (' + x + ')' : ''}`);
const sleep = ms => new Promise(r => setTimeout(r, ms));
const items = async () => (await (await fetch(url + 'api/downloads')).json()).items || [];

const zip = Buffer.from(await (await fetch(url + 'extension/Pullwell-extension-chrome-edge.zip')).arrayBuffer());
check('the program hands out the extension', zip.length > 1000 && zip[0] === 0x50, zip.length + ' bytes');
fs.rmSync(work, {recursive: true, force: true}); fs.mkdirSync(work + '/ext', {recursive: true});
fs.writeFileSync(work + '/ext.zip', zip);
execSync(`${process.platform === 'win32' ? 'python' : 'python3'} -c "import zipfile;zipfile.ZipFile('../ext.zip').extractall('.')"`, {cwd: work + '/ext'});
// the button over videos only exists where the person allowed "all sites"; a browser test cannot click that permission
// prompt, so the test copy of the extension has the permission from the start
const mf = JSON.parse(fs.readFileSync(work + '/ext/manifest.json', 'utf8'));
mf.host_permissions = [...(mf.host_permissions || []), '<all_urls>']; delete mf.optional_host_permissions;
fs.writeFileSync(work + '/ext/manifest.json', JSON.stringify(mf, null, 2));
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

// the default: the program asks first (IDM-style "Download file info" box) and adds nothing until the person agrees
const post = b => fetch(url + 'api/downloads', {method: 'POST', headers: H, body: JSON.stringify(b)}).then(r => r.json());
const askedNow = async () => (await (await fetch(url + 'api/downloads')).json()).ext.asked;
const nA = (await items()).length, a0 = await askedNow(); const pa = await ctx.newPage();
await pa.setContent(`<a id="a" href="http://files.example.test/file/22MB">ask</a>`); await pa.click('#a'); await sleep(2500);
// (on Windows the program's own window may already have taken it and shown the box, so the box's queue can be empty)
const asked = await post({action: 'takeAsk'});
check('a browser download waits for the person\'s yes', (await items()).length === nA && (await askedNow()) === a0 + 1, 'held: ' + ((await askedNow()) - a0));
if (asked.ask) check('the box gets the browser\'s details', asked.ask.url.includes('/file/22MB') && !!(asked.ask.header && asked.ask.header['User-Agent']), JSON.stringify(Object.keys(asked.ask.header || {})));
// Cancel in the box, and the page starts the same download again: it is not asked again (a retrying page used to bring the box back every time)
await post({action: 'askDecline', url: 'http://files.example.test/file/22MB'}); const a1 = await askedNow();
await pa.click('#a'); await sleep(2500);
check('a link cancelled in the box is not asked about again when the browser starts it again', (await askedNow()) === a1, 'asked ' + ((await askedNow()) - a1) + ' more time(s)');
await pa.close();
await post({action: 'extConfig', extMinMB: 5, extOff: false, extNoAsk: true}); // the checks below look at the direct hand-over
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
// a link with no file ending and no announced size (like testfile.org/files-5GB) is taken by its content type
const pg2 = await ctx.newPage();
await pg2.setContent(`<a id="u" href="http://files.example.test/nolength/31MB">unknown size</a>`);
const nU = (await items()).length; await pg2.click('#u'); await sleep(2500);
check('a download without a size or file ending is taken by its content type', (await items()).length === nU + 1);
await pg2.close();
// a small one stays in the browser
await page.click('#small'); await sleep(2500);
its = await items();
check('a small download stays in the browser', !its.some(v => v.url.includes('/file/1MB') && v.url.includes('example.test')));
// the small button over a video: it sends the video's link to the program
const vpage = await ctx.newPage();
await vpage.route('http://files.example.test/video-page', r => r.fulfill({contentType: 'text/html', body: `<body style="margin:0"><video id="v" controls src="http://files.example.test/file/23MB" style="width:480px;height:270px;display:block"></video></body>`}));
await vpage.goto('http://files.example.test/video-page');
await sleep(2500);
await vpage.mouse.move(200, 120); await sleep(400); await vpage.mouse.move(210, 130); await sleep(600);
const vb = await vpage.evaluate(() => { const h = [...document.documentElement.children].find(e => e.shadowRoot && e.shadowRoot.querySelector('button')); return h ? {shown: getComputedStyle(h).display !== 'none', text: h.shadowRoot.querySelector('button').textContent} : null; });
check('a button appears over a video', !!vb && vb.shown && /Download with/.test(vb.text), JSON.stringify(vb));
const nV = (await items()).length; const askQ0 = await post({action: 'extConfig', extMinMB: 5, extOff: false, extNoAsk: true});
await vpage.evaluate(() => [...document.documentElement.children].find(e => e.shadowRoot && e.shadowRoot.querySelector('button')).shadowRoot.querySelector('button').click()); await sleep(2500);
check('clicking it sends the video to the program', (await items()).length === nV + 1 && (await items()).some(v => v.url.includes('/file/23MB')));
await vpage.close();
// the right-click menu exists (clicking it is the same "send" as above)
const menu = await sw.evaluate(() => new Promise(r => chrome.contextMenus.update('nm', {}, () => r(!chrome.runtime.lastError))));
check('"Download with ..." is in the right-click menu', menu);

// languages: the popup follows the browser's language (popup.html?lang=xx stands in for it here)
for (const [lg, retry, rtl] of [['ur', 'دوبارہ کوشش کریں', true]]) {
  const lp = await ctx.newPage();
  await lp.goto(`chrome-extension://${extId}/popup.html?lang=${lg}`); await sleep(600);
  const st = await lp.evaluate(() => ({retry: document.getElementById('retry').textContent, dir: document.documentElement.dir || 'ltr', h1: document.querySelector('h1').textContent.trim(), ph: document.getElementById('code').placeholder}));
  check(`the popup in ${lg}: a button is translated`, st.retry === retry, st.retry);
  check(`the popup in ${lg}: ${rtl ? 'right-to-left' : 'left-to-right'}`, st.dir === (rtl ? 'rtl' : 'ltr'), st.dir);
  check(`the popup in ${lg}: the name and the code box are kept`, /Pullwell/.test(st.h1) && st.ph === 'xxxxx-xxxxx', st.h1 + ' | ' + st.ph);
  await lp.close();
}
const en0 = await ctx.newPage(); await en0.goto(`chrome-extension://${extId}/popup.html?lang=en`); await sleep(400);
check('the popup in English stays English', (await en0.evaluate(() => document.getElementById('retry').textContent)) === 'Try again'); await en0.close();
// the button over a video follows the language too, and a web page's own text is never translated
{
  const lv = await ctx.newPage();
  await lv.route('http://files.example.test/lang-page', r => r.fulfill({contentType: 'text/html', body: '<body><p id="p">Try again</p></body>'}));
  await lv.goto('http://files.example.test/lang-page'); await sleep(1200);
  check('a web page keeps its own text (nothing translated in it)', (await lv.evaluate(() => document.getElementById('p').textContent)) === 'Try again');
  await lv.close();
}

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
