// Languages: every shipped language shows translated menus and buttons, flips right-to-left where it should, remembers the choice, and leaves no English in Options.
// Usage: node lang_check.mjs <debug-port> <app-url> <fixtures-url> <out-dir>
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
await send('Runtime.enable'); await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', {width: 1180, height: 760, deviceScaleFactor: 1, mobile: false});
const open = async lang => {
  await send('Page.navigate', {url}); await sleep(1200);
  await js(`localStorage.setItem('pw.lang', JSON.stringify(${JSON.stringify(lang)}))`);
  await send('Page.navigate', {url}); await sleep(2200);
};
// English words that may stay as they are in every language: names, units, key names, file types
// a text counts as English left over when it uses words that no other language here uses
const ENGLISH = /\b(the|and|your|with|from|this|that|when|will|are|have|you|its|not|for the|click|choose|downloads? (is|are))\b/i;
const leftovers = () => js(`(() => {
  const out = new Set(), w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  for (let n; (n = w.nextNode());) {
    const p = n.parentElement; if (!p || ['SCRIPT','STYLE','CODE'].includes(p.tagName) || p.closest('[hidden]') || !p.getClientRects().length) continue;
    const t = n.data.replace(/\\s+/g, ' ').trim(); if (!/[A-Za-z]{4}/.test(t)) continue;
    out.add(t);
  }
  return [...out];
})()`);
const langs = await js('I18N.shipped');
check('the program offers its languages', Array.isArray(langs) && langs.length >= 6, langs && langs.join(','));
const RTL = ['ar', 'ur'];
const enBar = 'Tasks,Downloads,View,Tools,Help';
for (const lang of ['en', ...langs]) {
  await open(lang);
  const st = await js(`({lang: document.documentElement.lang, dir: document.documentElement.dir || 'ltr', add: document.querySelector('[data-tb=add] span').textContent, bar: [...document.querySelectorAll('#menubar button')].map(b => b.textContent).join(','), search: document.querySelector('#search').placeholder})`);
  check(`${lang}: page language and direction`, st.lang === lang && st.dir === (RTL.includes(lang) ? 'rtl' : 'ltr'), JSON.stringify([st.lang, st.dir]));
  if (lang === 'en') { check('en: stays English', st.add === 'New download' && st.bar === enBar); continue; }
  check(`${lang}: toolbar button is translated`, st.add !== 'New download' && st.add.length > 0, st.add);
  check(`${lang}: the menu names are translated`, st.bar.split(',').length === 5 && st.bar.split(',').filter((w, i) => w !== enBar.split(',')[i]).length >= 3, st.bar);
  check(`${lang}: search box hint is translated`, st.search !== 'Search downloads (Ctrl+F)', st.search);
  check(`${lang}: the toolbar fits the window (1180 px wide)`, await js(`(() => { const t = document.querySelector('.tb'); const last = [...t.querySelectorAll('.tbtn')].pop().getBoundingClientRect(); return t.scrollWidth <= t.clientWidth + 1 && last.right <= innerWidth && last.left >= 0; })()`), await js(`[...document.querySelectorAll('.tb .tbtn')].map(b => b.textContent.trim()).join(' | ')`));
  // Options, every tab
  await js(`document.querySelector('[data-tb=opts]').click()`); await sleep(500);
  check(`${lang}: Options shows the chosen language`, (await js(`(document.querySelector('#optLang') || {}).value`)) === lang);
  const tabs = await js(`[...document.querySelectorAll('.opt nav button')].map(b => b.dataset.opt)`);
  const left = new Set();
  for (const t of tabs) {
    await js(`document.querySelector('.opt nav button[data-opt="${t}"]').click()`); await sleep(350);
    for (const x of await leftovers()) if (ENGLISH.test(x)) left.add(x);
    if (t === 'general') await shot(`lang-${lang}-options`);
  }
  check(`${lang}: Options has no leftover English`, left.size === 0, [...left].slice(0, 8).join(' | '));
  await key('Escape'); await sleep(200);
  await shot(`lang-${lang}-main`);
}

// changing the language from Options reloads the page in that language and keeps it
await open('en');
await js(`document.querySelector('[data-tb=opts]').click()`); await sleep(500);
await js(`(() => { const s = document.querySelector('#optLang'); s.value = 'de'; s.dispatchEvent(new Event('change', {bubbles: true})); })()`); await sleep(2500);
check('choosing Deutsch in Options switches the window', (await js('document.documentElement.lang')) === 'de' && (await js(`document.querySelector('[data-tb=add] span').textContent`)) !== 'New download');
await send('Page.navigate', {url}); await sleep(2200);
check('the choice is still there after a restart of the page', (await js('document.documentElement.lang')) === 'de');
await js(`localStorage.removeItem('pw.lang')`); await send('Page.navigate', {url}); await sleep(2000);
check('automatic follows the browser language (English here)', (await js('document.documentElement.lang')) === 'en');
// the View menu lists the languages too
await js(`document.querySelector('#menubar [data-mb=view]').click()`); await sleep(200);
check('View menu has a Language entry', await js(`[...document.querySelectorAll('.ctx > button')].some(b => b.textContent.trim().startsWith('Language'))`));
await key('Escape'); await sleep(100);
check('no script errors', errors.length === 0, errors.join(' ; '));
console.log(results.join('\n'));
console.log(`${results.filter(r => r.startsWith('PASS')).length}/${results.length} passed`);
ws.close();
