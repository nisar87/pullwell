// The main window: toolbar, categories, the download table, context menu, keyboard, status bar, and the dialogs
// for adding downloads and for options. The other dialogs (add with options, properties, queues, folders, link
// grabber, save/load list) are in downloads-more.js. APP (name, header, version, host) is set by index.html.
'use strict';

/* ---------- small helpers (also used by downloads-more.js) ---------- */
const $ = s => document.querySelector(s), $$ = s => [...document.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const bytes = b => !(b > 0) ? '0 KB' : b >= 1e12 ? (b/1e12).toFixed(2)+' TB' : b >= 1e9 ? (b/1e9).toFixed(2)+' GB' : b >= 1e6 ? (b/1e6).toFixed(1)+' MB' : Math.max(1, Math.round(b/1e3))+' KB';
const dur = s => { if (!(s > 0) || !isFinite(s)) return ''; s = Math.ceil(s); const d = Math.floor(s/86400), h = Math.floor(s%86400/3600), m = Math.floor(s%3600/60); return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : m ? `${m}m ${s%60}s` : `${s}s`; };
const setHTML = (el, html) => { if (el && el.dataset.h !== html) { el.dataset.h = html; el.innerHTML = html; } };
const labelOf = name => name;
let toastT;
function toast(msg, bad){ const t = $('#toast'); t.textContent = msg; t.className = 'toast show' + (bad ? ' bad' : ''); clearTimeout(toastT); toastT = setTimeout(() => t.className = 'toast', 2800); }
async function api(path, body){
  const r = await fetch('/api/' + path, body === undefined ? {} : {method:'POST', headers:{'Content-Type':'application/json', [APP.header]:'1'}, body: JSON.stringify(body)});
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || ('HTTP ' + r.status));
  return j;
}
const store = {
  get(k, d){ try { const v = localStorage.getItem('pw.' + k); return v === null ? d : JSON.parse(v); } catch(e){ return d; } },
  set(k, v){ try { localStorage.setItem('pw.' + k, JSON.stringify(v)); } catch(e){} },
};

/* ---------- state ---------- */
let DL = null;                       // the last list from the program (items, config, speed ...)
let view = store.get('view', {k:'all'}); // which group on the left is shown
let sortBy = store.get('sort', {c:'order', d:1});
let q = '';                          // search text
const sel = new Set();               // selected ids
let cur = null, anchor = null;       // the row with the keyboard focus, and where a shift-selection starts
let visible = [];                    // ids shown, in order
const prevState = {};
const speedHist = [], itemHist = new Map();
const DL_CATS = ['Programs','Archives','Videos','Music','Documents','Images','Torrents','Other'];
const DL_RISKY = /\.(exe|msi|msix|msixbundle|appx|bat|cmd|com|ps1|vbs|vbe|js|jse|wsf|jar|scr|pif|cpl|lnk|reg|hta|application)$/i;
const LIMITS = [0, 0.25, 0.5, 1, 2, 5, 10, 20, 50];

/* ---------- icons ---------- */
const I = {
  add:'<path d="M12 5v14M5 12h14"/>',
  resume:'<path d="M8 5l11 7-11 7z"/>',
  stop:'<rect x="6" y="6" width="12" height="12" rx="1.5"/>',
  stopall:'<rect x="3" y="7" width="10" height="10" rx="1.5"/><path d="M16 7v10M20 7v10"/>',
  startall:'<path d="M4 6l8 6-8 6zM13 6l8 6-8 6z"/>',
  del:'<path d="M4 7h16M10 11v6M14 11v6M6 7l1 12h10l1-12M9 7V4h6v3"/>',
  delfin:'<path d="M4 7h16M6 7l1 12h10l1-12M9 7V4h6v3"/><path d="M9.5 13.5l2 2 3.5-4"/>',
  queue:'<rect x="4" y="4" width="16" height="16" rx="2"/><path d="M8 9h8M8 13h8M8 17h5"/>',
  grab:'<circle cx="11" cy="11" r="6"/><path d="M20 20l-4.5-4.5M8.5 11h5M11 8.5v5"/>',
  opts:'<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>',
  all:'<path d="M4 6h16M4 12h16M4 18h16"/>',
  folder:'<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
  down:'<path d="M12 4v11m0 0l-4-4m4 4l4-4M5 20h14"/>',
  done:'<path d="M5 12.5l4.5 4.5L19 7.5"/>',
  warn:'<path d="M12 4l9 16H3z"/><path d="M12 10v4M12 17.5v.01"/>',
  flow:'<path d="M4 12h3l2-5 3 10 2-5h6"/>',
  todo:'<circle cx="12" cy="12" r="8"/><path d="M12 8v4l2.5 2"/>',
};
const svg = (k, cls) => `<svg viewBox="0 0 24 24" aria-hidden="true"${cls ? ` class="${cls}"` : ''}>${I[k]}</svg>`;

/* ---------- loading ---------- */
let loading = false, loadErrors = 0;
async function loadDL(){
  if (loading) return;
  loading = true;
  try { const d = await api('downloads'); d.items = d.items || []; d.config = d.config || {}; d.config.queues = d.config.queues || []; d.config.window = d.config.window || {from:'22:00', to:'07:00'}; DL = d; loadErrors = 0; if ($('#banner').dataset.down) { delete $('#banner').dataset.down; setHTML($('#banner'), ''); } }
  catch(e){ loadErrors++; if (UPD && UPD.status === 'installing' && loadErrors < 60) return; if (loadErrors === 3) { $('#banner').dataset.down = '1'; } if (loadErrors >= 3) setHTML($('#banner'), `<div class="bn bad">${esc(APP.name)} is not answering. If you closed it from the notification area, start it again.</div>`); return; }
  finally { loading = false; }
  for (const v of DL.items) { if (prevState[v.id] && prevState[v.id] !== 'done' && v.state === 'done' && !document.hidden) toast('Finished: ' + v.name); prevState[v.id] = v.state; }
  speedHist.push(DL.speed || 0); if (speedHist.length > 60) speedHist.shift();
  // the speed of each running download over the last two minutes, for its progress window; bounded per item and
  // dropped as soon as the download is no longer running
  for (const v of DL.items) if (v.state === 'downloading') { const h = itemHist.get(v.id) || []; h.push(v.speed || 0); if (h.length > 120) h.shift(); itemHist.set(v.id, h); }
  for (const id of itemHist.keys()) if (!DL.items.some(v => v.id === id && v.state === 'downloading')) itemHist.delete(id);
  render();
  if (++addPolls % 2 === 0 || document.hasFocus()) checkPendingAdd();
}
let addPolls = 0, addChecking = false;
// The headers (cookies, the page it came from) of a download the browser handed over: the box probes and adds with them.
let askHdr = null;
async function openAsk(a){
  askHdr = a.header || null;
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Checking the link" style="max-width:600px"><h3>Checking the link…</h3><div class="hint2 selectable" style="margin-top:6px;word-break:break-all">${esc(a.url)}</div></div>`);
  let p; try { p = await api('downloads', {action:'probe', url: a.url, header: askHdr}); } catch(e){ p = {ok: false, error: e.message}; }
  if (!$('#dlDlg')) return;
  p.info = Object.assign({}, p.info || {}); if (a.name) p.info.name = a.name;
  showProbe(a.url, p);
}
// A link chosen from the "Download this?" notification waits in the program until the window asks for it.
async function checkPendingAdd(){
  if (addChecking || $('#dlDlg')) return;
  addChecking = true;
  try {
    const k = DL.ext && DL.ext.browsers && DL.ext.browsers.length ? await api('downloads', {action: 'takeAsk'}) : null; // a download the browser handed over, waiting for a yes
    if (k && k.ask) { openAsk(k.ask); return; }
    const r = await api('app', {take: 'add'}); if (r && r.add) addDialog(r.add.trim());
    else if (r && r.view === 'complete') { // "show a finished download": its window, with Open and Open folder
      const top = DL.items.filter(v => v.state === 'done').sort((a, b) => (b.finished || 0) - (a.finished || 0))[0];
      if (top) { sel.clear(); sel.add(top.id); cur = top.id; render(); progressBox(top.id); }
    }
    else if (r && r.view) { // a notification was clicked: show what it was about, newest first
      view = {k: r.view}; store.set('view', view); q = ''; if ($('#search')) $('#search').value = '';
      sel.clear(); const top = DL.items.filter(v => inView(v, view)).sort((a, b) => (b.finished || b.added || 0) - (a.finished || a.added || 0))[0];
      if (top) { sel.add(top.id); cur = top.id; } render();
    } } catch(e){} finally { addChecking = false; }
}
async function dlAct(body, okMsg){
  try { const r = await api('downloads', body); await loadDL(); if (okMsg) toast(okMsg); afterChange(); return r || {}; }
  catch(e){ toast(e.message || 'That did not work', true); return null; }
}
const paintDL = () => render(); // downloads-more.js calls this name

/* ---------- what each row says ---------- */
// what the small coloured icon says: the file type (MP4, ZIP ...), or a short word for torrents and streams
const extOf = v => { if (v.kind === 'torrent') return 'TOR'; const m = /\.([a-z0-9]{1,4})$/i.exec(v.name || ''); return m ? m[1].toUpperCase() : (v.category || 'Other').slice(0, 3).toUpperCase(); };
const pctOf = v => v.state === 'done' ? 100 : v.size > 0 ? Math.min(100, v.done / v.size * 100) : 0;
const dlStateText = v => v.state === 'downloading' ? 'Downloading' : v.state === 'queued' ? (v.retryAt > Date.now()/1000 ? 'Trying again soon' : 'Waiting') : v.state === 'paused' ? 'Paused' : v.state === 'done' ? 'Complete' : 'Error';
function dlLineShare(v){ const tot = {}; Object.entries(v.bytes || {}).forEach(([k,b]) => tot[k] = (tot[k]||0) + b); const sum = Object.values(tot).reduce((a,b) => a+b, 0); return {tot, sum}; }
const leftOf = v => v.state === 'downloading' && v.size > 0 && v.speed > 0 ? (v.size - v.done) / v.speed : 0;
function infoOf(v){
  if (v.state === 'error') return `<span class="bad">${esc(v.err || 'Something went wrong')}</span>`;
  if (v.verify === 'bad') return '<span class="bad">Checksum does NOT match: do not open</span>';
  const out = [];
  if (v.state === 'done' && v.verify === 'ok') out.push('<span class="good">Checksum OK</span>');
  if (v.state === 'done' && v.verify === 'checking') out.push('Checking checksum…');
  if (v.kind === 'torrent' && v.state === 'downloading') out.push(`${v.peers||0} peers${v.seeds ? `, ${v.seeds} seeds` : ''}`);
  if (v.note && v.state !== 'done') out.push(esc(v.note));
  if (v.err && v.state !== 'done') out.push(esc(v.err));
  if (v.queue) out.push('Queue: ' + esc(dlQueueName(v.queue)));
  if (v.limit > 0 && v.state !== 'done') out.push('Limit ' + bytes(v.limit) + '/s');
  return out.join(' · ');
}
// flowParts folds the engine's part map (each entry 0..100, equal shares of the file) into at most n cells, so a
// download in 64 parts still reads as a line of parts and not as a hatch.
function flowParts(m, n){
  if (m.length <= n) return m;
  const out = [];
  for (let i = 0; i < n; i++) { const a = Math.floor(i * m.length / n), b = Math.floor((i + 1) * m.length / n); let t = 0; for (let j = a; j < b; j++) t += +m[j] || 0; out.push(t / (b - a)); }
  return out;
}
// The flow line: a thin track per download. A running download that reports its parts shows them along the line
// (the engine's real map, one cell per part); the others show one fill. The word next to it says the state.
function statusCell(v){
  const p = pctOf(v), queued = v.state === 'queued';
  const cls = v.state === 'done' ? 'done' : v.state === 'paused' ? 'paused' : v.state === 'error' ? 'err' : queued ? 'wait' : '';
  let txt, tcls = '';
  if (v.state === 'done') { txt = 'Done'; tcls = 'done'; }
  else if (v.state === 'error') { txt = 'Failed'; tcls = 'err'; }
  else if (queued && !(v.done > 0)) { txt = v.retryAt > Date.now()/1000 ? 'Retry soon' : 'Waiting'; tcls = 'wait'; }
  else if (v.state === 'paused') txt = 'Paused' + (v.size > 0 ? ' ' + Math.floor(p) + '%' : '');
  else { txt = v.size > 0 ? p.toFixed(p < 10 ? 1 : 0) + '%' : bytes(v.done); tcls = v.state === 'downloading' ? 'run' : 'wait'; }
  const map = v.state === 'downloading' && Array.isArray(v.map) && v.map.length > 1 ? flowParts(v.map, 16) : null;
  const line = map ? `<div class="pb map">${map.map(m => `<b><i style="width:${Math.max(0, Math.min(100, m))}%"></i></b>`).join('')}</div>`
    : `<div class="pb ${cls}"><i style="width:${v.state === 'error' && !(p > 0) ? 100 : p}%"></i></div>`;
  return `<div class="st">${line}<span class="pt ${tcls}">${txt}</span></div>`;
}
const when = t => { if (!t) return ''; const d = new Date(t*1000), now = new Date(); return d.toDateString() === now.toDateString() ? d.toLocaleTimeString(I18N.locale, {hour:'2-digit', minute:'2-digit'}) : d.toLocaleDateString(I18N.locale, {day:'numeric', month:'short'}) + ' ' + d.toLocaleTimeString(I18N.locale, {hour:'2-digit', minute:'2-digit'}); };

/* ---------- columns ---------- */
const COLS = [
  {k:'name', t:'File name', w:0, sort: v => (v.name || v.url).toLowerCase(), cell: v => `<td class="nm" title="${esc((v.name || '') + '\n' + v.url)}"><span class="fi ${esc(v.category || 'Other')}">${esc(extOf(v))}</span><span class="tx">${esc(v.name || v.url)}</span></td>`},
  {k:'size', t:'Size', w:86, r:1, sort: v => v.size || 0, cell: v => `<td class="r">${v.size > 0 ? bytes(v.size) : v.state === 'done' ? bytes(v.done) : '<span class="mute">?</span>'}</td>`},
  {k:'status', t:'Progress', w:190, sort: v => ({downloading:0, queued:1, paused:2, error:3, done:4})[v.state] * 1000 - pctOf(v), cell: v => `<td>${statusCell(v)}</td>`},
  {k:'speed', t:'Speed', w:84, r:1, sort: v => v.state === 'downloading' ? v.speed || 0 : -1, cell: v => `<td class="r sp">${v.state === 'downloading' ? bytes(v.speed) + '/s' : ''}</td>`},
  {k:'left', t:'Time left', w:78, r:1, sort: v => leftOf(v) || 1e12, cell: v => `<td class="r">${dur(leftOf(v))}</td>`},
  {k:'added', t:'Added', w:120, sort: v => v.added || 0, cell: v => `<td class="n" title="${v.finished ? 'Finished ' + esc(new Date(v.finished*1000).toLocaleString(I18N.locale)) : ''}">${when(v.added)}</td>`},
  {k:'site', t:'Website', w:140, sort: v => siteOf(v), cell: v => `<td title="${esc(v.url)}">${esc(siteOf(v))}</td>`},
  {k:'info', t:'Description', w:200, sort: v => (v.err || v.note || ''), cell: v => `<td class="info" title="${esc(v.detail || '')}">${infoOf(v)}</td>`},
];
let widths = store.get('widths', {});
let hideCols = store.get('hideCols', ['site']);  // Website is off until switched on           // columns switched off in View → Columns (never the name)
const shownCols = () => COLS.filter(c => c.k === 'name' || !hideCols.includes(c.k));

/* ---------- which rows are shown ---------- */
function inView(v, vw){
  switch (vw.k) {
    case 'cat': return (v.category || 'Other') === vw.v;
    case 'unfinished': return v.state !== 'done';
    case 'active': return v.state === 'downloading';
    case 'finished': return v.state === 'done';
    case 'errors': return v.state === 'error';
    case 'queue': return (v.queue || '') === vw.v && v.state !== 'done';
  }
  return true;
}
function shownItems(){
  const s = q.trim().toLowerCase();
  const list = DL.items.filter(v => inView(v, view) && (!s || (v.name || '').toLowerCase().includes(s) || v.url.toLowerCase().includes(s)));
  const col = COLS.find(c => c.k === sortBy.c);
  const key = col ? col.sort : (v => v.order);
  list.sort((a, b) => { const x = key(a), y = key(b); return (x < y ? -1 : x > y ? 1 : (a.order - b.order)) * sortBy.d; });
  return list;
}

/* ---------- drawing ---------- */
function render(){
  if (!DL) return;
  for (const id of [...sel]) if (!DL.items.some(v => v.id === id)) sel.delete(id);
  if (cur && !DL.items.some(v => v.id === cur)) cur = null;
  renderSide(); renderTable(); renderTools(); renderStatus(); renderBanner(); paintProgress();
}

function renderSide(){
  const it = DL.items, n = f => it.filter(f).length;
  const row = (vw, icon, label, count, sub) => { const on = view.k === vw.k && (view.v || '') === (vw.v || ''); return `<button class="it${sub ? ' sub' : ''}${on ? ' sel' : ''}" data-view='${esc(JSON.stringify(vw))}'>${svg(icon)}<span>${esc(label)}</span><small>${count || ''}</small></button>`; };
  const cats = DL_CATS.map(c => row({k:'cat', v:c}, 'folder', c, n(v => (v.category || 'Other') === c), true)).join('');
  const queues = [{id:'', name:'Main queue'}].concat(DL.config.queues || []).map(qq => row({k:'queue', v:qq.id}, 'queue', qq.name + (qq.paused ? ' (paused)' : ''), n(v => (v.queue || '') === qq.id && v.state !== 'done'), true)).join('');
  const active = n(v => v.state === 'downloading');
  setHTML($('#side'), `<div class="h">Library</div>${row({k:'all'}, 'all', 'All downloads', it.length)}${row({k:'active'}, 'flow', 'Downloading', active).replace('<small>', active ? '<i class="live" aria-hidden="true"></i><small>' : '<small>')}
    ${row({k:'unfinished'}, 'todo', 'Unfinished', n(v => v.state !== 'done'))}${row({k:'finished'}, 'done', 'Finished', n(v => v.state === 'done'))}${row({k:'errors'}, 'warn', 'Needs attention', n(v => v.state === 'error'))}
    <div class="h">File types</div>${cats}
    <div class="h">Queues</div>${queues}`);
}

let headKey = '';
const rowEls = new Map(); // id -> <tr>, kept between refreshes so clicks and double-clicks land on the same element
function renderTable(){
  const items = shownItems();
  visible = items.map(v => v.id);
  const SC = shownCols();
  const hk = JSON.stringify([sortBy, widths, hideCols]);
  if (hk !== headKey) {
    headKey = hk;
    $('#cg').innerHTML = SC.map(c => `<col style="width:${c.w ? (widths[c.k] || c.w) + 'px' : 'auto'}">`).join('');
    $('#tbl').style.minWidth = SC.reduce((a, c) => a + (c.w ? (widths[c.k] || c.w) : 170), 0) + 'px'; // the name keeps at least 170 px; narrower windows scroll sideways
    $('#th').innerHTML = `<tr>${SC.map(c => `<th data-sort="${c.k}"${c.r ? ' class="r"' : ''}>${c.t}${sortBy.c === c.k ? `<span class="ar">${sortBy.d > 0 ? '▲' : '▼'}</span>` : ''}</th>`).join('')}</tr>`;
  }
  const tb = $('#tb'), keep = new Set(visible);
  for (const [id, tr] of rowEls) if (!keep.has(id)) { tr.remove(); rowEls.delete(id); }
  let prev = null;
  for (const v of items) {
    let tr = rowEls.get(v.id);
    if (!tr) { tr = document.createElement('tr'); tr.dataset.id = v.id; rowEls.set(v.id, tr); }
    const html = SC.map(c => c.cell(v)).join('');
    if (tr.dataset.h !== html) { tr.dataset.h = html; tr.innerHTML = html; }
    const cls = (sel.has(v.id) ? 'sel' : '') + (cur === v.id ? ' cur' : '') + (v.state === 'downloading' ? ' run' : '');
    if (tr.className !== cls) tr.className = cls;
    const want = prev ? prev.nextSibling : tb.firstChild;
    if (want !== tr) tb.insertBefore(tr, want);
    prev = tr;
  }
  const empty = $('#empty');
  if (items.length) empty.hidden = true;
  else {
    empty.hidden = false;
    setHTML(empty, DL.items.length ? (q ? 'Nothing matches your search.' : 'Nothing here.')
      : `<b>No downloads yet</b>Paste a link with <b>Ctrl+V</b>, drop a link or a .torrent file here, or choose <button class="btn pri" id="emptyAdd">New download</button>`
        + (DL.ext && (DL.ext.browsers || []).length ? '' : `<br><br>To let big downloads from Chrome, Edge or Firefox come here by themselves: <button class="btn" id="emptyExt">Connect a browser…</button>`));
  }
}

function selectedItems(){ return DL ? DL.items.filter(v => sel.has(v.id)) : []; }
function renderTools(){
  const s = selectedItems(), any = f => s.some(f);
  const en = {
    resume: any(v => v.state === 'paused' || v.state === 'error'),
    stop: any(v => v.state === 'downloading' || v.state === 'queued'),
    stopall: DL.items.some(v => v.state === 'downloading' || v.state === 'queued'),
    startall: DL.items.some(v => v.state === 'paused' || v.state === 'error'),
    del: s.length > 0,
    delfin: DL.items.some(v => v.state === 'done'),
  };
  for (const [k, on] of Object.entries(en)) { const b = $(`[data-tb="${k}"]`); if (b && b.disabled === on) b.disabled = !on; }
}

function spark(){
  if (speedHist.length < 3) return '';
  const mx = Math.max(...speedHist, 1), w = 70, h = 14, n = speedHist.length;
  const pts = speedHist.map((v, i) => `${(i/(n-1)*w).toFixed(1)},${(h-1-v/mx*(h-2)).toFixed(1)}`).join(' ');
  return `<svg class="spark" width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" aria-hidden="true"><polyline points="${pts}" fill="none" stroke="var(--good)" stroke-width="1.4"/></svg>`;
}
function renderStatus(){
  const it = DL.items, act = it.filter(v => v.state === 'downloading').length, que = it.filter(v => v.state === 'queued').length;
  const s = selectedItems();
  const selTxt = s.length > 1 ? `<span><b>${s.length}</b> selected · ${bytes(s.reduce((a, v) => a + (v.size > 0 ? v.size : v.done), 0))}</span>` : '';
  const limit = DL.config.limitAll > 0 ? ` · limit ${bytes(DL.config.limitAll)}/s` : '';
  setHTML($('#status'), `<span><b>${it.length}</b> download${it.length === 1 ? '' : 's'}</span><span><b>${act}</b> downloading${que ? `, ${que} waiting` : ''}</span>
    <span class="stlim" id="stLimit" role="button" tabindex="0" title="Click to set a speed limit for all downloads">↓ <b>${bytes(DL.speed || 0)}/s</b>${limit} ${spark()}</span>${selTxt}<span class="sp"></span><span class="selectable" title="Change in Options">Saving to ${esc(DL.config.dir)}</span>`);
}
let UPD = null, updAt = 0;
async function loadUpd(){ try { UPD = await api('update'); if (UPD.current !== APP.version || (UPD.build || '') !== APP.build) location.reload(); } catch(e){} } // a new version is running: load its page
function renderBanner(){
  if (Date.now() - updAt > 10000 || (UPD && UPD.status !== 'idle' && Date.now() - updAt > 1500)) { updAt = Date.now(); loadUpd().then(() => { if (optTab === 'updates' && $('.opt')) showOptions('updates'); }); }
  let h = '';
  if (UPD && UPD.status === 'installing') h += `<div class="bn warn"><b>Installing version ${esc(UPD.latest)}.</b> ${esc(APP.name)} restarts in a moment; downloads continue afterwards.</div>`;
  else if (UPD && UPD.available && UPD.status === 'idle' && !UPD.auto) h += `<div class="bn warn"><b>Version ${esc(UPD.latest)} is ready.</b><span style="flex:1"></span><button class="btn" data-act="updInstall">Install and restart</button></div>`;
  if (DL.powerAt && (Date.now()/1000 - DL.powerAt) < 70) h += `<div class="bn bad"><b>The PC shuts down in less than a minute</b> because your downloads finished.<span style="flex:1"></span><button class="btn" data-act="cancelPower">Cancel shutdown</button></div>`;
  if (DL.hold) h += `<div class="bn warn"><b>Downloads are waiting:</b> ${esc(DL.hold.replace(/^Waiting: /, ''))}</div>`;
  setHTML($('#banner'), h);
}

/* ---------- selection ---------- */
function pick(id, how){
  if (how === 'toggle') { sel.has(id) ? sel.delete(id) : sel.add(id); anchor = id; }
  else if (how === 'range' && anchor && visible.includes(anchor)) {
    const a = visible.indexOf(anchor), b = visible.indexOf(id); sel.clear();
    for (let i = Math.min(a, b); i <= Math.max(a, b); i++) sel.add(visible[i]);
  } else { sel.clear(); sel.add(id); anchor = id; }
  cur = id; render();
}
function moveCur(delta, extend){
  if (!visible.length) return;
  let i = cur ? visible.indexOf(cur) : -1;
  i = delta === -Infinity ? 0 : delta === Infinity ? visible.length - 1 : Math.max(0, Math.min(visible.length - 1, i + delta));
  pick(visible[i], extend ? 'range' : 'one');
  const tr = $(`tr[data-id="${CSS.escape(visible[i])}"]`); if (tr) tr.scrollIntoView({block:'nearest'});
}

/* ---------- actions on the selection ---------- */
async function eachSel(action, extra, okWord){
  const ids = [...sel]; if (!ids.length) return;
  let bad = 0;
  for (const id of ids) { try { await api('downloads', {action, id, ...(extra || {})}); } catch(e){ bad++; if (ids.length === 1) toast(e.message, true); } }
  await loadDL();
  if (ids.length > 1 || okWord) toast(bad ? `${ids.length - bad} ${okWord || 'done'}, ${bad} could not be changed` : `${ids.length > 1 ? ids.length + ' ' : ''}${okWord || 'Done'}`, !!bad);
}
async function doResume(){
  const s = selectedItems();
  for (const v of s) { if (v.state === 'error') await api('downloads', {action:'retry', id:v.id}).catch(e => toast(e.message, true)); else if (v.state === 'paused') await api('downloads', {action:'resume', id:v.id}).catch(e => toast(e.message, true)); }
  loadDL();
}
async function doStop(){
  const s = selectedItems().filter(v => v.state === 'downloading' || v.state === 'queued');
  if (!(await confirmNoResume(s))) return;
  for (const v of s) await api('downloads', {action:'pause', id:v.id}).catch(e => toast(e.message, true));
  loadDL();
}
// Stopping a download from a site that does not allow resuming throws away what is downloaded: ask first.
async function confirmNoResume(list){
  const lose = list.filter(v => v.state === 'downloading' && v.noResume && v.done > 0);
  if (!lose.length) return true;
  const r = await askBox({title: lose.length === 1 ? `Pause "${lose[0].name}"?` : `Pause ${lose.length} downloads?`, text: lose.length === 1 ? `This website does not allow resuming: when it starts again, it starts from the beginning (${bytes(lose.reduce((a, v) => a + v.done, 0))} downloaded so far).` : `These websites do not allow resuming: when they start again, they start from the beginning (${bytes(lose.reduce((a, v) => a + v.done, 0))} downloaded so far).`, ok: 'Pause anyway', danger: true});
  return r.ok;
}
async function doDelete(withFiles){
  const s = selectedItems(); if (!s.length) return;
  const unfinished = s.filter(v => v.state !== 'done' && v.state !== 'error' && v.done > 0).length;
  const fin = s.filter(v => v.state === 'done').length;
  const what = s.length === 1 ? `"${s[0].name || s[0].url}"` : `${s.length} downloads`;
  const r = await askBox({title: 'Delete ' + what + '?', text: (unfinished ? `${unfinished === 1 && s.length === 1 ? 'It is' : unfinished + ' of them are'} not finished: the part downloaded so far is deleted. ` : '') + 'The download is removed from the list.',
    check: fin ? (fin === 1 && s.length === 1 ? 'Also delete the file from the disk' : `Also delete the ${fin} finished file${fin === 1 ? '' : 's'} from the disk`) : '', checked: !!withFiles, ok:'Delete', danger:true});
  if (!r.ok) return;
  await eachSel('remove', {deleteFiles: !!r.checked}, r.checked ? 'Deleted with files' : 'Removed from list');
  sel.clear(); render();
}
async function openItem(v, folder){
  if (!v) return;
  if (!folder && v.state !== 'done') return progressBox(v.id);
  if (!folder && DL_RISKY.test(v.name || '')) { const r = await askBox({title:'Open a program?', text:`"${v.name}" is a program or script. Only open it if you trust the website it came from.`, ok:'Open', danger:true}); if (!r.ok) return; }
  dlAct({action: folder ? 'folder' : 'open', id: v.id});
}

/* ---------- context menu ---------- */
let menuEl = null;
function closeMenu(){
  if (menuEl) { menuEl.remove(); menuEl = null; }
  $$('#menubar button.open').forEach(b => b.classList.remove('open'));
}
function showMenu(x, y, entries, drop){
  closeMenu();
  const m = document.createElement('div'); m.className = 'ctx' + (drop ? ' drop' : ''); m.setAttribute('role', 'menu');
  const syncOpen = () => m.querySelectorAll('button.sub.open').forEach(o => { if (![...m.querySelectorAll('.ctx.subm')].some(s => s.opener === o)) o.classList.remove('open'); });
  const build = (box, list) => list.forEach(e => {
    // the highlight is the focus, so mouse and keyboard share one row: hovering moves it (a disabled row keeps the previous one)
    const hot = b => { if (!b.disabled) b.focus({preventScroll: true}); };
    if (e === '-') { box.appendChild(document.createElement('hr')); return; }
    const b = document.createElement('button'); b.setAttribute('role', 'menuitem');
    b.innerHTML = esc(e.t) + (e.k ? `<kbd>${esc(e.k)}</kbd>` : '');
    if (e.chk) b.classList.add('chk');
    if (e.off) b.disabled = true;
    if (e.sub) {
      b.classList.add('sub');
      const open = () => {
        m.querySelectorAll('.ctx.subm').forEach(s => s.remove()); m.querySelectorAll('button.open').forEach(o => o.classList.remove('open'));
        b.classList.add('open');
        const s = document.createElement('div'); s.className = 'ctx subm'; s.setAttribute('role', 'menu'); s.opener = b; build(s, e.sub); m.appendChild(s);
        const r = b.getBoundingClientRect(), Z = zoomNow(); const sw = s.offsetWidth * Z, sh = s.offsetHeight * Z; // screen pixels; styles are in zoomed pixels
        s.style.left = (r.right + sw > innerWidth ? r.left - sw : r.right) / Z + 'px'; s.style.top = Math.max(4, Math.min(r.top - 4, innerHeight - sh - 4)) / Z + 'px';
      };
      b.onmouseenter = () => { open(); hot(b); }; b.onclick = ev => { ev.stopPropagation(); open(); const f = m.querySelector('.ctx.subm button:not(:disabled)'); if (f) f.focus(); };
      b.openSub = () => b.onclick({stopPropagation(){}});
    } else {
      b.onmouseenter = () => { m.querySelectorAll('.ctx.subm').forEach(s => { if (!s.contains(b)) s.remove(); }); syncOpen(); hot(b); };
      b.onclick = ev => { ev.stopPropagation(); closeMenu(); e.do(); };
    }
    box.appendChild(b);
  });
  build(m, entries.filter((e, i, a) => !(e === '-' && (i === 0 || a[i - 1] === '-' || i === a.length - 1))));
  document.body.appendChild(m); menuEl = m;
  const Z = zoomNow(), w = m.offsetWidth * Z, h = m.offsetHeight * Z;
  m.style.left = Math.max(4, Math.min(x, innerWidth - w - 4)) / Z + 'px'; m.style.top = Math.max(4, Math.min(y, innerHeight - h - 4)) / Z + 'px';
  const first = m.querySelector('button:not(:disabled)'); if (first) first.focus();
}
function rowMenu(x, y){
  const s = selectedItems(); if (!s.length) return listMenu(x, y);
  const one = s.length === 1 ? s[0] : null, any = f => s.some(f);
  const movable = any(v => v.state === 'queued' || v.state === 'paused');
  const qs = [{id:'', name:'Main queue'}].concat(DL.config.queues || []);
  const curQ = one ? (one.queue || '') : null, curLim = one ? one.limit || 0 : -1;
  showMenu(x, y, [
    {t:'Open', k:'Enter', off: !(one && one.state === 'done'), do: () => openItem(one)},
    {t:'Open folder', off: !(one && one.path), do: () => openItem(one, true)},
    '-',
    {t:'Resume', off: !any(v => v.state === 'paused' || v.state === 'error'), do: doResume},
    {t:'Pause', off: !any(v => v.state === 'downloading' || v.state === 'queued'), do: doStop},
    '-',
    {t:'Move to top', off: !movable, do: () => eachSel('move', {move:'top'})},
    {t:'Move up', off: !movable, do: () => eachSel('move', {move:'up'})},
    {t:'Move down', off: !movable, do: () => eachSel('move', {move:'down'})},
    {t:'Queue', off: !any(v => v.state !== 'done'), sub: qs.map(qq => ({t: qq.name, chk: curQ === qq.id, do: () => eachSel('setQueue', {queue: qq.id}, 'Moved')}))},
    {t:'Speed limit', off: !any(v => v.state !== 'done'), sub: LIMITS.map(l => ({t: limitLabel(l), chk: Math.abs(curLim - l * 1048576) < 1, do: () => eachSel('limit', {limitBytes: l * 1048576}, l ? 'Limit set' : 'Limit removed')}))},
    '-',
    {t:'Copy link', do: () => copyText(s.map(v => v.url).join('\n'), s.length > 1 ? 'Links copied' : 'Link copied')},
    {t:'Progress', off: !(one && one.state !== 'done'), do: () => progressBox(one.id)},
    {t:'Open when finished', chk: !!(one && one.openDone), off: !(one && one.state !== 'done' && one.kind !== 'torrent'), do: () => dlAct({action:'openWhenDone', id: one.id, openWhenDone: !one.openDone}, one.openDone ? 'Will not open by itself' : 'Opens when finished')},
    {t:'Rename…', k:'F2', off: !canRename(one), do: () => renameBox(one)},
    {t:'Move to another folder…', off: !(one && one.state === 'done' && one.path && one.kind !== 'torrent'), do: () => moveBox(one)},
    {t:'Check the checksum…', off: !(one && one.state === 'done' && one.path), do: () => checksumBox(one)},
    {t:'Properties', k:'Alt+Enter', off: !one, do: () => dlProps(one.id)},
    '-',
    {t:'Delete', k:'Del', do: () => doDelete(false)},
    {t:'Delete with file', k:'Shift+Del', off: !any(v => v.state === 'done'), do: () => doDelete(true)},
  ]);
}
function listMenu(x, y){
  showMenu(x, y, [
    {t:'New download…', k:'Ctrl+N', do: () => addDialog('')},
    {t:'Add a torrent file…', do: pickTorrent},
    '-',
    {t:'Select all', k:'Ctrl+A', off: !visible.length, do: () => { visible.forEach(id => sel.add(id)); render(); }},
    {t:'Resume all', off: !DL.items.some(v => v.state === 'paused' || v.state === 'error'), do: () => dlAct({action:'resumeAll'})},
    {t:'Pause all', off: !DL.items.some(v => v.state === 'downloading' || v.state === 'queued'), do: () => command('stopall')},
  ]);
}
/* ---------- rename ---------- */
// A finished file is renamed on disk; one that has not started gets the name it will be saved under.
const canRename = v => !!v && v.kind !== 'torrent' && (v.state === 'done' ? !!v.path : !v.path);
function renameBox(v){
  if (!canRename(v)) return;
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Rename" style="max-width:520px"><h3>Rename</h3>
    <div class="hint2" style="margin:4px 0 8px">${v.state === 'done' ? 'The file is renamed in its folder.' : 'The download will be saved under this name.'}</div>
    <input class="inp" id="rnName" style="width:100%" spellcheck="false" value="${esc(v.name || '')}">
    <div class="row" style="justify-content:flex-end;gap:8px;margin-top:14px"><button class="btn" id="rnNo">Cancel</button><button class="btn pri" id="rnYes">Rename</button></div></div>`);
  const inp = $('#rnName'); inp.focus();
  const dot = (v.name || '').lastIndexOf('.'); inp.setSelectionRange(0, dot > 0 ? dot : inp.value.length); // the name, not the type
  $('#rnNo').onclick = dlClose;
  const go = async () => { const n = inp.value.trim(); if (!n || n === v.name) return dlClose(); const r = await dlAct({action:'rename', id: v.id, name: n}, 'Renamed'); if (r !== null) dlClose(); };
  $('#rnYes').onclick = go;
  inp.onkeydown = e => { if (e.key === 'Enter') { e.preventDefault(); go(); } };
}

// a small dialog with one text box (move, checksum)
function inputBox({title, text, value = '', placeholder = '', ok, run}){
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="${esc(title)}" style="max-width:520px"><h3>${esc(title)}</h3>
    <div class="hint2" style="margin:4px 0 8px">${esc(text)}</div>
    <input class="inp" id="ibVal" style="width:100%" spellcheck="false" value="${esc(value)}" placeholder="${esc(placeholder)}">
    <div class="row" style="justify-content:flex-end;gap:8px;margin-top:14px"><button class="btn" id="ibNo">Cancel</button><button class="btn pri" id="ibYes">${esc(ok)}</button></div></div>`);
  const inp = $('#ibVal'); inp.focus(); inp.select();
  $('#ibNo').onclick = dlClose;
  const go = async () => { const v = inp.value.trim(); if (!v) return; const r = await run(v); if (r !== null) dlClose(); };
  $('#ibYes').onclick = go; inp.onkeydown = e => { if (e.key === 'Enter') { e.preventDefault(); go(); } };
}
const dirOf = p => p.replace(/[\\/][^\\/]*$/, '');
function moveBox(v){
  inputBox({title: 'Move to another folder', text: `"${v.name}" is moved into the folder you type (it is made if needed).`, value: dirOf(v.path), ok: 'Move',
    run: d => dlAct({action:'moveTo', id: v.id, dir: d}, 'Moved')});
}
function checksumBox(v){
  inputBox({title: 'Check the checksum', text: 'Paste the checksum the website gives for this file (SHA-256, SHA-1, MD5 or SHA-512). The result shows in the Description column.', placeholder: 'e.g. 3a7bd3e2360a3d…', ok: 'Check',
    run: h => dlAct({action:'checkFile', id: v.id, hash: h}, 'Checking…')});
}

/* ---------- live progress of one download ---------- */
// Opened by double-click or Enter on an unfinished download. It is redrawn after every refresh of the list, so it
// shows the speed, time left, connections and parts as they change; when the download finishes it offers Open.
let progId = null;
function progressBox(id){
  if (!DL.items.some(v => v.id === id)) return;
  progId = id;
  dlDialog(`<div class="card prog" role="dialog" aria-modal="true" aria-labelledby="pgTitle">
    <div class="ph"><span class="fi" id="pgType"></span><div style="min-width:0"><h3 id="pgTitle"></h3><div class="src selectable" id="pgUrl"></div></div></div>
    <div class="hero"><span class="big" id="pgBig"></span><span class="state" id="pgState"></span><span class="sp"></span><span class="rate" id="pgRate"></span></div>
    <div class="flow"><div class="pgbar"><i id="pgFill"></i></div><div id="pgSeg"></div><div id="pgSpark"></div></div>
    <div class="pggrid" id="pgGrid"></div>
    <div id="pgLog" class="pglog" aria-label="Recent events"></div>
    <div class="pfoot"><button class="btn" id="pgProps">Properties</button><span class="sp"></span><button class="btn" id="pgFolder">Open folder</button><button class="btn" id="pgMain"></button><button class="btn pri" id="pgClose">Close</button></div></div>`);
  $('#pgClose').onclick = dlClose;
  $('#pgProps').onclick = () => { const i = progId; dlClose(); dlProps(i); };
  $('#pgFolder').onclick = () => { const v = DL.items.find(x => x.id === progId); if (v) openItem(v, true); };
  $('#pgMain').onclick = async () => {
    const v = DL.items.find(x => x.id === progId); if (!v) return;
    if (Date.now() - (+$('#pgMain').dataset.changed || 0) < 900) return; // the button just changed under the pointer (e.g. Pause became Open)
    if (v.state === 'done') { dlClose(); return openItem(v); }
    if (v.state === 'downloading' || v.state === 'queued') { if (await confirmNoResume([v])) await dlAct({action:'pause', id: v.id}); if (!$('#pgGrid')) progressBox(v.id); }
    else await dlAct({action:'resume', id: v.id});
  };
  paintProgress();
  $('#pgClose').focus();
}
function paintProgress(){
  if (!progId || !$('#pgGrid')) { progId = $('#pgGrid') ? progId : null; return; }
  const v = DL.items.find(x => x.id === progId);
  if (!v) { $('#pgTitle').textContent = 'This download was removed'; $('#pgMain').disabled = true; return; }
  const pct = pctOf(v), conns = (v.lineStats || []).reduce((a, l) => a + (l.workers || 0), 0);
  const word = v.state === 'done' ? 'Complete' : v.state === 'error' ? 'Failed' : dlStateText(v);
  $('#pgTitle').textContent = v.name || 'Download';
  $('#pgUrl').textContent = v.url;
  const ty = $('#pgType'); ty.className = 'fi ' + (v.category || 'Other'); ty.textContent = extOf(v);
  setHTML($('#pgBig'), v.state === 'done' ? '100<small>%</small>' : v.size > 0 ? Math.floor(pct) + '<small>%</small>' : esc(bytes(v.done || 0)));
  const st = $('#pgState'); st.textContent = word; st.className = 'state ' + ({downloading:'run', done:'done', error:'err', paused:'paused', queued:'wait'}[v.state] || '');
  setHTML($('#pgRate'), v.state === 'downloading' ? `<b>${esc(bytes(v.speed || 0))}/s</b>${esc(dur(leftOf(v)) ? dur(leftOf(v)) + ' left' : 'working out the time left')}`
    : v.state === 'done' ? `<b>${esc(bytes(v.size || v.done || 0))}</b>saved` : '');
  const h = itemHist.get(v.id) || [];
  setHTML($('#pgSpark'), h.length >= 3 && v.state === 'downloading' ? (() => { const mx = Math.max(...h, 1), w = 300, ht = 34, n = h.length;
    const pts = h.map((x, i) => `${(i/(n-1)*w).toFixed(1)},${(ht-2-x/mx*(ht-6)).toFixed(1)}`).join(' ');
    return `<svg class="spark2" viewBox="0 0 ${w} ${ht}" preserveAspectRatio="none" role="img" aria-label="Speed over the last ${n} seconds, peak ${esc(bytes(mx))}/s"><polyline points="0,${ht} ${pts} ${w},${ht}" fill="var(--accent-soft)" stroke="none"/><polyline points="${pts}" fill="none" stroke="var(--accent)" stroke-width="1.5" vector-effect="non-scaling-stroke"/></svg>`; })() : '');
  $('#pgFill').style.width = (v.size > 0 || v.state === 'done' ? pct : 0) + '%';
  $('#pgFill').className = v.state === 'error' ? 'bad' : v.state === 'done' ? 'ok' : v.state === 'paused' ? 'paused' : '';
  setHTML($('#pgSeg'), dlSegBar(v));
  const rows = [
    // the state itself is shown next to the percentage; this row only carries what goes with it
    v.state === 'error' && v.err ? ['Problem', v.err] : null,
    v.state === 'queued' && v.retryAt > Date.now()/1000 ? ['Next try', 'in ' + dur(v.retryAt - Date.now()/1000)] : null,
    ['File size', v.size > 0 ? bytes(v.size) : 'not known yet'],
    ['Downloaded', bytes(v.done || 0) + (v.size > 0 ? ` (${pct.toFixed(1)}%)` : '')],
    ['Speed', v.state === 'downloading' ? bytes(v.speed || 0) + '/s' + (v.limit ? ' (limit ' + bytes(v.limit) + '/s)' : '') : '–'],
    ['Time left', v.state === 'downloading' ? (dur(leftOf(v)) || 'working it out') : '–'],
    ['Connections', v.state === 'downloading' ? String(conns || 1) : '–'],
    ['Can resume', v.state === 'done' ? '–' : v.noResume ? 'No: pausing starts it again from the beginning' : v.state === 'downloading' ? 'Yes' : '–'],
    ['Saved as', v.path || '(decided when the download starts)'],
  ];
  setHTML($('#pgGrid'), rows.filter(Boolean).map(([k, t]) => `<div class="${k === 'Saved as' || k === 'Problem' ? 'wide' : ''}"><span class="k">${k}</span><span class="v ${k === 'Problem' ? 'bad' : k === 'Can resume' && v.noResume && v.state !== 'done' ? 'warnv' : ''}" title="${esc(t)}">${esc(t)}</span></div>`).join(''));
  setHTML($('#pgLog'), (v.log || []).slice(-4).reverse().map(l => `<div><span>${new Date(l.t*1000).toLocaleTimeString(I18N.locale)}</span>${esc(l.s)}</div>`).join(''));
  const m = $('#pgMain'), label = v.state === 'done' ? 'Open' : v.state === 'downloading' || v.state === 'queued' ? 'Pause' : 'Resume';
  if (m.textContent !== label) { if (m.textContent) m.dataset.changed = Date.now(); m.textContent = label; }
  $('#pgFolder').disabled = !v.path;
}

/* ---------- menu bar ---------- */
// The same commands as the toolbar, the right-click menu and the keyboard, grouped so everything can be found from
// the top of the window. Each entry is built when its menu opens, so what is greyed out matches the selection.
const BAR_KEYS = {t:'tasks', d:'downloads', v:'view', o:'tools', h:'help'};
const barKey = e => { const k = BAR_KEYS[(e.code || '').replace(/^Key/, '').toLowerCase()] || BAR_KEYS[(e.key || '').toLowerCase()]; return k ? $(`#menubar [data-mb="${k}"]`) : null; };
function barStep(b, d){ const bs = $$('#menubar button'); return bs[(bs.indexOf(b) + d + bs.length) % bs.length]; }
function openBarMenu(b, focusFirst){
  if (!b || !DL || $('#dlDlg')) return;
  const r = b.getBoundingClientRect();
  showMenu(r.left, r.bottom, BAR_MENUS[b.dataset.mb](), true);
  b.classList.add('open');
  if (!focusFirst && menuEl) { const f = menuEl.querySelector('button:focus'); if (f) f.blur(); }
}
let uiPrefs = Object.assign({side: true, labels: true, status: true, dense: false}, store.get('ui', {}));
function applyUiPrefs(){
  document.body.classList.toggle('noside', !uiPrefs.side);
  document.body.classList.toggle('notb', !uiPrefs.labels);
  document.body.classList.toggle('nosb', !uiPrefs.status);
  document.body.classList.toggle('dense', !!uiPrefs.dense);
}
function setUiPref(k){ uiPrefs[k] = !uiPrefs[k]; store.set('ui', uiPrefs); applyUiPrefs(); }
const busy = () => DL.items.filter(v => v.state === 'downloading').length;
async function command(c){
  if (!DL) return;
  switch (c) {
    case 'add': return addDialog('');
    case 'resume': return doResume();
    case 'stop': return doStop();
    case 'stopall': if (!(await confirmNoResume(DL.items))) return; return dlAct({action:'pauseAll'}, 'All paused');
    case 'startall': return dlAct({action:'resumeAll'}, 'All started');
    case 'del': return doDelete(false);
    case 'delfin': { const n = DL.items.filter(v => v.state === 'done').length; const r = await askBox({title:`Remove ${n} finished download${n === 1 ? '' : 's'} from the list?`, text:'The files stay on your disk.', ok:'Clear'}); if (r.ok) dlAct({action:'clear'}, 'Finished downloads cleared'); return; }
    case 'queues': return dlQueues();
    case 'grab': return dlGrab();
    case 'opts': return showOptions('general');
  }
}
async function openFolder(which){ try { await api('app', {open: which}); } catch(e){ toast(e.message, true); } }
async function quitApp(){
  const n = busy();
  const r = await askBox({title:`Quit ${APP.name}?`, text: (n ? `${n} download${n === 1 ? ' is paused' : 's are paused'} and continue${n === 1 ? 's' : ''} the next time you start ${APP.name}. ` : '') + 'Downloads from the browser are not caught while it is closed.', ok:'Quit'});
  if (!r.ok) return;
  try { await api('app', {quit: true}); } catch(e){ return toast(e.message, true); }
  document.body.innerHTML = `<div class="empty" style="padding-top:20vh">${esc(APP.name)} has been closed. You can close this window.</div>`;
}
const canRedo = v => (v.state === 'done' || v.state === 'error') && /^(https?|ftp):\/\/|^magnet:/i.test(v.url || '');
async function redownload(){
  const s = selectedItems().filter(canRedo); if (!s.length) return;
  const r = await askBox({title: s.length === 1 ? `Download "${s[0].name || s[0].url}" again?` : `Download ${s.length} files again?`, text:'The file you already have is kept; the new copy gets a number added to its name if the name is taken.', ok:'Download again'});
  if (!r.ok) return;
  let n = 0;
  for (const v of s) { try { await api('downloads', {action:'add', url: v.url, force: true, start: true, ...(v.name && v.kind !== 'torrent' ? {name: v.name} : {})}); n++; } catch(e){ toast(e.message, true); } }
  await loadDL(); if (n) toast(n === 1 ? 'Downloading again' : `${n} downloading again`);
}
function shortcutsBox(){
  const K = [['Ctrl+N or Insert','Add a download'],['Ctrl+V','Add the link you copied'],['Enter','Open the file (or its progress, when not finished)'],['F2','Rename'],['Alt+Enter','Properties'],['Space','Pause or resume'],['Delete','Delete from the list'],['Shift+Delete','Delete with the file'],['Ctrl+A','Select all'],['↑ ↓ Home End','Move in the list (Shift: select)'],['Ctrl+F','Search'],['F5','Refresh'],['Alt or F10','Menu bar (then arrows, Enter, Escape)'],['Alt+T, D, V, O, H','Open Tasks, Downloads, View, Tools, Help'],['Shift+F10','Menu for the selected downloads'],['Escape','Close a dialog or menu; clear the selection']];
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Keyboard shortcuts" style="max-width:520px"><h3>Keyboard shortcuts</h3>
    <div class="keys-tbl">${K.map(k => `<kbd>${esc(k[0])}</kbd><span>${esc(k[1])}</span>`).join('')}</div>
    <div class="row" style="justify-content:flex-end;margin-top:14px"><button class="btn pri" id="kbOk" autofocus>Close</button></div></div>`);
  $('#kbOk').onclick = dlClose;
}
// Report a problem: the issue page on GitHub, with the version filled in; the log folder is opened too, so the log
// can be attached if the person wants (nothing is sent by Pullwell itself).
function reportProblem(){
  const body = encodeURIComponent(`**What happened**\n\n\n**What you expected**\n\n\n**Version:** ${APP.version}${APP.build && APP.build !== 'dev' ? ' (' + APP.build.slice(0, 7) + ')' : ''}\n**Windows:** ${navigator.userAgent.match(/Windows NT [\d.]+/) || ''}\n\nThe log (${APP.name.toLowerCase()}.log in the data folder) helps; look through it first, it lists the addresses you downloaded from.`);
  window.open(`https://github.com/${APP.repo}/issues/new?body=${body}`, '_blank', 'noopener');
  openFolder('log');
}
function aboutBox(){
  const u = UPD || {};
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="About" style="max-width:440px"><h3 class="row" style="gap:10px;align-items:center">${document.querySelector('.brand svg') ? document.querySelector('.brand svg').outerHTML.replace('<svg', '<svg style="width:28px;height:28px;flex:none"') : ''}${esc(APP.name)}</h3>
    <div class="hint2 selectable" style="margin-top:4px">Version ${esc(APP.version)}${APP.build && APP.build !== 'dev' ? ' (build ' + esc(APP.build.slice(0, 7)) + ')' : ''}${u.available ? ` · <b>${esc(u.latest)} is ready</b>` : ''}</div>
    <p class="hint2" style="margin:10px 0 0">A download manager for Windows. No telemetry and no account: it contacts only the sites you download from, and GitHub to look for updates.</p>
    ${APP.repo ? `<p class="hint2" style="margin:8px 0 0">Source code and releases: <a href="https://github.com/${esc(APP.repo)}" target="_blank" rel="noopener">github.com/${esc(APP.repo)}</a></p>` : ''}
    <div class="row" style="justify-content:flex-end;margin-top:14px"><button class="btn pri" id="abOk" autofocus>Close</button></div></div>`);
  $('#abOk').onclick = dlClose;
}
const limitLabel = l => l ? (l < 1 ? Math.round(l * 1024) + ' KB/s' : l + ' MB/s') : 'No limit'; // the limit is in binary units, so 0.25 is 256 KB/s
const BAR_MENUS = {
  tasks: () => [
    {t:'New download…', k:'Ctrl+N', do: () => addDialog('')},
    {t:'Add with options…', do: () => dlOptions()},
    {t:'Add a torrent file…', do: pickTorrent},
    '-',
    {t:'Grab links from a web page…', do: () => dlGrab()},
    '-',
    {t:'Load a list…', do: () => dlImport()},
    {t:'Save the list…', off: !DL.items.length, do: () => dlExport()},
    '-',
    {t:'Quit ' + APP.name, do: quitApp},
  ],
  downloads: () => {
    const s = selectedItems(), one = s.length === 1 ? s[0] : null, any = f => s.some(f);
    const qs = [{id:'', name:'Main queue'}].concat(DL.config.queues || []);
    const curQ = one ? (one.queue || '') : null, curLim = one ? one.limit || 0 : -1;
    return [
      {t:'Open', k:'Enter', off: !(one && one.state === 'done'), do: () => openItem(one)},
      {t:'Open folder', off: !(one && one.path), do: () => openItem(one, true)},
      '-',
      {t:'Resume', k:'Space', off: !any(v => v.state === 'paused' || v.state === 'error'), do: doResume},
      {t:'Pause', k:'Space', off: !any(v => v.state === 'downloading' || v.state === 'queued'), do: doStop},
      {t:'Download again', off: !any(canRedo), do: redownload},
      '-',
      {t:'Resume all', off: !DL.items.some(v => v.state === 'paused' || v.state === 'error'), do: () => command('startall')},
      {t:'Pause all', off: !DL.items.some(v => v.state === 'downloading' || v.state === 'queued'), do: () => command('stopall')},
      '-',
      {t:'Queue', off: !any(v => v.state !== 'done'), sub: qs.map(qq => ({t: qq.name, chk: curQ === qq.id, do: () => eachSel('setQueue', {queue: qq.id}, 'Moved')}))},
      {t:'Speed limit', off: !any(v => v.state !== 'done'), sub: LIMITS.map(l => ({t: limitLabel(l), chk: Math.abs(curLim - l * 1048576) < 1, do: () => eachSel('limit', {limitBytes: l * 1048576}, l ? 'Limit set' : 'Limit removed')}))},
      {t:'Copy link', off: !s.length, do: () => copyText(s.map(v => v.url).join('\n'), s.length > 1 ? 'Links copied' : 'Link copied')},
      {t:'Progress', off: !(one && one.state !== 'done'), do: () => progressBox(one.id)},
      {t:'Rename…', k:'F2', off: !canRename(one), do: () => renameBox(one)},
      {t:'Move to another folder…', off: !(one && one.state === 'done' && one.path && one.kind !== 'torrent'), do: () => moveBox(one)},
      {t:'Check the checksum…', off: !(one && one.state === 'done' && one.path), do: () => checksumBox(one)},
      {t:'Properties', k:'Alt+Enter', off: !one, do: () => dlProps(one.id)},
      '-',
      {t:'Select all', k:'Ctrl+A', off: !visible.length, do: () => { visible.forEach(id => sel.add(id)); render(); }},
      {t:'Delete', k:'Del', off: !s.length, do: () => doDelete(false)},
      {t:'Delete with file', k:'Shift+Del', off: !any(v => v.state === 'done'), do: () => doDelete(true)},
      {t:'Clear finished', off: !DL.items.some(v => v.state === 'done'), do: () => command('delfin')},
    ];
  },
  view: () => [
    {t:'Categories panel', chk: uiPrefs.side, do: () => setUiPref('side')},
    {t:'Toolbar labels', chk: uiPrefs.labels, do: () => setUiPref('labels')},
    {t:'Status bar', chk: uiPrefs.status, do: () => setUiPref('status')},
    '-',
    {t:'Columns', sub: COLS.filter(c => c.k !== 'name').map(c => ({t: c.t, chk: !hideCols.includes(c.k), do: () => { hideCols = hideCols.includes(c.k) ? hideCols.filter(x => x !== c.k) : hideCols.concat(c.k); store.set('hideCols', hideCols); render(); }}))
      .concat(['-', {t:'Reset widths', do: () => { widths = {}; store.set('widths', widths); headKey = ''; render(); }}])},
    {t:'Sort by', sub: [{k:'order', t:'Queue order'}].concat(COLS).map(c => ({t: c.t, chk: sortBy.c === c.k, do: () => { sortBy = {c: c.k, d: sortBy.c === c.k ? -sortBy.d : 1}; store.set('sort', sortBy); render(); }}))},
    {t:'Show', sub: [[{k:'all'},'All downloads'],[{k:'unfinished'},'Unfinished'],[{k:'finished'},'Finished'],[{k:'errors'},'Needs attention']].map(([vw, t]) => ({t, chk: view.k === vw.k, do: () => { view = vw; store.set('view', view); sel.clear(); cur = null; render(); }}))},
    {t:'Row spacing', sub: [['Comfortable', false], ['Compact', true]].map(([t, d]) => ({t, chk: !!uiPrefs.dense === d, do: () => { if (!!uiPrefs.dense !== d) setUiPref('dense'); }}))},
    {t:'Text size', sub: ZOOMS.map(([z, t]) => ({t, chk: store.get('zoom', 1) === z, do: () => { store.set('zoom', z); applyZoom(); }}))},
    {t:'Theme', sub: [['auto','Same as Windows'],['light','Light'],['dark','Dark']].map(([k, t]) => ({t, chk: store.get('theme', 'auto') === k, do: () => { store.set('theme', k); applyTheme(); }}))},
    {t:'Language', sub: langChoices().map(([k, t]) => ({t, chk: I18N.choice === k, do: () => { if (I18N.choice !== k) I18N.set(k); }}))},
    '-',
    {t:'Refresh', k:'F5', do: () => loadDL()},
  ],
  tools: () => [
    {t:'Scheduler and queues…', do: () => dlQueues()},
    {t:'Folders for types and websites…', do: () => dlFolders()},
    {t:'Options…', do: () => showOptions('general')},
    {t:'Speed limit for all downloads', sub: LIMITS.map(l => ({t: limitLabel(l), chk: Math.abs((DL.config.limitAll || 0) - l * 1048576) < 1, do: () => dlAct({action:'config', config: {...DL.config, limitAll: l * 1048576}}, l ? 'All downloads limited to ' + limitLabel(l) : 'No speed limit')}))},
    '-',
    {t:'Connect a browser…', do: extConnect},
    '-',
    {t:'Open the download folder', do: () => openFolder('downloads')},
    {t:'Open the log folder', do: () => openFolder('log')},
  ],
  help: () => [
    {t:'Keyboard shortcuts', do: shortcutsBox},
    {t:'Check for updates', do: () => { showOptions('updates'); updAction({action:'check'}, null); }},
    '-',
    {t:"What's new", off: !APP.repo, do: () => window.open(`https://github.com/${APP.repo}/releases`, '_blank', 'noopener')},
    {t:'Report a problem…', off: !APP.repo, do: reportProblem},
    {t:'About ' + APP.name, do: aboutBox},
  ],
};
async function copyText(t, msg){
  try { await navigator.clipboard.writeText(t); toast(msg); }
  catch(e){ const a = document.createElement('textarea'); a.value = t; document.body.appendChild(a); a.select(); try { document.execCommand('copy'); toast(msg); } catch(e2){ toast('Could not copy', true); } a.remove(); }
}

/* ---------- dialogs (also used by downloads-more.js) ---------- */
let dlOpener = null;
function dlClose(){
  const d = $('#dlDlg'); if (d) d.remove();
  $$('[data-dlinert]').forEach(el => { el.inert = false; el.removeAttribute('data-dlinert'); });
  if (dlOpener && document.contains(dlOpener)) { try { dlOpener.focus(); } catch(e){} }
  dlOpener = null; optTab = null;
  if (askResolve) { const r = askResolve; askResolve = null; r({ok:false}); }
}
function dlDialog(html){
  closeMenu();
  let d = $('#dlDlg');
  if (!d) {
    dlOpener = document.activeElement;
    d = document.createElement('div'); d.id = 'dlDlg'; d.className = 'wiz'; document.body.appendChild(d);
    [...document.body.children].forEach(el => { if (el !== d && el.tagName !== 'SCRIPT' && !el.inert && el.id !== 'toast') { el.inert = true; el.setAttribute('data-dlinert', ''); } });
    d.addEventListener('mousedown', e => { if (e.target === d) d.dataset.down = '1'; });
    d.addEventListener('mouseup', e => { if (e.target === d && d.dataset.down) dlClose(); delete d.dataset.down; });
  }
  setHTML(d, html);
  const card = d.querySelector('.card'); if (card) { card.setAttribute('tabindex', '-1'); const f = card.querySelector('[autofocus]') || card.querySelector('textarea,input:not([type=hidden]):not([type=checkbox]),select,button.pri,button'); (f || card).focus(); }
}
document.addEventListener('keydown', e => {
  const d = $('#dlDlg'); if (!d) return;
  if (e.key === 'Escape') { e.preventDefault(); dlClose(); return; }
  if (e.key !== 'Tab') return;
  const f = [...d.querySelectorAll('input:not([type=hidden]):not([disabled]),select:not([disabled]),textarea:not([disabled]),button:not([disabled]),a[href]')].filter(x => x.offsetParent !== null);
  if (!f.length) return;
  const first = f[0], last = f[f.length - 1];
  if (e.shiftKey && (document.activeElement === first || !d.contains(document.activeElement))) { e.preventDefault(); last.focus(); }
  else if (!e.shiftKey && (document.activeElement === last || !d.contains(document.activeElement))) { e.preventDefault(); first.focus(); }
}, true);

// askBox: a small question with OK/Cancel and an optional tick box. Resolves {ok, checked}.
let askResolve = null;
function askBox({title, text, ok = 'OK', danger = false, check = '', checked = false}){
  return new Promise(res => {
    dlDialog(`<div class="card" role="alertdialog" aria-modal="true" aria-label="${esc(title)}" style="max-width:440px"><h3 style="word-break:break-word">${esc(title)}</h3>
      ${text ? `<div class="hint2" style="margin:6px 0 4px">${esc(text)}</div>` : ''}
      ${check ? `<label class="row" style="margin-top:10px;gap:6px"><input type="checkbox" id="askChk"${checked ? ' checked' : ''}> ${esc(check)}</label>` : ''}
      <div class="row" style="justify-content:flex-end;margin-top:14px"><button class="btn" id="askNo">Cancel</button><button class="btn pri" id="askYes"${danger ? ' style="background:var(--bad);border-color:var(--bad);color:#fff"' : ''} autofocus>${esc(ok)}</button></div></div>`);
    askResolve = res;
    $('#askNo').onclick = () => dlClose();
    $('#askYes').onclick = () => { const c = $('#askChk') ? $('#askChk').checked : false; askResolve = null; dlClose(); res({ok:true, checked:c}); };
  });
}

/* ---------- adding downloads ---------- */
const linksIn = text => (text || '').split(/\s*[\r\n]+\s*/).map(s => s.trim()).filter(s => s);
function addDialog(prefill){
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Add a download" style="max-width:600px"><h3>Add a download</h3>
    <div class="hint2" style="margin-bottom:8px">Paste a link (http, https, ftp or magnet). Several links: one per line. <code>file[001-050].jpg</code> adds a numbered series.</div>
    <textarea class="inp" id="addUrl" rows="3" style="width:100%" spellcheck="false" placeholder="https://…" autofocus>${esc(prefill || '')}</textarea>
    <div class="row" style="margin-top:12px"><button class="btn" id="addPaste">Paste</button><button class="btn" id="addTor">Torrent file…</button><button class="btn" id="addMore">More options…</button>
      <span style="flex:1"></span><button class="btn" id="addNo">Cancel</button><button class="btn pri" id="addOk">OK</button></div></div>`);
  const ta = $('#addUrl'); ta.focus(); ta.setSelectionRange(ta.value.length, ta.value.length);
  ta.onkeydown = e => { if (e.key === 'Enter' && !e.shiftKey && !e.ctrlKey) { e.preventDefault(); $('#addOk').click(); } };
  $('#addNo').onclick = dlClose;
  $('#addTor').onclick = () => { dlClose(); pickTorrent(); };
  $('#addPaste').onclick = async () => { try { const s = await navigator.clipboard.readText(); if (s) { ta.value = s.trim(); ta.focus(); } } catch(e){ toast('Press Ctrl+V in the box', true); ta.focus(); } };
  $('#addOk').onclick = () => submitLinks(linksIn(ta.value));
  $('#addMore').onclick = () => { const v = ta.value; dlOptions(); if ($('#oUrl')) $('#oUrl').value = v; };
}
async function submitLinks(links){
  if (!links.length) return toast('Paste a link first', true);
  const series = /\[(\d+-\d+(:\d+)?|[a-z]-[a-z])\]/i.test(links[0]); // file[001-050].jpg: many files, added without checking each
  if (links.length > 1 || series || /^magnet:/i.test(links[0])) {
    const r = await dlAct({action:'add', urls: links});
    if (r) { dlClose(); toast(r.duplicate && !(r.added || []).length ? 'Already in your list' : `${(r.added || []).length} added` + (r.skipped ? `, ${r.skipped} already in your list` : '')); }
    return;
  }
  askHdr = null;
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Checking the link" style="max-width:600px"><h3>Checking the link…</h3><div class="hint2 selectable" style="margin-top:6px;word-break:break-all">${esc(links[0])}</div></div>`);
  let p; try { p = await api('downloads', {action:'probe', url: links[0]}); } catch(e){ dlClose(); return toast(e.message, true); }
  if (!$('#dlDlg')) return; // closed while checking
  showProbe(links[0], p);
}
function showProbe(link, p){
  const info = p.info || {}, fine = p.ok, hls = fine && p.hls;
  const audio = hls && p.hls.audio ? (p.mux ? ' Picture and sound are joined with ffmpeg.' : ' The sound is a separate track; without ffmpeg you get two files.') : '';
  const how = !fine ? '' : hls ? `Video in ${p.hls.segments} parts, downloaded several at a time and joined into one file.`
    : !info.ranges ? 'This website does not allow splitting the file: one connection is used.'
    : !(info.size > 0) ? 'The size is not known: one connection is used.'
    : p.locked ? 'This website allows only one connection for this file.'
    : 'The file is split into parts and downloaded with several connections at once.';
  const size = hls ? 'Video stream · about ' + (p.hls.seconds < 90 ? Math.round(p.hls.seconds) + ' s' : Math.round(p.hls.seconds/60) + ' min') + (p.hls.quality ? ' · ' + esc(p.hls.quality) : '') : info.size > 0 ? bytes(info.size) : 'Size not known';
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Download file info" style="max-width:600px"><h3>Download file info</h3>
    <div class="hint2 selectable" style="word-break:break-all;margin-bottom:10px">${esc(link)}</div>
    ${fine ? '' : `<div class="note bad" style="margin-bottom:10px">${esc(p.error || 'The link could not be checked.')} You can still add it.</div>`}
    ${info.warning === 'html' ? '<div class="note warn" style="margin-bottom:10px"><b>This looks like a web page, not a file.</b> The site may want you to sign in first.</div>' : ''}
    ${p.duplicate ? '<div class="note warn" style="margin-bottom:10px"><b>This link is already in your list.</b> Adding it again makes a second copy.</div>' : ''}
    <div style="display:grid;grid-template-columns:90px 1fr;gap:8px 10px;align-items:center">
      <label for="dlName" class="hint2">File name</label><input class="inp" id="dlName" value="${esc(info.name || '')}" spellcheck="false">
      <label for="dlFolder" class="hint2">Save in</label><input class="inp" id="dlFolder" value="${esc(p.dir || DL.config.dir)}" spellcheck="false">
      <span class="hint2">Size</span><span><b>${size}</b>${hls ? '' : info.type ? ' <span class="hint2">· ' + esc(info.type) + '</span>' : ''}</span>
      ${hls && (p.hls.heights || []).length > 1 ? `<label for="dlQual" class="hint2">Quality</label><select class="sel" id="dlQual"><option value="0"${!p.height ? ' selected' : ''}>Best (${p.hls.heights[0]}p)</option>${p.hls.heights.slice(1).map(h => `<option value="${h}"${p.height === h ? ' selected' : ''}>${h}p</option>`).join('')}</select>` : ''}
      <label for="dlQ" class="hint2">Queue</label><select class="sel" id="dlQ">${dlQueueOpts('')}</select>
      <span></span><label class="row" style="gap:6px"><input type="checkbox" id="dlOpenDone"${store.get('openDone', false) ? ' checked' : ''}> Open it when it has finished</label>
    </div>
    ${how ? `<div class="hint2" style="margin-top:10px">${esc(how + audio)}</div>` : ''}
    <details style="margin-top:8px"><summary class="hint2" style="cursor:pointer">More</summary><div style="display:grid;grid-template-columns:90px 1fr;gap:8px 10px;align-items:center;margin-top:8px">
      <label for="dlTime" class="hint2">Start at</label><input type="time" class="inp" id="dlTime" title="Leave empty to start now">
      <label for="dlHash" class="hint2">Checksum</label><input class="inp" id="dlHash" placeholder="SHA-256, SHA-1 or MD5 from the website (optional)" spellcheck="false">
    </div></details>
    <div class="row" style="justify-content:flex-end;margin-top:14px"><button class="btn" id="dlCancel">Cancel</button><button class="btn" id="dlLater">Download later</button><button class="btn pri" id="dlGo">Start download</button></div></div>`);
  if ($('#dlQual')) $('#dlQual').onchange = async e => { const h = +e.target.value; try { const q2 = await api('downloads', {action:'probe', url: link, height: h, header: askHdr}); q2.height = h; showProbe(link, q2); } catch(err){ toast(err.message, true); } };
  $('#dlGo').onclick = () => confirmAdd(link, p, true);
  $('#dlLater').onclick = () => confirmAdd(link, p, false);
  $('#dlCancel').onclick = () => { if (askHdr) api('downloads', {action: 'askDecline', url: link}).catch(() => {}); dlClose(); };
  $('#dlName').onkeydown = $('#dlFolder').onkeydown = e => { if (e.key === 'Enter') { e.preventDefault(); $('#dlGo').click(); } };
  setTimeout(() => $('#dlGo') && $('#dlGo').focus(), 20);
}
async function confirmAdd(link, p, start){
  const name = $('#dlName').value.trim(), folder = $('#dlFolder').value.trim();
  const body = {action:'add', url: link, start};
  if (askHdr) body.header = askHdr;
  if (name) body.name = name;
  if (folder && folder !== p.dir) body.dir = folder;
  if (p.duplicate) body.force = true;
  if (p.height) body.height = p.height;
  if ($('#dlQ').value) body.queue = $('#dlQ').value;
  const tm = $('#dlTime').value;
  if (tm) { const [h, mi] = tm.split(':').map(Number); const d = new Date(); d.setHours(h, mi, 0, 0); if (d.getTime() <= Date.now()) d.setDate(d.getDate() + 1); body.startAt = Math.floor(d.getTime()/1000); }
  const hs = $('#dlHash').value.trim(); if (hs) body.hash = hs;
  if ($('#dlOpenDone').checked) body.openWhenDone = true;
  store.set('openDone', $('#dlOpenDone').checked); // remembered for the next download
  const r = await dlAct(body, start ? 'Download started' : 'Added');
  if (r) { dlClose(); const v = (r.added || [])[0]; if (v && v.id) { sel.clear(); sel.add(v.id); cur = anchor = v.id; render(); } }
}
function pickTorrent(){ const f = $('#torFile'); f.value = ''; f.click(); }
async function addTorrentFile(f){
  if (!f) return;
  if (f.size > 16e6) return toast('That file is too big to be a torrent', true);
  const buf = new Uint8Array(await f.arrayBuffer()); let bin = ''; for (let i = 0; i < buf.length; i += 32768) bin += String.fromCharCode.apply(null, buf.subarray(i, i + 32768));
  const torrent = btoa(bin);
  let p; try { p = await api('downloads', {action:'torrentInfo', torrent}); } catch(e){ return toast(e.message, true); }
  const fl = p.files || [];
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Add torrent" style="max-width:600px"><h3 style="word-break:break-all">${esc(p.name)}</h3>
    <div class="hint2" style="margin:2px 0 10px">${bytes(p.size)} · ${fl.length} file${fl.length === 1 ? '' : 's'}${p.private ? ' · private torrent' : ''}</div>
    ${p.duplicate ? '<div class="note warn" style="margin-bottom:10px"><b>This torrent is already in your list.</b></div>' : ''}
    ${fl.length > 1 ? `<div class="row" style="margin-bottom:6px"><label class="row" style="gap:6px"><input type="checkbox" id="tpAll" checked> All files</label><span class="hint2" id="tpSum"></span></div>` : ''}
    <div class="scrollbox" style="max-height:240px">${fl.map((x, i) => `<label class="row" style="gap:8px;padding:2px 0;flex-wrap:nowrap"><input type="checkbox" class="tpSel" data-i="${i}" checked><span style="flex:1;word-break:break-all">${esc(x.name)}</span><span class="hint2">${bytes(x.size)}</span></label>`).join('')}</div>
    <label class="hint2" style="display:block;margin-top:10px">Save in<input class="inp" id="tpDir" value="${esc(p.dir)}" style="width:100%;margin-top:3px" spellcheck="false"></label>
    <div class="row" style="justify-content:flex-end;margin-top:14px"><button class="btn" id="dlCancel">Cancel</button><button class="btn" id="tpLater">Download later</button><button class="btn pri" id="tpGo">Start download</button></div></div>`);
  const sum = () => { const on = $$('.tpSel').filter(b => b.checked); if ($('#tpSum')) $('#tpSum').textContent = `${on.length} chosen · ${bytes(on.reduce((a, b) => a + fl[+b.dataset.i].size, 0))}`; if ($('#tpAll')) $('#tpAll').checked = on.length === fl.length; };
  sum(); $$('.tpSel').forEach(b => b.onchange = sum);
  if ($('#tpAll')) $('#tpAll').onchange = e => { $$('.tpSel').forEach(b => b.checked = e.target.checked); sum(); };
  const go = async start => {
    const s = $$('.tpSel').map(b => b.checked); if (!s.some(Boolean)) return toast('Choose at least one file', true);
    const body = {action:'add', torrent, start, force: !!p.duplicate};
    if (!s.every(Boolean)) body.select = s;
    const dir = $('#tpDir').value.trim(); if (dir && dir !== p.dir) body.dir = dir;
    const r = await dlAct(body, start ? 'Torrent added' : 'Added'); if (r) dlClose();
  };
  $('#tpGo').onclick = () => go(true); $('#tpLater').onclick = () => go(false); $('#dlCancel').onclick = dlClose;
}

/* ---------- options ---------- */
let optTab = null;
const OPT_TABS = [['general','General'],['connection','Connection'],['schedule','Schedule'],['browser','Browser'],['torrents','Torrents'],['updates','Updates'],['advanced','Advanced']];
function limSel(key, cur){
  const near = LIMITS.reduce((b, x) => Math.abs(x*1048576 - cur) < Math.abs(b*1048576 - cur) ? x : b, 0);
  return `<select class="sel" data-cfg="${esc(key)}" data-lim2="1" aria-label="Speed limit">${LIMITS.map(x => `<option value="${x}"${x === near ? ' selected' : ''}>${limitLabel(x)}</option>`).join('')}</select>`;
}
const tog = (attr, on, label) => `<button class="tog${on ? ' on' : ''}" ${attr} role="switch" aria-checked="${!!on}" aria-label="${esc(label)}"></button>`;
const orow = (title, desc, ctl) => `<div class="dnsrow"><div><b>${title}</b>${desc ? `<div class="d">${desc}</div>` : ''}</div>${ctl}</div>`;
function optionsBody(tab){
  const c = DL.config;
  const num = (id, vals, v) => `<select class="sel" id="${id}">${vals.map(x => `<option${x == v ? ' selected' : ''}>${x}</option>`).join('')}</select>`;
  if (tab === 'general') return (APPCFG ? orow('Start with Windows', APPCFG.canAutostart ? `${esc(APP.name)} starts in the notification area when you sign in, so browser downloads are caught right away.` : `Not available for this copy of ${esc(APP.name)}.`, APPCFG.canAutostart ? tog('id="autoTog"', APPCFG.autostart, 'Start with Windows') : '') : '')
    + orow('Save downloads in', 'New downloads go into a folder for their type (Videos, Programs …) when that is on.', `<div class="row" style="flex-wrap:nowrap"><input class="inp" id="dlDir" value="${esc(c.dir)}" style="width:250px" spellcheck="false"><button class="btn" id="dlDirSave">Save</button></div>`)
    + orow('Sort into folders by type', 'Videos, Programs, Documents and so on.', tog('id="dlCatTog"', c.useCategories, 'Sort into folders'))
    + orow('Folders for types and websites', 'Send Videos to one folder, or everything from one website into its own folder.', `<button class="btn" data-dm="folders">Choose folders…</button>`)
    + orow('If a file with that name is already there', '', `<select class="sel" data-cfg="onExists">${[['rename','Keep both'],['overwrite','Replace the old file'],['skip','Skip the download']].map(o => `<option value="${o[0]}"${(c.onExists || 'rename') === o[0] ? ' selected' : ''}>${o[1]}</option>`).join('')}</select>`)
    + orow('Notify me when a download finishes', 'Problems are always shown.', tog('data-dm="notifyTog"', !c.noNotify, 'Notify when finished'))
    + (APPCFG ? orow('Show a finished download', 'Bring up its window with Open and Open folder as soon as it finishes (also when the window is closed).', tog('id="doneTog"', APPCFG.showDone, 'Show a finished download')) : '')
    + (APPCFG && APPCFG.canInstall ? orow('Start menu entry', `So Windows search and the Start menu find ${esc(APP.name)}, and notifications show its name and icon.`, tog('id="smTog"', APPCFG.startMenu, 'Start menu entry')) : '')
    + (APPCFG ? orow('Offer links I copy', `When you copy a link to a file (a program, archive, video, PDF …) anywhere, a notification asks whether to download it. Nothing is downloaded unless you click it and confirm.`, tog('id="clipTog"', APPCFG.clipboard, 'Offer copied links')) : '')
    + orow('Language', 'The language of the menus and dialogs. Anything not translated yet stays in English.', `<select class="sel" id="optLang">${langChoices().map(o => `<option value="${o[0]}"${I18N.choice === o[0] ? ' selected' : ''}>${o[1]}</option>`).join('')}</select>`)
    + orow('Appearance', '', `<select class="sel" id="optTheme">${[['auto','Same as Windows'],['light','Light'],['dark','Dark']].map(o => `<option value="${o[0]}"${store.get('theme', 'auto') === o[0] ? ' selected' : ''}>${o[1]}</option>`).join('')}</select>`);
  if (tab === 'connection') return orow('Downloads at the same time', 'More than 3 can slow each one down. Recommended: 3.', num('dlMax', [1,2,3,4,5,6,8,10], c.maxActive))
    + orow('Connections per download', `How many parallel connections one file uses. Some websites refuse many; ${esc(APP.name)} lowers it by itself. Recommended: 8.`, num('dlPer', [1,2,4,6,8,12,16,24,32], c.maxPerLine))
    + orow('Speed limit for all downloads', 'Keeps some speed free for everything else.', limSel('limitAll', c.limitAll))
    + (APPCFG ? orow('Proxy', esc(APPCFG.systemProxy || ''), `<div style="display:grid;gap:6px;justify-items:end">
        <select class="sel" id="pxMode">${[['system','Same as Windows'],['none','No proxy'],['manual','This proxy:']].map(o => `<option value="${o[0]}"${((APPCFG.proxy||{}).mode || 'system') === o[0] ? ' selected' : ''}>${o[1]}</option>`).join('')}</select>
        <div class="row" style="flex-wrap:nowrap;${((APPCFG.proxy||{}).mode) === 'manual' ? '' : 'display:none'}" id="pxRow"><input class="inp" id="pxAddr" value="${esc((APPCFG.proxy||{}).addr || '')}" placeholder="host:port or socks5://host:port" style="width:220px" spellcheck="false"><button class="btn" id="pxSave">Save</button></div>
        <button class="btn" id="pxTest">Test</button></div>`) : '')
    + orow('Saved logins', 'A user name and password kept for a website, used for every download from it. They stay on this PC, protected by your Windows account.', `<button class="btn" data-dm="logins">Manage logins…</button>`)
    + orow(`What ${esc(APP.name)} learned about websites`, `Some websites only accept one connection per file. ${DL.knownSites ? DL.knownSites + ' site' + (DL.knownSites === 1 ? '' : 's') + ' remembered.' : 'Nothing remembered.'}`, `<button class="btn" id="dlForget"${DL.knownSites ? '' : ' disabled'}>Forget</button>`);
  if (tab === 'schedule') return (APPCFG ? orow('Keep the PC awake while downloading', `Windows does not go to sleep while a download is running (the screen can still turn off). It may sleep again when nothing is downloading.`, tog('id="awakeTog"', APPCFG.keepAwake, 'Keep the PC awake while downloading')) : '')
    + orow('Only download at certain hours', 'For example at night. Downloads outside these hours wait.', `<div class="row" style="flex-wrap:nowrap"><input type="time" class="inp" id="dlWinFrom" value="${esc(c.window.from)}"><span class="hint2">to</span><input type="time" class="inp" id="dlWinTo" value="${esc(c.window.to)}">${tog('id="dlWinTog"', c.window.enabled, 'Download hours')}</div>`)
    + orow('When everything is finished', 'Done once, after the last download. A shutdown gives you one minute to cancel.', `<select class="sel" data-cfg="afterAll">${[['','Do nothing'],['sleep','Put the PC to sleep'],['shutdown','Shut the PC down']].map(o => `<option value="${o[0]}"${(c.afterAll || '') === o[0] ? ' selected' : ''}>${o[1]}</option>`).join('')}</select>`)
    + orow('Queues', `Groups of downloads with their own hours and days, for example “Night”. ${(c.queues || []).length} queue${(c.queues || []).length === 1 ? '' : 's'}.`, `<button class="btn" data-dm="queues">Manage queues…</button>`);
  if (tab === 'browser') {
    const x = DL.ext || {browsers: [], minMB: 5, enabled: true}, bs = x.browsers || [];
    return orow('Take over downloads from the browser', `Big downloads in Chrome, Edge, Brave or Firefox go to ${esc(APP.name)} once the extension is connected. If ${esc(APP.name)} is not running, the browser downloads as usual.`, tog('id="extOn"', x.enabled, 'Take over browser downloads'))
      + orow('Only files bigger than', 'Smaller files are quicker to leave with the browser.', `<select class="sel" id="extMin">${[0,1,5,10,25,50,100].map(n => `<option value="${n}"${x.minMB === n ? ' selected' : ''}>${n ? n + ' MB' : 'Everything'}</option>`).join('')}</select>`)
      + orow('Ask before a browser download starts', `Shows the “Download file info” box first, so a wrong click does not start a download. You can change the name and folder there.`, tog('id="extAsk"', x.ask !== false, 'Ask before a browser download starts'))
      + orow('Connected browsers', bs.length ? '' : 'None yet.', `<button class="btn pri" id="extCode">Connect a browser…</button>`)
      + bs.map(b => orow(esc(b.name || 'Browser'), `Connected ${when(b.created)}${b.lastSeen ? ' · last used ' + when(b.lastSeen) : ''}`, `<button class="btn" data-extrm="${esc(b.id)}">Disconnect</button>`)).join('');
  }
  if (tab === 'updates') {
    const u = UPD || {};
    const state = u.status === 'downloading' ? `Downloading… ${u.progress || 0}%` : u.status === 'installing' ? `Installing: ${esc(APP.name)} restarts in a moment.`
      : u.available ? `<b>Version ${esc(u.latest)} is ready.</b>` : u.error ? 'Could not check for updates.' : u.checked ? 'You have the newest version.' : 'Not checked yet.';
    return orow('Version', `${esc(APP.name)} ${esc(u.current || APP.version)}${u.build && u.build !== 'dev' ? ' (build ' + esc(u.build.slice(0, 7)) + ')' : ''} · ${state}${u.error ? `<br><span class="bad">${esc(u.error)}</span>` : ''}`,
        `<div class="row" style="flex-wrap:nowrap"><button class="btn" id="updCheck"${u.status && u.status !== 'idle' ? ' disabled' : ''}>Check now</button>${u.available ? `<button class="btn pri" id="updInstall"${u.status !== 'idle' ? ' disabled' : ''}>Install and restart</button>` : ''}</div>`)
      + orow('Install updates by themselves', `When an update is ready and nothing is downloading, ${esc(APP.name)} installs it and restarts. Unfinished downloads continue afterwards.`, tog('id="updAuto"', u.auto, 'Install updates by themselves'))
      + orow('Which versions', 'Test builds are every change as soon as it is made; releases are the tested versions.', `<select class="sel" id="updChannel"><option value="release"${u.channel !== 'test' ? ' selected' : ''}>Releases</option><option value="test"${u.channel === 'test' ? ' selected' : ''}>Test builds</option></select>`)
      + (u.previous ? orow('Go back to the previous version', 'If the new version gives trouble. You can come back to this one the same way.', `<button class="btn" id="updBack">Go back</button>`) : '');
  }
  if (tab === 'torrents') return orow('Torrents and magnet links', 'Turn off to refuse magnet links and .torrent files.', tog('data-dm="torTog"', !c.noTorrents, 'Allow torrents'))
    + (c.noTorrents ? '' : orow('Share while downloading', 'Pieces you already have go out to others while the rest comes in.', tog('data-dm="upTog"', !c.noUpload, 'Share while downloading'))
    + (c.noUpload ? '' : orow('Upload speed limit', '', limSel('uploadLimit', c.uploadLimit || 0))));
  return orow('Run a program after each download', 'For example a virus scanner. Use <code>%f</code> for the file: <code>"C:\\Tools\\scan.exe" "%f"</code>', `<div class="row" style="flex-wrap:nowrap"><input class="inp" id="dlAfterCmd" value="${esc(c.afterCmd || '')}" style="width:230px" spellcheck="false"><button class="btn" data-dm="afterSave">Save</button></div>`)
    + orow('ffmpeg', DL.ffmpeg ? 'Found: web videos with a separate sound track are joined into one file.' : `Not found: such videos are saved as two files. Put ffmpeg.exe next to ${esc(APP.name)} to join them.`, `<span class="hint2">${DL.ffmpeg ? '✓ ready' : 'not installed'}</span>`)
    + orow('Your list', 'Save the list of downloads to a file, or load one (also a text file with one link per line).', `<div class="row" style="flex-wrap:nowrap"><button class="btn" data-dm="export">Save list…</button><button class="btn" data-dm="import">Load list…</button></div>`)
    + orow('About', `${esc(APP.name)} ${esc(APP.version)}. No telemetry: it contacts only the sites you download from.`, '')
    + (APPCFG && APPCFG.canInstall ? orow(`Uninstall ${esc(APP.name)}`, `Removes the Start menu entry, the startup entry and the program file. Your downloaded files stay where they are.`, `<button class="btn" id="uninst">Uninstall…</button>`) : '');
}
let APPCFG = null; // {autostart, canAutostart}
async function loadAppCfg(){ try { APPCFG = await api('app'); } catch(e){ APPCFG = null; } }
function showOptions(tab){
  if (!APPCFG && !showOptions.asked) { showOptions.asked = true; loadAppCfg().then(() => { if (optTab && $('.opt')) showOptions(optTab); }); }
  optTab = tab || optTab || 'general';
  const keepFocus = document.activeElement && document.activeElement.id;
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Options" style="max-width:720px;padding-bottom:12px"><div class="row"><h3 style="flex:1">Options</h3><button class="btn" id="optClose">Close</button></div>
    <div class="opt"><nav>${OPT_TABS.map(t => `<button data-opt="${t[0]}" class="${optTab === t[0] ? 'sel' : ''}">${t[1]}</button>`).join('')}</nav><section>${optionsBody(optTab)}</section></div></div>`);
  $('#optClose').onclick = dlClose;
  if (keepFocus && $('#' + keepFocus) && $('#dlDlg').contains($('#' + keepFocus))) $('#' + keepFocus).focus();
}
function afterChange(){ if (optTab && $('#dlDlg') && $('.opt')) showOptions(optTab); }

/* ---------- connecting a browser ---------- */
async function extConnect(){
  let r; try { r = await api('downloads', {action:'extCode'}); } catch(e){ return toast(e.message, true); }
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Connect a browser" style="max-width:600px"><h3>Connect a browser</h3>
    <div class="hint2" style="margin-bottom:10px">The extension is not in the browser stores yet, so it is added by hand once (about a minute).</div>
    <ol class="hint2" style="margin:0 0 12px;padding-left:18px;display:grid;gap:8px">
      <li><b>Save the extension</b> (it goes into your download folder, which then opens): <button class="btn" data-saveext="chrome-edge">Chrome / Edge / Brave</button> <button class="btn" data-saveext="firefox">Firefox</button></li>
      <li><b>Chrome, Edge, Brave:</b> unzip it into a folder you keep. Open <code>chrome://extensions</code> (Edge: <code>edge://extensions</code>) <button class="btn" id="extCopy" style="padding:1px 8px;min-height:0">Copy</button>, switch on <b>Developer mode</b>, click <b>Load unpacked</b> and choose that folder.<br><b>Firefox:</b> open <code>about:debugging#/runtime/this-firefox</code>, click <b>Load Temporary Add-on</b> and choose the zip (Firefox forgets it when it closes).</li>
      <li><b>Pin the ${esc(APP.name)} icon</b> (the puzzle piece in the toolbar), click it and type this code:</li>
    </ol>
    <div class="selectable" style="font:700 28px/1.2 var(--mono);letter-spacing:.12em;text-align:center;padding:12px;border-radius:8px;background:var(--inset)">${esc(r.code)}</div>
    <div class="hint2" style="margin-top:6px;text-align:center" id="extLeft"></div>
    <div class="row" style="justify-content:flex-end;margin-top:14px"><button class="btn pri" id="extDone">Done</button></div></div>`);
  $('#extDone').onclick = () => showOptions('browser');
  $('#extCopy').onclick = () => copyText('chrome://extensions', 'Copied: paste it in a new browser tab');
  $$('[data-saveext]').forEach(b => b.onclick = async () => { try { const r = await api('app', {saveExt: b.dataset.saveext}); toast('Saved: ' + r.path); } catch(e){ toast(e.message, true); } });
  const tick = () => { const el = $('#extLeft'); if (!el) return; const left = r.expires - Math.floor(Date.now()/1000); if (left <= 0) { el.textContent = 'The code has expired. Close this and connect again.'; return; } el.textContent = `Valid for ${Math.floor(left/60)}:${String(left%60).padStart(2,'0')}, one use only.`; setTimeout(tick, 1000); };
  tick();
}

async function updAction(body, msg){
  try { UPD = await api('update', body); if (msg) toast(msg); }
  catch(e){ toast(e.message, true); await loadUpd(); }
  updAt = Date.now(); renderBanner(); if (optTab === 'updates' && $('.opt')) showOptions('updates');
}

/* ---------- events ---------- */
// the speed in the status bar: a quick speed limit for all downloads
function limitMenu(){
  const r = $('#stLimit').getBoundingClientRect(), cur = DL.config.limitAll || 0;
  showMenu(r.left, r.top, LIMITS.map(l => ({t: limitLabel(l), chk: Math.abs(cur - l * 1048576) < 1, do: () => dlAct({action:'config', config: {...DL.config, limitAll: l * 1048576}}, l ? 'All downloads limited to ' + limitLabel(l) : 'No speed limit')})));
  if (menuEl) { const h = menuEl.getBoundingClientRect().height; menuEl.style.top = Math.max(4, r.top - h - 2) / zoomNow() + 'px'; }
}
document.addEventListener('click', async e => {
  if (e.target.closest('#stLimit')) return limitMenu();
  if (e.target.closest('#emptyExt')) return extConnect();
  if (e.target.closest('#emptyAdd')) return addDialog('');
  const mbb = e.target.closest('#menubar button');
  if (mbb) { if (mbb.classList.contains('open')) { closeMenu(); return; } return openBarMenu(mbb, e.detail === 0); }
  if (menuEl && !menuEl.contains(e.target)) closeMenu();
  const t = e.target.closest('button, th, [data-view]'); if (!t) return;
  if (t.dataset.view) { view = JSON.parse(t.dataset.view); store.set('view', view); sel.clear(); cur = null; return render(); }
  if (t.dataset.sort) { if (t.dataset.resizing) return; sortBy = sortBy.c === t.dataset.sort ? {c: t.dataset.sort, d: -sortBy.d} : {c: t.dataset.sort, d: 1}; store.set('sort', sortBy); return render(); }
  if (t.dataset.opt) return showOptions(t.dataset.opt);
  if (t.dataset.act === 'cancelPower') return dlAct({action:'cancelPower'}, 'Shutdown cancelled');
  if (t.dataset.act === 'updInstall' || t.id === 'updInstall') return updAction({action:'install'}, 'Installing the update…');
  if (t.id === 'updCheck') return updAction({action:'check'}, null);
  if (t.id === 'updAuto') return updAction({action:'settings', auto: !(UPD && UPD.auto)}, null);
  if (t.id === 'updBack') { const r = await askBox({title:'Go back to the previous version?', text:`${APP.name} restarts with the version you had before. Downloads continue afterwards.`, ok:'Go back'}); if (r.ok) updAction({action:'rollback'}, 'Going back…'); return; }
  const tb = t.dataset.tb;
  if (tb) return command(tb);
  // options controls
  if (t.id === 'dlDirSave') return dlAct({action:'config', config: {...DL.config, dir: $('#dlDir').value.trim()}}, 'Folder saved');
  if (t.id === 'dlCatTog') return dlAct({action:'config', config: {...DL.config, useCategories: !DL.config.useCategories}});
  if (t.id === 'dlWinTog') return dlAct({action:'config', config: {...DL.config, window: {...DL.config.window, enabled: !DL.config.window.enabled}}});
  if (t.id === 'dlForget') return dlAct({action:'forgetHosts'}, 'Forgotten');
  if (t.id === 'extCode') return extConnect();
  if (t.id === 'pxSave') { try { await api('app', {proxy: {mode: 'manual', addr: $('#pxAddr').value}}); APPCFG = await api('app'); toast('Proxy saved'); } catch(err){ toast(err.message, true); } return; }
  if (t.id === 'pxTest') { t.disabled = true; try { await api('app', {proxyTest: true}); toast('Works: github.com answered' + (((APPCFG.proxy || {}).mode || 'system') === 'none' ? '' : ' through this setting')); } catch(err){ toast(err.message, true); } t.disabled = false; return; }
  if (t.id === 'doneTog') { try { await api('app', {showDone: !APPCFG.showDone}); APPCFG = await api('app'); } catch(err){ toast(err.message, true); } return showOptions('general'); }
  if (t.id === 'awakeTog') { try { await api('app', {keepAwake: !APPCFG.keepAwake}); APPCFG = await api('app'); toast(APPCFG.keepAwake ? 'The PC stays awake while downloading' : 'The PC may sleep while downloading'); } catch(err){ toast(err.message, true); } return showOptions('schedule'); }
  if (t.id === 'smTog') { try { await api('app', {startMenu: !APPCFG.startMenu}); APPCFG = await api('app'); toast(APPCFG.startMenu ? 'Added to the Start menu' : 'Removed from the Start menu'); } catch(err){ toast(err.message, true); await loadAppCfg(); } return showOptions('general'); }
  if (t.id === 'uninst') {
    const r = await askBox({title:`Uninstall ${APP.name}?`, text:`${APP.name} closes and removes its Start menu entry, its startup entry and the program file. Downloaded files are not touched.`, ok:'Uninstall', danger:true, check:'Also delete the download list, settings and log', checked:false});
    if (!r.ok) return;
    try { await api('app', {uninstall: {data: r.checked}}); } catch(err){ return toast(err.message, true); }
    document.body.innerHTML = `<div class="empty" style="padding-top:20vh">${esc(APP.name)} has been uninstalled. You can close this window.</div>`;
    return;
  }
  if (t.id === 'clipTog') { try { await api('app', {clipboard: !APPCFG.clipboard}); APPCFG = await api('app'); toast(APPCFG.clipboard ? 'Copied links will be offered' : 'Copied links are not offered'); } catch(err){ toast(err.message, true); await loadAppCfg(); } return showOptions('general'); }
  if (t.id === 'autoTog') { try { APPCFG = await api('app', {autostart: !APPCFG.autostart}); toast(APPCFG.autostart ? 'Starts with Windows' : 'Does not start with Windows'); } catch(err){ toast(err.message, true); await loadAppCfg(); } return showOptions('general'); }
  if (t.id === 'extOn') return dlAct({action:'extConfig', extMinMB: DL.ext.minMB, extOff: DL.ext.enabled, extNoAsk: DL.ext.ask === false});
  if (t.dataset.extrm) { const r = await askBox({title:'Disconnect this browser?', text:'Its extension stops sending downloads until you connect it again.', ok:'Disconnect'}); if (r.ok) { await dlAct({action:'extRevoke', id: t.dataset.extrm}, 'Disconnected'); showOptions('browser'); } return; }
});
document.addEventListener('change', async e => {
  const t = e.target; if (!DL) return;
  if (t.id === 'torFile') { const f = t.files && t.files[0]; return addTorrentFile(f); }
  if (t.id === 'optTheme') { store.set('theme', t.value); applyTheme(); return; }
  if (t.id === 'optLang') { if (I18N.choice !== t.value) I18N.set(t.value); return; }
  if (t.id === 'pxMode') {
    $('#pxRow').style.display = t.value === 'manual' ? '' : 'none';
    if (t.value === 'manual') { $('#pxAddr').focus(); return; } // saved with its address
    try { await api('app', {proxy: {mode: t.value}}); APPCFG = await api('app'); toast(t.value === 'none' ? 'No proxy' : 'Using the same proxy as Windows'); } catch(err){ toast(err.message, true); }
    return;
  }
  if (t.id === 'updChannel') return updAction({action:'settings', channel: t.value}, null);
  if (t.id === 'extMin') return dlAct({action:'extConfig', extMinMB: +t.value, extOff: !DL.ext.enabled, extNoAsk: DL.ext.ask === false});
  if (t.id === 'extAsk') return dlAct({action:'extConfig', extMinMB: DL.ext.minMB, extOff: !DL.ext.enabled, extNoAsk: DL.ext.ask !== false});
  if (t.id === 'dlMax') return dlAct({action:'config', config: {...DL.config, maxActive: +t.value}});
  if (t.id === 'dlPer') return dlAct({action:'config', config: {...DL.config, maxPerLine: +t.value}});
  if (t.id === 'dlWinFrom' || t.id === 'dlWinTo') return dlAct({action:'config', config: {...DL.config, window: {...DL.config.window, from: $('#dlWinFrom').value || '22:00', to: $('#dlWinTo').value || '07:00'}}});
  if (t.dataset && t.dataset.cfg) {
    const k = t.dataset.cfg, c = {...DL.config};
    if (k === 'limitAll' || k === 'uploadLimit') c[k] = +t.value * 1048576;
    else if (k === 'afterAll') {
      if (t.value === 'shutdown') { const r = await askBox({title:'Shut the PC down when all downloads are finished?', text:'You get one minute to cancel.', ok:'Shut down when finished'}); if (r.ok) { c.afterAll = t.value; await dlAct({action:'config', config: c}); } return showOptions('schedule'); }
      c.afterAll = t.value;
    }
    else c[k] = t.value;
    return dlAct({action:'config', config: c});
  }
});
document.addEventListener('input', e => { if (e.target.id === 'search') { q = e.target.value; render(); } });

// table: selection, double-click, context menu, column resizing
const list = () => $('#list');
document.addEventListener('mousedown', e => {
  if (e.button !== 0 && e.button !== 2) return;
  const tr = e.target.closest('#tbl tbody tr'); if (!tr) { if (e.target.closest('#list') && !e.target.closest('th') && e.button === 0) { sel.clear(); cur = null; render(); } return; }
  const id = tr.dataset.id;
  if (e.button === 2 && sel.has(id)) { cur = id; return; }
  pick(id, e.shiftKey ? 'range' : (e.ctrlKey || e.metaKey) ? 'toggle' : 'one');
});
document.addEventListener('dblclick', e => {
  const tr = e.target.closest('#tbl tbody tr'); if (!tr) return;
  const v = DL.items.find(x => x.id === tr.dataset.id); if (v) openItem(v);
});
document.addEventListener('contextmenu', e => {
  if (e.target.closest('input, textarea') || e.target.closest('#dlDlg')) return; // the normal copy/paste menu
  e.preventDefault();
  if (e.target.closest('#list')) return e.target.closest('#tbl tbody tr') ? rowMenu(e.clientX, e.clientY) : listMenu(e.clientX, e.clientY);
});
let resize = null;
document.addEventListener('pointerdown', e => {
  const th = e.target.closest('#tbl th'); if (!th) return;
  const r = th.getBoundingClientRect(); if (r.right - e.clientX > 5) return;
  const col = COLS.find(c => c.k === th.dataset.sort); if (!col || !col.w) return;
  e.preventDefault(); resize = {col, x: e.clientX, w: r.width, th}; th.dataset.resizing = '1';
});
document.addEventListener('pointermove', e => {
  const th = e.target.closest && e.target.closest('#tbl th');
  if (th) th.style.cursor = th.getBoundingClientRect().right - e.clientX <= 5 && COLS.find(c => c.k === th.dataset.sort).w ? 'col-resize' : '';
  if (!resize) return;
  widths[resize.col.k] = Math.max(40, Math.min(600, Math.round(resize.w + e.clientX - resize.x)));
  const i = shownCols().indexOf(resize.col); const col = $$('#tbl col')[i]; if (col) col.style.width = widths[resize.col.k] + 'px';
});
document.addEventListener('pointerup', () => { if (!resize) return; store.set('widths', widths); const th = resize.th; resize = null; setTimeout(() => { delete th.dataset.resizing; headKey = ''; }, 0); });

// keyboard
document.addEventListener('keydown', e => {
  if (menuEl) {
    const subm = document.activeElement && document.activeElement.closest('.ctx.subm');
    const box = subm || menuEl;
    if (e.key === 'Escape') {
      e.preventDefault();
      if (subm) { const o = subm.opener; subm.remove(); o.classList.remove('open'); o.focus(); return; }
      const mb = $('#menubar button.open'); closeMenu(); if (mb) mb.focus(); return;
    }
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'Home' || e.key === 'End') {
      e.preventDefault(); const bs = [...box.querySelectorAll(':scope > button:not(:disabled)')]; if (!bs.length) return; const i = bs.indexOf(document.activeElement);
      const n = e.key === 'Home' ? 0 : e.key === 'End' ? bs.length - 1 : (i + (e.key === 'ArrowDown' ? 1 : -1) + bs.length) % bs.length;
      (bs[n] || bs[0]).focus(); return;
    }
    if (e.key === 'ArrowRight' && document.activeElement && document.activeElement.openSub && !subm) { e.preventDefault(); document.activeElement.openSub(); return; }
    if (e.key === 'ArrowLeft' && subm) { e.preventDefault(); const o = subm.opener; subm.remove(); o.classList.remove('open'); o.focus(); return; }
    const mb = $('#menubar button.open');
    if (mb && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) { e.preventDefault(); return openBarMenu(barStep(mb, e.key === 'ArrowRight' ? 1 : -1), true); }
    if (mb && e.key.length === 1 && e.key !== ' ' && !e.ctrlKey && !e.altKey) { // a letter jumps to the first entry starting with it
      const bs = [...box.querySelectorAll(':scope > button:not(:disabled)')], i = bs.indexOf(document.activeElement), ch = e.key.toLowerCase();
      const hit = bs.slice(i + 1).concat(bs.slice(0, i + 1)).find(b => b.textContent.trim().toLowerCase().startsWith(ch));
      if (hit) { e.preventDefault(); hit.focus(); }
      return;
    }
    if (e.altKey && barKey(e)) { e.preventDefault(); return openBarMenu(barKey(e), true); }
    return;
  }
  if (document.activeElement && document.activeElement.closest('#menubar')) {
    const b = document.activeElement.closest('button');
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') { e.preventDefault(); barStep(b, e.key === 'ArrowRight' ? 1 : -1).focus(); return; }
    if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') { e.preventDefault(); return openBarMenu(b, true); }
    if (e.key === 'Escape') { e.preventDefault(); b.blur(); $('#menubar').classList.remove('keys'); return; }
    const hit = BAR_KEYS[e.key.toLowerCase()]; if (hit && !e.ctrlKey) { e.preventDefault(); return openBarMenu($(`#menubar [data-mb="${hit}"]`), true); }
  }
  if (e.altKey && !e.ctrlKey && barKey(e)) { e.preventDefault(); return openBarMenu(barKey(e), true); }
  if (e.key === 'F10' && !e.shiftKey) { e.preventDefault(); $('#menubar').classList.add('keys'); $('#menubar button').focus(); return; }
  if ($('#dlDlg')) return;
  const tag = (e.target.tagName || '').toLowerCase(), typing = tag === 'input' || tag === 'textarea' || tag === 'select';
  const k = e.key, ctrl = e.ctrlKey || e.metaKey;
  if (ctrl && k.toLowerCase() === 'f') { e.preventDefault(); $('#search').focus(); $('#search').select(); return; }
  if ((ctrl && k.toLowerCase() === 'n') || k === 'Insert') { e.preventDefault(); return addDialog(''); }
  if (k === 'F5') { e.preventDefault(); return loadDL(); }
  if (typing) { if (k === 'Escape' && e.target.id === 'search') { e.target.value = ''; q = ''; e.target.blur(); render(); } return; }
  if (ctrl && k.toLowerCase() === 'a') { e.preventDefault(); visible.forEach(id => sel.add(id)); return render(); }
  if (k === 'ArrowDown') { e.preventDefault(); return moveCur(1, e.shiftKey); }
  if (k === 'ArrowUp') { e.preventDefault(); return moveCur(-1, e.shiftKey); }
  if (k === 'Home') { e.preventDefault(); return moveCur(-Infinity, e.shiftKey); }
  if (k === 'End') { e.preventDefault(); return moveCur(Infinity, e.shiftKey); }
  if (k === 'PageDown' || k === 'PageUp') { e.preventDefault(); return moveCur((k === 'PageDown' ? 1 : -1) * Math.max(1, Math.floor(list().clientHeight / 26) - 1), e.shiftKey); }
  if (k === 'Escape') { sel.clear(); cur = null; return render(); }
  if (!sel.size) return;
  if (k === 'Delete') { e.preventDefault(); return doDelete(e.shiftKey); }
  if (k === 'F2') { e.preventDefault(); const s1 = selectedItems(); if (s1.length === 1) renameBox(s1[0]); return; }
  if (k === 'Enter') { e.preventDefault(); const v = DL.items.find(x => x.id === cur) || selectedItems()[0]; if (!v) return; return e.altKey ? dlProps(v.id) : openItem(v); }
  if (k === ' ') { e.preventDefault(); return selectedItems().some(v => v.state === 'downloading' || v.state === 'queued') ? doStop() : doResume(); }
  if (k === 'ContextMenu' || (e.shiftKey && k === 'F10')) {
    e.preventDefault(); const tr = $(`tr[data-id="${CSS.escape(cur || [...sel][0])}"]`); const r = tr ? tr.getBoundingClientRect() : {left: 200, bottom: 100};
    return rowMenu(r.left + 40, r.bottom);
  }
});
let altAlone = false;
document.addEventListener('keydown', e => { altAlone = e.key === 'Alt' && !e.repeat ? true : (e.key === 'Alt' ? altAlone : false); if (e.altKey) $('#menubar').classList.add('keys'); }, true);
document.addEventListener('keyup', e => {
  if (e.key !== 'Alt') return;
  $('#menubar').classList.toggle('keys', false);
  if (!altAlone || $('#dlDlg')) return; altAlone = false; e.preventDefault();
  if (menuEl) return closeMenu();
  const inBar = document.activeElement && document.activeElement.closest('#menubar');
  if (inBar) { document.activeElement.blur(); return; }
  $('#menubar').classList.add('keys'); $('#menubar button').focus();
});
document.addEventListener('mousedown', () => { altAlone = false; }, true);
// Ctrl+V anywhere outside a text box: add the link
document.addEventListener('paste', e => {
  if ($('#dlDlg')) return;
  const tag = (e.target.tagName || '').toLowerCase(); if (tag === 'input' || tag === 'textarea') return;
  const txt = ((e.clipboardData && e.clipboardData.getData('text')) || '').trim();
  if (!txt) return;
  e.preventDefault();
  if (!/^(https?:|ftp:|magnet:)/im.test(txt)) return toast('The clipboard has no link', true);
  addDialog(txt.slice(0, 100000));
});
// drop links or a .torrent file onto the window
window.addEventListener('dragover', e => { if (e.dataTransfer && [...(e.dataTransfer.types || [])].some(t => t === 'text/uri-list' || t === 'text/plain' || t === 'Files')) { e.preventDefault(); list().classList.add('drop'); } });
window.addEventListener('dragleave', e => { if (!e.relatedTarget) list().classList.remove('drop'); });
window.addEventListener('drop', async e => {
  list().classList.remove('drop');
  const dt = e.dataTransfer; if (!dt) return; e.preventDefault();
  if (dt.files && dt.files[0] && /\.torrent$/i.test(dt.files[0].name)) return addTorrentFile(dt.files[0]);
  let text = dt.getData('text/uri-list') || dt.getData('text/plain');
  if (!text && dt.files && dt.files[0] && /\.(txt|lst|csv)$/i.test(dt.files[0].name) && dt.files[0].size < 1e6) text = await dt.files[0].text();
  const links = linksIn(text).filter(s => /^(https?|ftp):\/\/|^magnet:/i.test(s));
  if (links.length) addDialog(links.join('\n'));
});
document.addEventListener('mouseover', e => { const b = e.target.closest('#menubar button'); if (b && menuEl && $('#menubar button.open') && !b.classList.contains('open')) openBarMenu(b, false); });
window.addEventListener('blur', closeMenu);
window.addEventListener('resize', closeMenu);

/* ---------- theme and start ---------- */
// the tray menu and the notifications are drawn by the program: tell it the language shown here
api('app', {lang: I18N.lang === 'xx' ? 'en' : I18N.lang}).catch(() => {});
const siteOf = v => { try { return v.kind === 'torrent' && /^magnet:/i.test(v.url) ? 'torrent' : new URL(v.url).hostname.replace(/^www\./, ''); } catch(e){ return ''; } };
const ZOOMS = [[0.9, 'Small'], [1, 'Normal'], [1.15, 'Large'], [1.3, 'Larger']];
function zoomNow(){ return +store.get('zoom', 1) || 1; }
function applyZoom(){ const z = zoomNow(); document.documentElement.style.zoom = z; document.documentElement.style.setProperty('--zoom', z); } // the layout height is divided by it, so the window still fits
// 'Automatic' follows the browser's language; the others are written in their own language so they can be found
function langChoices(){ return [['auto', 'Automatic']].concat(Object.keys(I18N.names).filter(c => c === 'en' || I18N.shipped.includes(c)).map(c => [c, I18N.names[c]])); }
function applyTheme(){ const t = store.get('theme', 'auto'); if (t === 'auto') delete document.documentElement.dataset.theme; else document.documentElement.dataset.theme = t; }
applyTheme();
// Bahnschrift (Windows 10 and 11) is the font for numbers; it needs a small nudge in the list (see app.css .bahn)
try { const c = document.createElement('canvas').getContext('2d'), t = '0123456789 MB/s', w = f => { c.font = '32px ' + f; return c.measureText(t).width; };
  if (w('"Bahnschrift", monospace') !== w('monospace')) document.documentElement.classList.add('bahn'); } catch (e) {}
applyUiPrefs();
applyZoom();
if (APP.host === 'browser') setTimeout(() => toast(`${APP.name} is shown in your browser. Closing this tab does not stop downloads.`), 800);
$('#search').value = '';
loadDL();
setInterval(() => { if (!document.hidden || Date.now() % 5000 < 1000) loadDL(); }, 1000);
