// Frontend modernization checks. Run against an isolated test copy with fixture data.
// PLAYWRIGHT_MODULE=/path/to/playwright node scripts/uitest/modern_check.mjs <app-url> <out-dir>
import {createRequire} from 'node:module';
import fs from 'node:fs';
const require = createRequire(import.meta.url);
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const [url = 'http://127.0.0.1:9197/', out = '/tmp/pullwell-modern-check'] = process.argv.slice(2);
fs.mkdirSync(out, {recursive:true});
const browser = await chromium.launch({headless:true, args:['--no-sandbox']});
const page = await browser.newPage({viewport:{width:1280, height:800}, reducedMotion:'reduce'});
page.setDefaultTimeout(1200);
const errors = [], results = [];
page.on('pageerror', e => errors.push(e.message));
const check = async (name, fn) => {
  try { if (!await fn()) throw new Error('assertion returned false'); results.push({name, pass:true}); }
  catch(e) { results.push({name, pass:false, error:e.message}); }
};
await page.goto(url);
await page.waitForFunction(() => typeof DL !== 'undefined' && DL !== null);
await check('workspace has a current-view heading', () => page.locator('#viewTitle').innerText().then(t => t === 'All downloads'));
await check('keyboard skip link reaches main content', async () => {
  await page.locator('.skip-link').focus(); await page.keyboard.press('Enter');
  return page.evaluate(() => document.activeElement.id === 'main');
});
await check('filter updates the view heading', async () => {
  await page.locator('[data-view]').filter({hasText:/^Finished/}).click();
  return await page.locator('#viewTitle').innerText() === 'Finished';
});
await page.evaluate(() => { view = {k:'all'}; render(); });
await check('empty search offers a working clear action', async () => {
  await page.locator('#search').fill('no-such-file-849273');
  await page.locator('#clearSearch').click();
  return await page.locator('#search').inputValue() === '';
});
await check('row spacing control preserves its selected state', async () => {
  const button = page.locator('#densityToggle');
  await button.click();
  const compact = await button.getAttribute('aria-pressed');
  await button.click();
  return compact === 'true' && await button.getAttribute('aria-pressed') === 'false';
});
await page.locator('[data-tb=add]').click();
await check('new download input has an accessible name', () => page.locator('#addUrl').getAttribute('aria-label').then(Boolean));
await check('dialog has a named close control', () => page.locator('.dialog-close').getAttribute('aria-label').then(t => t === 'Close'));
await check('modal keyboard focus stays inside the dialog', async () => {
  const buttons = page.locator('#dlDlg button:visible');
  await buttons.last().focus(); await page.keyboard.press('Tab');
  const forward = await page.evaluate(() => !!document.activeElement.closest('#dlDlg'));
  await page.keyboard.press('Shift+Tab');
  return forward && await page.evaluate(() => !!document.activeElement.closest('#dlDlg'));
});
await page.keyboard.press('Escape');
await check('Escape restores focus to the opener', () => page.evaluate(() => document.activeElement.dataset.tb === 'add'));
await page.locator('[data-tb=opts]').click();
await check('options has accessible tab semantics', () => page.locator('[data-opt=general]').getAttribute('aria-selected').then(t => t === 'true'));
await check('options tabs support arrow-key navigation', async () => {
  await page.locator('[data-opt=general]').focus(); await page.keyboard.press('ArrowDown');
  return await page.locator('[data-opt=connection]').getAttribute('aria-selected') === 'true';
});
await page.keyboard.press('Escape');
await check('reduced motion disables dialog animation', async () => {
  await page.locator('[data-tb=add]').click();
  const duration = await page.locator('#dlDlg .card').evaluate(el => getComputedStyle(el).animationDuration);
  await page.keyboard.press('Escape'); return duration.split(',').every(t => parseFloat(t) <= 0.01);
});
for (const theme of ['light', 'dark']) {
  await page.evaluate(theme => { store.set('theme', theme); applyTheme(); }, theme);
  for (const width of [375, 768, 980, 1280, 1440]) {
    await page.setViewportSize({width, height:800});
    await check(`${theme} workspace fits ${width}px`, () => page.evaluate(() => {
      const main = document.querySelector('#main').getBoundingClientRect();
      const top = document.querySelector('.top').getBoundingClientRect();
      return document.documentElement.scrollWidth <= innerWidth && document.documentElement.scrollHeight <= innerHeight
        && main.left >= 0 && main.right <= innerWidth && top.right <= innerWidth;
    }));
    await page.screenshot({path:`${out}/${theme}-${width}.png`});
    await page.locator('[data-tb=add]').click();
    await check(`${theme} dialog fits ${width}px`, () => page.locator('#dlDlg .card').evaluate(el => { const r = el.getBoundingClientRect(); return r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight; }));
    await page.keyboard.press('Escape');
  }
}
await page.setViewportSize({width:1280, height:800});
await check('all table progress indicators have a status name', async () => {
  return page.locator('#tb .st').evaluateAll(els => els.every(el => el.querySelector('[role=progressbar]')?.getAttribute('aria-label')));
});
await check('polling preserves sidebar keyboard focus', async () => {
  await page.locator('[data-view]').first().focus();
  await page.waitForTimeout(1300);
  return await page.evaluate(() => document.activeElement.matches('[data-view]'));
});
// Deterministic, explicitly synthetic rendering fixtures. No server state is changed.
await page.evaluate(() => {
  loading = true;
  DL.items = [{id:'ui-fixture', name:'Finished', url:'https://files.example.test/a-report.pdf', category:'Documents', state:'paused', size:10485760, done:2621440, order:0, added:1700000000}];
  view = {k:'all'}; q = ''; sel.clear(); render();
});
await check('progress reports its actual numeric value', () => page.locator('#tb [role=progressbar]').getAttribute('aria-valuenow').then(v => v === '25'));
await check('count changes preserve focused sidebar control', async () => {
  await page.locator('[data-view]').first().focus();
  await page.evaluate(() => { DL.items.push({...DL.items[0],id:'ui-fixture-2'}); render(); });
  return page.evaluate(() => document.activeElement.dataset.view === JSON.stringify({k:'all'}));
});
await check('modal restores its sidebar opener after a live count change', async () => {
  await page.locator('#sideConnect').focus(); await page.keyboard.press('Enter');
  await page.locator('#dlDlg').waitFor();
  await page.evaluate(() => { DL.items.push({...DL.items[0],id:'ui-fixture-3'}); render(); });
  await page.keyboard.press('Escape');
  return page.evaluate(() => document.activeElement.id === 'sideConnect');
});
await check('speed changes preserve the global speed control focus', async () => {
  await page.locator('#stLimit').focus();
  await page.evaluate(() => { DL.speed = 1024000; renderStatus(); });
  return page.evaluate(() => document.activeElement.id === 'stLimit');
});
await check('native button keyboard activation does not trigger a download shortcut', async () => {
  await page.evaluate(() => { sel.add('ui-fixture'); cur = 'ui-fixture'; render(); });
  await page.locator('#densityToggle').focus(); await page.keyboard.press('Space');
  const on = await page.locator('#densityToggle').getAttribute('aria-pressed');
  await page.keyboard.press('Space');
  return on === 'true' && await page.locator('#dlDlg').count() === 0;
});
await page.locator('[data-tb=add]').click();
await check('F10 stays inside a modal', async () => {
  await page.keyboard.press('F10');
  return await page.locator('.ctx').count() === 0 && await page.evaluate(() => !!document.activeElement.closest('#dlDlg'));
});
await page.keyboard.press('Escape');
await check('all columns remain available when automatic fitting is off', async () => {
  await page.setViewportSize({width:768,height:800});
  await page.evaluate(() => { uiPrefs.fitColumns = false; applyUiPrefs(); });
  const visible = await page.locator('th[data-col=info]').isVisible();
  await page.evaluate(() => { uiPrefs.fitColumns = true; applyUiPrefs(); });
  return visible;
});
await page.setViewportSize({width:375,height:350});
await page.evaluate(() => progressBox('ui-fixture'));
await check('progress footer is reachable in a short narrow window', async () => {
  await page.locator('#pgClose').scrollIntoViewIfNeeded();
  return page.locator('#pgClose').evaluate(el => {const r=el.getBoundingClientRect();return r.top>=0&&r.bottom<=innerHeight&&r.right<=innerWidth;});
});
await page.keyboard.press('Escape');
await page.evaluate(() => openBarMenu(document.querySelector('[data-mb=downloads]'),true));
await check('long menus keep their final action reachable', async () => {
  await page.keyboard.press('End');
  return page.evaluate(() => {const r=document.activeElement.getBoundingClientRect();return !!document.activeElement.closest('.ctx')&&r.top>=0&&r.bottom<=innerHeight;});
});
await page.keyboard.press('Escape');
await page.setViewportSize({width:980,height:560});
await page.evaluate(() => { store.set('zoom',1.3); applyZoom(); });
await check('130% text keeps the workspace inside its viewport', () => page.locator('#main').evaluate(el => el.getBoundingClientRect().right<=innerWidth));
await page.evaluate(() => { store.set('zoom',1); applyZoom(); });
await page.setViewportSize({width:1280,height:800});
await page.evaluate(() => { localStorage.setItem('pw.lang', JSON.stringify('ur')); });
await page.reload(); await page.waitForFunction(() => typeof DL !== 'undefined' && DL);
await page.evaluate(() => {
  loading = true; view = {k:'all'};
  DL.items = [{id:'literal-name',name:'Finished',url:'https://files.example.test/report.pdf',category:'Documents',state:'paused',size:1000,done:0,order:0}];
  render();
});
await page.waitForTimeout(80);
await check('Urdu preserves a literal filename matching a translated phrase', () => page.locator('#tb .tx').innerText().then(t => t === 'Finished'));
await page.locator('#tb .tx').evaluate(el => {el.firstChild.data='Options';});
await page.waitForTimeout(80);
await check('Urdu mutation observer preserves updated literal filenames', () => page.locator('#tb .tx').innerText().then(t => t === 'Options'));
await check('Urdu translates the new interface copy', () => page.locator('#viewHint').innerText().then(t => t !== 'Your files, moving in the right direction.'));
await page.setViewportSize({width:375,height:800});
await check('Urdu layout fits a narrow viewport', () => page.locator('#main').evaluate(el => document.documentElement.dir==='rtl'&&el.getBoundingClientRect().right<=innerWidth));
await page.screenshot({path:`${out}/urdu-375.png`});
await page.evaluate(() => localStorage.removeItem('pw.lang'));
await check('no runtime exceptions', () => errors.length === 0);
await browser.close();
for (const r of results) console.log(`${r.pass ? 'PASS' : 'FAIL'} ${r.name}${r.error ? ': ' + r.error : ''}`);
console.log(`${results.filter(r => r.pass).length}/${results.length} passed`);
fs.writeFileSync(`${out}/results.json`, JSON.stringify({results, errors}, null, 2));
if (results.some(r => !r.pass)) process.exitCode = 1;
