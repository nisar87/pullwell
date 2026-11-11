// {{NAME}} browser extension: hands big downloads to {{NAME}}, which downloads them with several connections at once.
if (typeof importScripts === 'function') importScripts('core.js');
const api = globalThis.browser || globalThis.chrome;
// The program listens on the first free port of this list (the same list as in its server.go, filled in by build.py).
// The extension must find it before it can pair, so the list is fixed and tried in order.
const PORTS = {{PORTS}};
const APP_ID = '{{SCHEME}}';

// base finds the program: the port that worked last time, else the first port whose /ext/ping answers as this app
// (another program on the same port is not mistaken for it).
async function base(force) {
  const s = await api.storage.local.get(['port']);
  if (s.port && !force) return 'http://127.0.0.1:' + s.port;
  for (const p of PORTS) {
    const ctl = new AbortController();
    const t = setTimeout(() => ctl.abort(), 800);
    try {
      const r = await fetch('http://127.0.0.1:' + p + '/ext/ping', { signal: ctl.signal, cache: 'no-store' });
      const j = await r.json().catch(() => ({}));
      if (r.ok && j.app === APP_ID) { await api.storage.local.set({ port: p }); return 'http://127.0.0.1:' + p; }
    } catch (e) { } finally { clearTimeout(t); }
  }
  return null;
}

async function store() { return api.storage.local.get(['token', 'enabled', 'minMB', 'localOn', 'ignore', 'types', 'seen']); }

async function nm(path, opt, retried) {
  const s = await store();
  const b = await base(retried);
  if (!b) return { ok: false, status: 0, data: { error: '{{NAME}} is not running' } };
  const ctl = new AbortController();
  const t = setTimeout(() => ctl.abort(), (opt && opt.timeout) || 4000);
  try {
    const r = await fetch(b + path, {
      method: (opt && opt.body) ? 'POST' : 'GET', signal: ctl.signal,
      headers: Object.assign({ 'Content-Type': 'application/json' }, s.token ? { Authorization: 'Bearer ' + s.token } : {}),
      body: (opt && opt.body) ? JSON.stringify(opt.body) : undefined, cache: 'no-store'
    });
    let j = {};
    try { j = await r.json(); } catch (e) { }
    if (r.status === 401 && s.token && path !== '/ext/pair') await api.storage.local.remove('token'); // the program forgot this browser
    return { ok: r.ok, status: r.status, data: j };
  } catch (e) {
    clearTimeout(t);
    if (!retried) return nm(path, opt, true); // it may have moved to another port: look again once
    return { ok: false, status: 0, data: { error: '{{NAME}} is not running' } };
  } finally { clearTimeout(t); }
}

async function refresh() {
  const r = await nm('/ext/status', { timeout: 1500 });
  if (r.ok) await api.storage.local.set({ enabled: r.data.enabled !== false, minMB: r.data.minMB, seen: Date.now() });
  return r;
}

async function cookiesFor(url) {
  try { return NMCore.cookieHeader(await api.cookies.getAll({ url })); } catch (e) { return ''; }
}

async function send(url, referrer, filename) {
  return nm('/ext/add', { timeout: 12000, body: { url, referrer: referrer || '', cookie: await cookiesFor(url), userAgent: navigator.userAgent, filename: filename || '' } });
}

// A site can refuse the program (some protection services block everything but a real browser) while the browser
// itself is let in. For a link the person chose by hand (right-click menu, video button) the browser then downloads it
// itself, so the click is never silently lost. The link is remembered so the download it starts is not offered again.
const browserOwn = new Set();
function refusedByTheSite(r) { return !r.ok && r.status === 400 && /refused/i.test((r.data && r.data.error) || ''); }
async function browserDownload(url) {
  try { browserOwn.add(url); setTimeout(() => browserOwn.delete(url), 60000); await api.downloads.download({ url }); return true; } catch (e) { return false; }
}

function flash(text, color) {
  try {
    const a = api.action || api.browserAction;
    a.setBadgeBackgroundColor({ color: color || '#16a34a' });
    a.setBadgeText({ text });
    setTimeout(() => a.setBadgeText({ text: '' }), 3500);
  } catch (e) { }
}

api.downloads.onCreated.addListener(async (item) => {
  try {
    const s = await store();
    const cfg = { paired: !!s.token, enabled: s.enabled !== false, localOn: s.localOn !== false, minMB: s.minMB == null ? 5 : s.minMB, ignore: s.ignore || [], types: s.types || [] };
    const d = NMCore.shouldCatch(item, cfg);
    if (!d.catch || browserOwn.has(item.url)) return;
    try { await api.downloads.pause(item.id); } catch (e) { }
    const r = await send(item.url, item.referrer, NMCore.cleanName(item.filename));
    if (r.ok) {
      try { await api.downloads.cancel(item.id); } catch (e) { }
      try { await api.downloads.erase({ id: item.id }); } catch (e) { }
      flash(r.data.duplicate ? '=' : '+');
    } else {
      try { await api.downloads.resume(item.id); } catch (e) { } // the program could not take it: the browser keeps it
      if (r.status !== 0) flash('!', '#dc2626');
    }
  } catch (e) { }
});

function menus() {
  try {
    api.contextMenus.removeAll(() => {
      api.contextMenus.create({ id: 'nm', title: 'Download with {{NAME}}', contexts: ['link', 'video', 'audio', 'image'] });
    });
  } catch (e) { }
}
api.runtime.onInstalled.addListener(() => { menus(); videoScript(); });
if (api.runtime.onStartup) api.runtime.onStartup.addListener(() => { menus(); refresh(); videoScript(); });

// The button over videos is a content script, added only while the person allowed all sites and has not switched the
// button off (popup). New and reloaded pages get it; pages that were already open need a reload.
async function videoScript() {
  try {
    const s = await api.storage.local.get(['videoBtn']);
    const want = s.videoBtn !== false && await api.permissions.contains({ origins: ['<all_urls>'] });
    const have = (await api.scripting.getRegisteredContentScripts({ ids: ['pw-video'] })).length > 0;
    if (want && !have) await api.scripting.registerContentScripts([{ id: 'pw-video', js: ['video.js'], matches: ['http://*/*', 'https://*/*'], allFrames: true, runAt: 'document_idle', persistAcrossSessions: true }]);
    if (!want && have) await api.scripting.unregisterContentScripts({ ids: ['pw-video'] });
  } catch (e) { }
}
try { api.permissions.onAdded.addListener(videoScript); api.permissions.onRemoved.addListener(videoScript); } catch (e) { }
try { api.storage.onChanged.addListener((ch) => { if (ch.videoBtn) videoScript(); }); } catch (e) { }
videoScript();

api.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId !== 'nm') return;
  const url = info.linkUrl || info.srcUrl;
  if (!url || !/^(https?:|magnet:)/i.test(url)) return;
  if (/^https?:/i.test(url) && NMCore.isLocalHost(NMCore.hostOf(url))) return flash('!', '#dc2626');
  const r = await send(url, info.pageUrl || (tab && tab.url) || '', '');
  if (refusedByTheSite(r) && /^https?:/i.test(url) && await browserDownload(url)) return flash('B', '#d97706'); // the site only lets the browser in
  flash(r.ok ? (r.data.duplicate ? '=' : '+') : '!', r.ok ? '#16a34a' : '#dc2626');
});

// ---- videos and sounds the page loads, for the "Videos on this page" list in the popup ----
// Only works for sites the person allowed ("Allow for all sites"); nothing is stored on disk.
const media = new Map(); // tab id -> list
function headerOf(h, name) { const x = (h || []).find((y) => y.name.toLowerCase() === name); return x ? x.value : ''; }
try {
  api.webRequest.onResponseStarted.addListener(async (d) => {
    if (d.tabId < 0 || d.statusCode >= 300) return;
    const kind = NMCore.mediaKind(d.url, headerOf(d.responseHeaders, 'content-type'), parseInt(headerOf(d.responseHeaders, 'content-length') || '-1', 10));
    if (!kind) return;
    try { const tab = await api.tabs.get(d.tabId); if (!tab || tab.incognito) return; } catch (e) { return; } // nothing is recorded for private windows
    const list = media.get(d.tabId) || [];
    if (list.some((m) => m.url === d.url)) return;
    list.push({ url: d.url, kind, size: parseInt(headerOf(d.responseHeaders, 'content-range').split('/')[1] || headerOf(d.responseHeaders, 'content-length') || '-1', 10), time: Date.now() });
    while (list.length > 30) list.shift();
    media.set(d.tabId, list);
  }, { urls: ['<all_urls>'], types: ['media', 'xmlhttprequest', 'other'] }, ['responseHeaders']);
  api.tabs.onRemoved.addListener((id) => media.delete(id));
  api.tabs.onUpdated.addListener((id, ch) => { if (ch.status === 'loading' && ch.url) media.delete(id); });
} catch (e) { }

api.alarms.create('nm-refresh', { periodInMinutes: 1 });
api.alarms.onAlarm.addListener((a) => { if (a.name === 'nm-refresh') refresh(); });

api.runtime.onMessage.addListener((msg, sender, reply) => {
  (async () => {
    if (msg.type === 'ping') return reply(await nm('/ext/ping', { timeout: 1500 }));
    if (msg.type === 'refresh') return reply(await refresh());
    if (msg.type === 'media') return reply({ list: (media.get(msg.tab) || []).slice().reverse() });
    if (msg.type === 'grab') { // the button over a video was clicked
      const tabId = sender.tab && sender.tab.id;
      let url = /^https?:/i.test(msg.src || '') ? msg.src : ''; // a blob: link cannot be downloaded: the page's own video request is used instead
      if (!url) {
        const list = (media.get(tabId) || []).filter((m) => m.kind !== 'audio');
        const pick = list.slice().reverse().find((m) => m.kind === 'stream') || list.slice().sort((a, b) => b.size - a.size)[0];
        url = pick ? pick.url : '';
      }
      if (!url || NMCore.isLocalHost(NMCore.hostOf(url))) return reply({ ok: false, none: !url });
      const r = await send(url, (sender.tab && sender.tab.url) || msg.frame || '', '');
      if (refusedByTheSite(r) && await browserDownload(url)) { flash('B', '#d97706'); return reply({ ok: true, browser: true }); }
      flash(r.ok ? (r.data.duplicate ? '=' : '+') : '!', r.ok ? '#16a34a' : '#dc2626');
      return reply({ ok: r.ok, duplicate: !!(r.data && r.data.duplicate) });
    }
    if (msg.type === 'sendMedia') {
      if (!/^https?:/i.test(msg.url || '') || NMCore.isLocalHost(NMCore.hostOf(msg.url))) return reply({ ok: false });
      const r = await send(msg.url, msg.referrer || '', '');
      flash(r.ok ? (r.data.duplicate ? '=' : '+') : '!', r.ok ? '#16a34a' : '#dc2626');
      return reply({ ok: r.ok, duplicate: !!(r.data && r.data.duplicate) });
    }
    if (msg.type === 'pair') {
      const r = await nm('/ext/pair', { body: { code: msg.code, name: msg.name } });
      if (r.ok && r.data.token) await api.storage.local.set({ token: r.data.token });
      return reply({ ok: r.ok, error: r.data && r.data.error });
    }
    if (msg.type === 'unpair') { await nm('/ext/unpair', { body: {} }); await api.storage.local.remove('token'); return reply({ ok: true }); }
    reply({});
  })();
  return true;
});
