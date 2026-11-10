// The downloads page. APP (name, header, version) is set by index.html.
// It uses the helpers of the main page ($, api, esc, bytes, toast, setHTML, go ...).
/* ---------- downloads ---------- */
let DL = null, dlFilter = 'all', dlCat = '', dlQ = '', dlBusy = false, dlPrev = {};
const DL_CATS = ['Programs','Archives','Videos','Music','Documents','Images','Torrents','Other'];
const DL_RISKY = /\.(exe|msi|bat|cmd|ps1|vbs|js|jar|scr|com|lnk|msix|appx|reg|hta)$/i;
async function loadDL(force){
  if (dlBusy && !force) return;
  try { DL = await api('downloads'); } catch(e){ return; }
  for (const v of DL.items) { if (dlPrev[v.id] && dlPrev[v.id] !== 'done' && v.state === 'done') toast('Finished: ' + v.name); dlPrev[v.id] = v.state; }
  paintDL();
}
async function dlAct(body, okMsg){
  try { const r = await api('downloads', body); if (r && r.items) DL = r; await loadDL(true); if (okMsg) toast(okMsg); return r; }
  catch(e){ toast(e.message || 'Failed', true); return null; }
}
const dlStateText = v => v.state === 'downloading' ? 'Downloading' : v.state === 'queued' ? (v.retryAt > Date.now()/1000 ? 'Trying again soon' : 'Waiting') : v.state === 'paused' ? 'Paused' : v.state === 'done' ? 'Finished' : 'Needs attention';
function dlLineShare(v){
  const tot = {}; (v.bytes ? Object.entries(v.bytes) : []).forEach(([k,b]) => tot[k] = (tot[k]||0) + b);
  (v.lineStats||[]).forEach(l => tot[l.name] = (tot[l.name]||0) + l.bytes);
  const sum = Object.values(tot).reduce((a,b) => a+b, 0);
  return {tot, sum};
}
const dlSel = new Set(); let dlVisIds = [];
const dlSpeedHist = []; let dlSpeedAt = 0;
function dlSpark(){
  if (dlSpeedHist.length < 3) return '';
  const mx = Math.max(...dlSpeedHist, 1), w = 120, h = 26, n = dlSpeedHist.length;
  const pts = dlSpeedHist.map((v,i) => `${(i/(n-1)*w).toFixed(1)},${(h-1-v/mx*(h-3)).toFixed(1)}`).join(' ');
  return `<svg width="${w}" height="${h}" viewBox="0 0 ${w} ${h}" role="img" aria-label="Speed over the last minute" style="vertical-align:middle;margin-left:8px"><polyline points="${pts}" fill="none" stroke="var(--good,#2a9d5c)" stroke-width="1.6" stroke-linejoin="round"/></svg><span class="hint2"> peak ${bytes(mx)}/s</span>`;
}
function dlBulkBar(){
  if (!dlSel.size) return '';
  return `<div class="note" style="margin-bottom:10px;display:flex;gap:8px;flex-wrap:wrap;align-items:center"><b>${dlSel.size} selected</b><button class="btn" data-bulk="pause">Pause</button><button class="btn" data-bulk="resume">Resume</button><button class="btn" data-bulk="remove">Remove</button><select class="sel" id="dlBulkQ" style="min-width:0" aria-label="Move to queue"><option value="\u0000">Move to queue…</option>${dlQueueOpts('')}</select><span style="flex:1"></span><button class="btn" data-bulk="all">Select all shown</button><button class="btn" data-bulk="none">Clear selection</button></div>`;
}
function dlRow(v){
  const pct = v.size > 0 ? Math.min(100, v.done / v.size * 100) : (v.state === 'done' ? 100 : 0);
  const sh = dlLineShare(v);
  const strip = sh.sum > 0 && Object.keys(sh.tot).length > 1 ? `<div class="dlstrip" title="How much each connection carried">${Object.entries(sh.tot).filter(([,b]) => b>0).map(([k,b]) => `<i style="width:${b/sh.sum*100}%;background:${colorOf(k)}" title="${esc(labelOf(k))}: ${Math.round(b/sh.sum*100)}%"></i>`).join('')}</div>` : '';
  let meta = [];
  if (v.state === 'downloading') {
    meta.push(`${bytes(v.done)}${v.size>0?' of '+bytes(v.size):''}`, `<b>${bytes(v.speed)}/s</b>`);
    if (v.kind === 'torrent') meta.push(`${v.peers||0} peer${v.peers===1?'':'s'}` + (v.seeds ? `, ${v.seeds} seed${v.seeds===1?'':'s'}` : '') + (v.swarmSeeds > (v.seeds||0) ? ` (${v.swarmSeeds} in the swarm)` : ''));
    if (v.kind === 'torrent' && v.up > 0) meta.push('↑ ' + bytes(v.up));
    if (v.size > 0 && v.speed > 0) meta.push(dur(Math.ceil((v.size - v.done)/v.speed)) + ' left');
    const act = (v.lineStats||[]).filter(l => l.status === 'active');
    if (act.length) meta.push(act.map(l => `<span style="color:${colorOf(l.name)}">●</span> ${esc(labelOf(l.name))} ${bytes(l.speed)}/s`).join(' &nbsp; '));
  } else if (v.state === 'done') {
    meta.push(`<span class="good">Finished</span> · ${bytes(v.size)}`);
    if (v.verify === 'ok') meta.push('<span class="good">✓ Checksum matches</span>');
    else if (v.verify === 'bad') meta.push('<span class="bad"><b>✗ Checksum does NOT match.</b> Do not open this file: it is damaged or not what you expected.</span>');
    else if (v.verify === 'checking') meta.push('Checking the checksum…');
    if (sh.sum > 0 && Object.keys(sh.tot).length > 1) meta.push(Object.entries(sh.tot).filter(([,b]) => b>0).map(([k,b]) => `${esc(labelOf(k))} ${Math.round(b/sh.sum*100)}%`).join(' · '));
  } else if (v.state === 'error') meta.push(`<span class="bad" title="${esc(v.detail||'')}">${esc(v.err || 'Something went wrong')}</span>`);
  else { meta.push(v.note && v.state === 'queued' ? esc(v.note) : dlStateText(v)); if (v.size > 0) meta.push(`${bytes(v.done)} of ${bytes(v.size)}`); if (v.err) meta.push(`<span title="${esc(v.detail||'')}">${esc(v.err)}</span>`); }
  if (v.note && v.state === 'downloading') meta.push(esc(v.note));
  if (v.queue && typeof dlQueueName === 'function') meta.push(`<span title="Queue">▤ ${esc(dlQueueName(v.queue))}</span>`);
  const btn = (a, t, extra) => `<button class="btn" data-dl="${a}" data-id="${esc(v.id)}"${extra||''}>${t}</button>`;
  const mv = v.state === 'queued' || v.state === 'paused' ? btn('move','▲',' data-mv="up" title="Move up" aria-label="Move up"') + btn('move','▼',' data-mv="down" title="Move down" aria-label="Move down"') + btn('move','First',' data-mv="top" title="Download this first"') : '';
  const LIMS = [[0,'No limit'],[0.5,'0.5 MB/s'],[1,'1 MB/s'],[2,'2 MB/s'],[5,'5 MB/s'],[10,'10 MB/s'],[20,'20 MB/s']];
  const cur = LIMS.reduce((b, l) => Math.abs(l[0]*1048576 - (v.limit||0)) < Math.abs(b[0]*1048576 - (v.limit||0)) ? l : b, LIMS[0]);
  const lim = v.state === 'done' ? '' : `<select class="sel" data-lim="${esc(v.id)}" aria-label="Speed limit for this download" style="min-width:0;padding:5px 8px;font-size:12.5px">${LIMS.map(l => `<option value="${l[0]}"${l[0]===cur[0]?' selected':''}>${l[1]}</option>`).join('')}</select>`;
  const props = `<button class="btn" data-dm="props" data-id="${esc(v.id)}" title="Details, log, mirrors, queue, change link">Properties</button>`;
  const acts = (v.state === 'downloading' || (v.state === 'queued') ? mv + lim + btn('pause','Pause') + btn('remove','Cancel')
    : v.state === 'paused' ? mv + lim + btn('resume','Resume') + btn('remove','Cancel')
    : v.state === 'error' ? btn('retry','Try again') + btn('remove','Remove')
    : btn('open','Open') + btn('folder','Show in folder') + btn('remove','Remove from list') + btn('delfile','Delete file')) + props;
  return `<div class="dlrow${dlSel.has(v.id)?' picked':''}"><input type="checkbox" class="dlck" data-id="${esc(v.id)}"${dlSel.has(v.id)?' checked':''} aria-label="Select ${esc(v.name || 'download')}" style="align-self:center;margin-right:8px"><div class="dlic" aria-hidden="true">${esc((v.category||'Other').slice(0,3).toUpperCase())}</div>
    <div style="min-width:0"><b class="dlname" title="${esc(v.url)}">${esc(v.name || v.url)}</b>
      <div class="dlprog ${v.state==='paused'?'paused':v.state==='error'?'err':''}"><div style="width:${pct}%"></div></div>${v.state==='downloading' && typeof dlSegBar==='function' ? dlSegBar(v) : ''}${strip}
      <div class="dlmeta">${meta.join(' · ')}</div></div>
    <div class="dlact">${acts}</div></div>`;
}
const LIM_OPTS = [0,0.5,1,2,5,10,20,50];
function limSel(key, cur){
  const near = LIM_OPTS.reduce((b, x) => Math.abs(x*1048576 - cur) < Math.abs(b*1048576 - cur) ? x : b, 0);
  return `<select class="sel" data-cfg="${esc(key)}" data-lim2="1" style="min-width:0" aria-label="Speed limit">${LIM_OPTS.map(x => `<option value="${x}"${x===near?' selected':''}>${x ? x + ' MB/s' : 'No limit'}</option>`).join('')}</select>`;
}
function paintDL(){
  const box = $('#dlBody'); if (!box || !DL) return;
  if (box.contains(document.activeElement) && document.activeElement.tagName === 'INPUT' && document.activeElement.id !== 'dlSearch') { /* typing in settings: keep */ }
  const items = DL.items, cfg = DL.config;
  const act = items.filter(v => v.state === 'downloading').length, que = items.filter(v => v.state === 'queued').length;
  const fin = items.filter(v => v.state === 'done').length, bad = items.filter(v => v.state === 'error').length;
  const q = dlQ.toLowerCase();
  const vis = items.filter(v => (dlFilter === 'all' || (dlFilter === 'active' && (v.state === 'downloading' || v.state === 'queued' || v.state === 'paused')) || (dlFilter === 'done' && v.state === 'done') || (dlFilter === 'bad' && v.state === 'error'))
    && (!dlCat || v.category === dlCat) && dlQueueMatch(v) && (!q || (v.name||'').toLowerCase().includes(q) || v.url.toLowerCase().includes(q)));
  dlSortItems(vis);
  dlVisIds = vis.map(v => v.id);
  for (const id of [...dlSel]) if (!items.some(v => v.id === id)) dlSel.delete(id);
  if (Date.now() - dlSpeedAt >= 1000) { dlSpeedAt = Date.now(); dlSpeedHist.push(DL.speed || 0); if (dlSpeedHist.length > 60) dlSpeedHist.shift(); }
  const chip = (id, t, n) => `<button data-f="${id}" class="${dlFilter===id?'sel':''}">${t}${n!==undefined?' '+n:''}</button>`;
  const num = (id, vals, cur) => `<select class="sel" id="${id}" style="min-width:0">${vals.map(x => `<option${x==cur?' selected':''}>${x}</option>`).join('')}</select>`;
  const list = vis.length ? vis.map(dlRow).join('') : `<div class="dlempty">${items.length ? 'Nothing matches this filter.' : '<b>No downloads yet.</b><br>Paste a link above (http, https, ftp or a magnet link), or add a .torrent file. ' + esc(APP.name) + ' downloads it with several connections at once.'}</div>`;
  const holdBanner = DL.hold ? `<div class="note warn" style="margin-bottom:12px"><b>Downloads are waiting.</b> ${esc(DL.hold.replace(/^Waiting: /,''))}.</div>` : '';
  const powerBanner = DL.powerAt && (Date.now()/1000 - DL.powerAt) < 70 ? `<div class="note bad" style="margin-bottom:12px"><span style="flex:1"><b>The PC is about to shut down</b> because your downloads finished.</span><button class="btn" data-dl="cancelPower">Cancel shutdown</button></div>` : '';
  const noLines = DL.lines.length === 0 ? '<div class="note warn" style="margin-bottom:12px"><b>No internet connection found.</b> Downloads will start when a connection is available.</div>' : '';
  const html = `${powerBanner}${holdBanner}${noLines}
  <div class="card"><div class="dlbox"><textarea class="inp" id="dlUrl" rows="1" placeholder="Paste a link or magnet link here (one per line to add several)" aria-label="Download link"></textarea>
    <button class="btn pri" id="dlAddBtn">Add</button><button class="btn" id="dlPaste" title="Paste the link you copied">Paste</button><button class="btn" id="dlTorBtn" title="Add a torrent from a .torrent file on this computer">Torrent file…</button><input type="file" id="dlTorFile" accept=".torrent,application/x-bittorrent" hidden></div>
    <div class="hint2" style="margin-top:8px">${DL.speed > 0 ? `Downloading at <b>${bytes(DL.speed)}/s</b> in total.${dlSpark()} ` : ''}You can also drag a link onto this window.</div>${dlToolsRow()}</div>
  <div class="dlbar2"><div class="chips">${chip('all','All',items.length)}${chip('active','In progress',act+que+items.filter(v=>v.state==='paused').length)}${chip('done','Finished',fin)}${chip('bad','Needs attention',bad)}</div>
    <select class="sel" id="dlCat" style="min-width:0" aria-label="Type"><option value="">All types</option>${DL_CATS.map(c => `<option${c===dlCat?' selected':''}>${c}</option>`).join('')}</select>
    <input class="inp" id="dlSearch" placeholder="Search" value="${esc(dlQ)}" style="min-width:140px;width:180px" aria-label="Search downloads">${dlSortSelect()}
    <div class="spacer" style="flex:1"></div>
    <button class="btn" data-dl="pauseAll">Pause all</button><button class="btn" data-dl="resumeAll">Resume all</button><button class="btn" data-dl="clear"${fin?'':' disabled'}>Clear finished</button></div>
  ${dlBulkBar()}<div class="card" style="padding:6px 16px">${list}</div>
  <details class="card" style="margin-top:16px"${dlBusy?' open':''} id="dlSet"><summary style="cursor:pointer;font-weight:650">Download settings</summary>
    <div style="margin-top:14px">
    <div class="dnsrow"><div><b>Save downloads in</b><div class="d">The main folder. New downloads go into a folder for their type (Videos, Programs, ...) when that is on.</div></div><div class="row" style="gap:8px"><input class="inp" id="dlDir" value="${esc(cfg.dir)}" style="min-width:260px" aria-label="Download folder"><button class="btn" id="dlDirSave">Save</button></div></div>
    <div class="dnsrow"><div><b>Sort into folders by type</b><div class="d">Videos, Programs, Documents and so on. Turn off to keep everything in one folder.</div></div><button class="tog${cfg.useCategories?' on':''}" id="dlCatTog" role="switch" aria-checked="${cfg.useCategories}" aria-label="Sort into folders"></button></div>
    <div class="dnsrow"><div><b>Downloads at the same time</b><div class="d">More than 3 can slow each one down. Recommended: 3.</div></div>${num('dlMax',[1,2,3,4,5,6,8,10],cfg.maxActive)}</div>
    <div class="dnsrow"><div><b>Connections per download</b><div class="d">How many parallel connections one file uses. Some websites refuse many; ${esc(APP.name)} lowers it by itself. Recommended: 8.</div></div>${num('dlPer',[2,4,6,8,12,16],cfg.maxPerLine)}</div>
    <div class="dnsrow"><div><b>Speed limit for all downloads</b><div class="d">Keeps some speed free for everything else. "No limit" uses everything the connection can give.</div></div>${limSel('limitAll', cfg.limitAll)}</div>
    ${DL.lines.length > 1 ? DL.lines.map(l => `<div class="dnsrow"><div><b>${esc(labelOf(l.name))}: use for downloads</b><div class="d">Turn off to keep this line free of downloads. You can also limit its speed:</div></div><div class="row" style="gap:8px">${limSel('line:' + l.name, (cfg.limitLines||{})[l.name]||0)}<button class="tog${(cfg.excludeLines||[]).includes(l.name)?'':' on'}" data-ex="${esc(l.name)}" role="switch" aria-checked="${!(cfg.excludeLines||[]).includes(l.name)}" aria-label="Use ${esc(labelOf(l.name))} for downloads"></button></div></div>`).join('') : ''}
    <div class="dnsrow"><div><b>Only download at certain hours</b><div class="d">For example at night, when nobody needs the internet. Downloads outside these hours wait.</div></div><div class="row" style="gap:8px"><input type="time" class="inp" id="dlWinFrom" value="${esc(cfg.window.from)}" style="min-width:0" aria-label="From"><input type="time" class="inp" id="dlWinTo" value="${esc(cfg.window.to)}" style="min-width:0" aria-label="To"><button class="tog${cfg.window.enabled?' on':''}" id="dlWinTog" role="switch" aria-checked="${cfg.window.enabled}" aria-label="Download hours"></button></div></div>
    <div class="dnsrow"><div><b>When everything is finished</b><div class="d">Do this once, after the last download, then switch back to "Nothing". A shutdown gives you one minute to cancel.</div></div><select class="sel" data-cfg="afterAll" style="min-width:210px" aria-label="When everything is finished">${[['','Nothing'],['sleep','Put the PC to sleep'],['shutdown','Shut the PC down']].map(o => `<option value="${o[0]}"${(cfg.afterAll||'')===o[0]?' selected':''}>${o[1]}</option>`).join('')}</select></div>
    ${dlMoreSettings(cfg)}
    <div class="dnsrow"><div><b>What ${esc(APP.name)} learned about websites</b><div class="d">Some websites only accept a file from one address. ${esc(APP.name)} remembers that for a few days so it does not waste time. ${DL.knownSites ? DL.knownSites + ' site' + (DL.knownSites===1?'':'s') + ' remembered.' : 'Nothing remembered.'}</div></div><button class="btn" id="dlForget"${DL.knownSites?'':' disabled'}>Forget</button></div>
    <div class="dnsrow"><div><b>Browser extension</b><div class="d">Hands big downloads from Chrome, Edge, Brave and Firefox to ${esc(APP.name)}. It comes in a later version.</div></div></div>
    ${(APP.ext && DL.ext) ? `<div class="dnsrow"><div><b>Send downloads from the browser</b><div class="d">Turn off to let browsers download by themselves again.</div></div><button class="tog${DL.ext.enabled?' on':''}" id="dlExtOn" role="switch" aria-checked="${DL.ext.enabled}"><span></span></button></div>
    <div class="dnsrow"><div><b>Only files bigger than</b><div class="d">Smaller files are quicker to leave with the browser.</div></div><select class="sel" id="dlExtMin" style="min-width:140px">${[0,1,5,10,25,50,100].map(n => `<option value="${n}"${DL.ext.minMB===n?' selected':''}>${n ? n + ' MB' : 'Everything'}</option>`).join('')}</select></div>
    ${(DL.ext.browsers||[]).map(b => `<div class="dnsrow"><div><b>${esc(b.name)}</b><div class="d">Connected ${new Date(b.created*1000).toLocaleDateString()}${b.lastSeen ? ', last used ' + new Date(b.lastSeen*1000).toLocaleString() : ''}</div></div><button class="btn" data-extrm="${esc(b.id)}">Disconnect</button></div>`).join('')}` : ''}
    </div></details>`;
  const ta = $('#dlUrl'), keep = ta ? ta.value : '', hadFocus = document.activeElement && document.activeElement.id;
  const search = document.activeElement && document.activeElement.id === 'dlSearch' ? [document.activeElement.selectionStart, document.activeElement.selectionEnd] : null;
  const open = $('#dlSet') ? $('#dlSet').open : false;
  const dirTyped = $('#dlDir') && document.activeElement && document.activeElement.id === 'dlDir' ? $('#dlDir').value : null;
  if (box.dataset.h !== html) {
    box.dataset.h = html; box.innerHTML = html;
    if ($('#dlUrl') && keep) $('#dlUrl').value = keep;
    if (open) $('#dlSet').open = true;
    if (dirTyped !== null) $('#dlDir').value = dirTyped;
    if (hadFocus && $('#' + hadFocus)) { $('#' + hadFocus).focus(); if (search && hadFocus === 'dlSearch') $('#dlSearch').setSelectionRange(search[0], search[1]); }
  }
}
let dlOpener = null;
function dlClose(){
  const d = $('#dlDlg'); if (d) d.remove();
  document.querySelectorAll('[data-dlinert]').forEach(el => { el.inert = false; el.removeAttribute('data-dlinert'); });
  if (dlOpener && document.contains(dlOpener)) { try { dlOpener.focus(); } catch(e){} }
  dlOpener = null;
}
function dlDialog(html){
  let d = $('#dlDlg');
  if (!d) {
    dlOpener = document.activeElement;
    d = document.createElement('div'); d.id = 'dlDlg'; d.className = 'wiz'; document.body.appendChild(d);
    // the page behind a dialog must not take focus or clicks from the keyboard
    [...document.body.children].forEach(el => { if (el !== d && el.tagName !== 'SCRIPT' && !el.inert && !/toast/i.test(el.id + el.className)) { el.inert = true; el.setAttribute('data-dlinert', ''); } });
  }
  setHTML(d, html);
  const card = d.querySelector('.card'); if (card) { card.setAttribute('tabindex', '-1'); const f = card.querySelector('input:not([type=hidden]),select,textarea,button'); (f || card).focus(); }
}
document.addEventListener('keydown', e => {
  const d = $('#dlDlg'); if (!d) return;
  if (e.key === 'Escape') { e.preventDefault(); dlClose(); return; }
  if (e.key !== 'Tab') return;
  const f = [...d.querySelectorAll('input:not([type=hidden]):not([disabled]),select:not([disabled]),textarea:not([disabled]),button:not([disabled]),a[href],[tabindex]:not([tabindex="-1"])')].filter(x => x.offsetParent !== null);
  if (!f.length) return;
  const first = f[0], last = f[f.length - 1];
  if (e.shiftKey && (document.activeElement === first || !d.contains(document.activeElement))) { e.preventDefault(); last.focus(); }
  else if (!e.shiftKey && (document.activeElement === last || !d.contains(document.activeElement))) { e.preventDefault(); first.focus(); }
}, true);
async function dlExtCode(){
  let r; try { r = await api('downloads', {action:'extCode'}); } catch(e){ return toast(e.message, true); }
  const base = r.repo ? `https://github.com/${r.repo}/releases/latest/download/` : '';
  const dl = (file, label) => base ? `<a class="btn pri" href="${esc(base + file)}" target="_blank" rel="noopener">${label}</a>` : '';
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Connect a browser" style="max-width:560px;max-height:92vh;overflow:auto"><h3 style="margin:0">Connect a browser</h3>
    <div class="hint2" style="margin:6px 0 12px">The extension hands big downloads from your browser to ${esc(APP.name)}. It is not in the browser stores yet, so you add it by hand (about a minute).</div>
    <ol class="hint2" style="margin:0 0 12px;padding-left:20px;display:grid;grid-template-columns:minmax(0,1fr);gap:10px">
      <li><div style="display:block"><b>Download the extension file</b><div class="row" style="gap:8px;margin-top:6px;flex-wrap:wrap">${dl(APP.name + '-extension-chrome-edge.zip','Chrome / Edge / Brave') + dl(APP.name + '-extension-firefox.zip','Firefox') || 'Get <code>' + esc(APP.name) + '-extension-chrome-edge.zip</code> (or the Firefox one) from the latest ' + esc(APP.name) + ' release page.'}</div></div></li>
      <li><div style="display:block"><b>Chrome, Edge, Brave:</b> unzip it into a folder you will keep. Open the address <code>chrome://extensions</code> (Edge: <code>edge://extensions</code>) <button class="btn" id="dlCopyExt" style="padding:2px 8px;margin-left:4px">Copy address</button>, switch on <b>Developer mode</b>, click <b>Load unpacked</b> and choose that folder.<br><b>Firefox:</b> open <code>about:debugging#/runtime/this-firefox</code>, click <b>Load Temporary Add-on</b> and choose the zip file (Firefox forgets it when it closes).</div></li>
      <li><div style="display:block"><b>Pin the ${esc(APP.name)} icon</b> (puzzle-piece menu in the toolbar), click it and type this code:</div></li>
    </ol>
    <div style="font:700 30px/1.2 ui-monospace,Consolas,monospace;letter-spacing:.12em;text-align:center;padding:14px;border-radius:10px;background:var(--bg2,rgba(127,127,127,.12))" id="dlExtCodeBox">${esc(r.code)}</div>
    <div class="hint2" style="margin-top:8px;text-align:center" id="dlExtLeft"></div>
    <div class="hint2" style="margin-top:10px">After that, downloads of 5 MB or more are taken over by ${esc(APP.name)}. Right-click any link and choose <b>Download with ${esc(APP.name)}</b> for the rest. If ${esc(APP.name)} is not running, the browser downloads as usual.</div>
    <div class="row" style="justify-content:flex-end;gap:8px;margin-top:16px"><button class="btn pri" id="dlCancel">Done</button></div></div>`);
  const cp = $('#dlCopyExt'); if (cp) cp.onclick = async () => { try { await navigator.clipboard.writeText('chrome://extensions'); toast('Copied. Paste it in a new browser tab.'); } catch(e){ toast('Type chrome://extensions in a new tab', true); } };
  $('#dlCancel').onclick = dlClose;
  const tick = () => { const el = $('#dlExtLeft'); if (!el) return; const s = r.expires - Math.floor(Date.now()/1000); if (s <= 0) { el.textContent = 'The code has expired. Close this and click Connect a browser again.'; return; } el.textContent = 'Valid for ' + Math.floor(s/60) + ':' + String(s%60).padStart(2,'0') + ', one use only.'; setTimeout(tick, 1000); };
  tick();
}
async function dlSubmit(){
  const ta = $('#dlUrl'); const links = ta.value.split(/\s*\n\s*/).map(s => s.trim()).filter(Boolean);
  if (!links.length) return toast('Paste a link first', true);
  if (links.length === 1 && /^magnet:|\.torrent(\?|#|$)/i.test(links[0])) { const r = await dlAct({action:'add', url: links[0]}, 'Torrent added'); if (r) ta.value = ''; return; }
  if (links.length > 1) { const r = await dlAct({action:'add', urls: links}); if (r) { ta.value = ''; toast(`${(r.added||[]).length} downloads added` + (r.skipped ? `, ${r.skipped} already in your list` : '')); } return; }
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Add download"><h3 style="margin:0">Checking the link…</h3><div class="hint2" style="margin-top:6px">${esc(links[0])}</div></div>`);
  let p; try { p = await api('downloads', {action:'probe', url: links[0]}); } catch(e){ dlClose(); return toast(e.message, true); }
  dlShowProbe(links[0], p);
}
function dlShowProbe(link, p){
  const info = p.info || {};
  const fine = p.ok;
  const hls = fine && p.hls;
  const can = hls ? p.lines > 1 : fine && info.ranges && info.size > 0 && p.lines > 1 && !p.locked;
  const audioNote = hls && p.hls.audio ? (p.mux ? ' The sound is a separate track and is joined to the picture with ffmpeg.' : ' The sound is a separate track. ffmpeg was not found on this PC, so you get two files (picture and sound).') : '';
  const why = hls ? (p.lines > 1 ? `The video comes in ${p.hls.segments} parts. They are downloaded over all ${p.lines} connections and joined into one file.` : `The video comes in ${p.hls.segments} parts and is downloaded over your one connection.`) : !fine ? '' : !info.ranges ? 'This website does not allow splitting the file, so one connection is used.' : info.size <= 0 ? 'The size is not known, so one connection is used.' : p.lines < 2 ? 'Only one connection is available right now.' : p.locked ? 'This website only accepts one address per file, so one line is used.' : `The file can be split over all ${p.lines} connections.`;
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Add download" style="max-width:560px"><h3 style="margin:0">Add this download</h3>
    ${fine ? '' : `<div class="note bad" style="margin:12px 0">${esc(p.error || 'The link could not be checked.')} You can still add it.</div>`}
    ${info.warning === 'html' ? '<div class="note warn" style="margin:12px 0"><b>This looks like a web page, not a file.</b> The site may want you to sign in first. Download the link from your browser instead if it fails.</div>' : ''}
    <div style="margin:12px 0;display:grid;gap:10px"><label class="hint2">Name<input class="inp" id="dlName" value="${esc(info.name || '')}" style="width:100%;margin-top:4px"></label>
    <label class="hint2">Save in<input class="inp" id="dlFolder" value="${esc(p.dir || '')}" style="width:100%;margin-top:4px"></label>
    <details class="hint2"><summary style="cursor:pointer">More options</summary><div style="display:grid;gap:10px;margin-top:10px">
      <label>Start at (leave empty to start now)<input type="time" class="inp" id="dlTime" style="width:100%;margin-top:4px"></label>
      <label>Checksum, to check the file is not damaged (SHA-256, SHA-1 or MD5 from the website)<input class="inp" id="dlHash" placeholder="Optional" style="width:100%;margin-top:4px"></label>
      ${(p.lineNames||[]).length > 1 ? `<div>Use these connections<div style="display:flex;gap:14px;flex-wrap:wrap;margin-top:6px">${p.lineNames.map(n => `<label style="display:flex;gap:6px;align-items:center"><input type="checkbox" class="dlLn" value="${esc(n)}" checked> ${esc(labelOf(n))}</label>`).join('')}</div></div>` : ''}
    </div></details></div>
    ${p.duplicate ? '<div class="note warn" style="margin:0 0 10px"><b>This link is already in your list.</b> Adding it again makes a second copy.</div>' : ''}
    ${fine ? `<div class="hint2" style="margin-bottom:6px">${hls ? '<b>Video stream</b> · about ' + (p.hls.seconds < 90 ? Math.round(p.hls.seconds) + ' sec' : Math.round(p.hls.seconds/60) + ' min') + (p.hls.quality ? ' · ' + esc(p.hls.quality) : '') + ' · ' : (info.size > 0 ? '<b>' + bytes(info.size) + '</b> · ' : '')}${hls ? '' : esc(info.type || 'unknown type')}</div>${hls && (p.hls.heights||[]).length > 1 ? `<label class="hint2" style="display:block;margin:6px 0">Picture quality <select class="sel" id="dlQual" style="min-width:0;margin-left:6px"><option value="0"${!p.height?' selected':''}>Best (${p.hls.heights[0]}p)</option>${p.hls.heights.slice(1).map(h => `<option value="${h}"${p.height===h?' selected':''}>${h}p</option>`).join('')}</select></label>` : ''}<div class="hint2" style="${can?'color:var(--good)':''}">${esc(why + audioNote)}</div>` : ''}
    <div class="row" style="justify-content:flex-end;gap:8px;margin-top:16px"><button class="btn" id="dlCancel">Cancel</button><button class="btn" id="dlLater">Add, start later</button><button class="btn pri" id="dlGo">Download now</button></div></div>`);
  if ($('#dlQual')) $('#dlQual').onchange = async e => { const h = +e.target.value; try { const q = await api('downloads', {action:'probe', url: link, height: h}); q.height = h; dlShowProbe(link, q); } catch(err){ toast(err.message, true); } };
  $('#dlGo').onclick = () => dlConfirm(link, p, true);
  $('#dlLater').onclick = () => dlConfirm(link, p, false);
  $('#dlCancel').onclick = dlClose;
  setTimeout(() => $('#dlGo') && $('#dlGo').focus(), 30);
}
async function dlConfirm(link, p, start){
  const name = $('#dlName').value.trim(), folder = $('#dlFolder').value.trim();
  const body = {action:'add', url: link, start};
  if (name && name !== (p.info||{}).name) body.name = name; else if (name) body.name = name;
  if (folder && folder !== p.dir) body.dir = folder;
  if (p.duplicate) body.force = true;
  if (p.height) body.height = p.height;
  const tm = $('#dlTime') && $('#dlTime').value;
  if (tm) { const [h, mi] = tm.split(':').map(Number); const d = new Date(); d.setHours(h, mi, 0, 0); if (d.getTime() <= Date.now()) d.setDate(d.getDate() + 1); body.startAt = Math.floor(d.getTime()/1000); }
  const hs = $('#dlHash') && $('#dlHash').value.trim(); if (hs) body.hash = hs;
  const boxes = $$('.dlLn'); if (boxes.length) { const on = boxes.filter(b => b.checked).map(b => b.value); if (!on.length) return toast('Choose at least one connection', true); if (on.length < boxes.length) body.lines = on; }
  const r = await dlAct(body, start ? 'Download started' : 'Added');
  if (r) { dlClose(); const ta = $('#dlUrl'); if (ta) ta.value = ''; }
}
document.addEventListener('keydown', e => { if (e.key === 'Escape' && $('#dlDlg')) dlClose(); if (e.key === 'Enter' && !e.shiftKey && e.target.id === 'dlUrl') { e.preventDefault(); dlSubmit(); } });
document.addEventListener('change', async e => {
  const t = e.target;
  if (t.classList && t.classList.contains('dlck')) { t.checked ? dlSel.add(t.dataset.id) : dlSel.delete(t.dataset.id); return paintDL(); }
  if (t.id === 'dlBulkQ' && t.value !== '\u0000') { const q = t.value, ids = [...dlSel]; for (const id of ids) await dlAct({action:'setQueue', id, queue: q}); toast(`${ids.length} moved`); }
});
async function dlBulk(a){
  if (a === 'all') { dlVisIds.forEach(id => dlSel.add(id)); return paintDL(); }
  if (a === 'none') { dlSel.clear(); return paintDL(); }
  const ids = [...dlSel]; if (!ids.length) return;
  if (a === 'remove') {
    const loose = ids.filter(id => { const v = DL.items.find(x => x.id === id); return v && v.state !== 'done' && v.state !== 'error' && v.done > 0; }).length;
    if (!confirm(`Remove ${ids.length} download${ids.length===1?'':'s'} from the list?` + (loose ? `\n\n${loose} of them are not finished: what was downloaded so far will be deleted. Finished files stay on your disk.` : '\n\nFinished files stay on your disk.'))) return;
  }
  let bad = 0;
  for (const id of ids) { const r = await dlAct({action: a, id}); if (r === null) bad++; }
  dlSel.clear(); paintDL();
  toast(bad ? `${ids.length - bad} done, ${bad} could not be changed` : `${ids.length} ${a === 'remove' ? 'removed' : a === 'pause' ? 'paused' : 'resumed'}`, !!bad);
}
document.addEventListener('keydown', e => {
  if (typeof page === 'undefined' || page !== 'downloads' || $('#dlDlg')) return;
  const tag = (e.target.tagName || '').toLowerCase(); if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'a') { e.preventDefault(); dlVisIds.forEach(id => dlSel.add(id)); paintDL(); }
  else if (e.key === 'Delete' && dlSel.size) { e.preventDefault(); dlBulk('remove'); }
  else if (e.key === 'Escape' && dlSel.size) { dlSel.clear(); paintDL(); }
});
document.addEventListener('paste', e => {
  if (typeof page === 'undefined' || page !== 'downloads' || $('#dlDlg')) return;
  const tag = (e.target.tagName || '').toLowerCase(); if (tag === 'input' || tag === 'textarea') return;
  const txt = ((e.clipboardData && e.clipboardData.getData('text')) || '').trim();
  if (!/^(https?:|ftp:|magnet:)/i.test(txt) || txt.length > 20000) return;
  e.preventDefault(); const ta = $('#dlUrl'); if (ta) { ta.value = txt; ta.focus(); toast('Link pasted: press Add'); }
});
document.addEventListener('click', async e => {
  const t = e.target.closest('button'); if (!t) return;
  if (t.dataset.bulk) return dlBulk(t.dataset.bulk);
  if (t.id === 'dlAddBtn') return dlSubmit();
  if (t.id === 'dlTorBtn') { const f = $('#dlTorFile'); if (f) f.click(); return; }
  if (t.id === 'dlPaste') { try { const s = await navigator.clipboard.readText(); if (s) $('#dlUrl').value = s.trim(); } catch(err){ toast('Press Ctrl+V in the box instead', true); } return; }
  if (t.dataset.f && page === 'downloads') { dlFilter = t.dataset.f; return paintDL(); }
  if (t.id === 'dlDirSave') return void dlAct({action:'config', config: {...DL.config, dir: $('#dlDir').value.trim()}}, 'Folder saved');
  if (t.id === 'dlCatTog') return void dlAct({action:'config', config: {...DL.config, useCategories: !DL.config.useCategories}});
  if (t.id === 'dlWinTog') return void dlAct({action:'config', config: {...DL.config, window: {...DL.config.window, enabled: !DL.config.window.enabled}}});
  if (t.dataset.ex !== undefined) { const ex = new Set(DL.config.excludeLines||[]); ex.has(t.dataset.ex) ? ex.delete(t.dataset.ex) : ex.add(t.dataset.ex); return void dlAct({action:'config', config: {...DL.config, excludeLines: [...ex]}}); }
  if (t.id === 'dlForget') return void dlAct({action:'forgetHosts'}, 'Forgotten');
  if (t.id === 'dlExtCode') return dlExtCode();
  if (t.id === 'dlExtOn') return void dlAct({action:'extConfig', extMinMB: DL.ext.minMB, extOff: DL.ext.enabled});
  if (t.dataset.extrm) { if (!confirm('Disconnect this browser? Its extension stops sending downloads until you connect it again.')) return; return void dlAct({action:'extRevoke', id: t.dataset.extrm}, 'Disconnected'); }
  if (t.id === 'dlMobTog') return void dlAct({action:'config', config: {...DL.config, allowMobile: !DL.config.allowMobile}});
  const a = t.dataset.dl; if (!a) return;
  const id = t.dataset.id, v = DL && DL.items.find(x => x.id === id);
  if (a === 'delfile') { if (!confirm('Delete "' + (v ? v.name : 'this file') + '" from your computer too? This cannot be undone.')) return; return void dlAct({action:'remove', id, deleteFiles:true}, 'File deleted'); }
  if (a === 'open' && v && DL_RISKY.test(v.name) && !confirm('"' + v.name + '" is a program. Only open it if you trust where it came from. Open it?')) return;
  if (a === 'remove' && v && v.state !== 'done' && v.state !== 'error' && v.done > 0 && !confirm('Cancel this download? What was downloaded so far will be deleted.')) return;
  if (a === 'move') return void dlAct({action:'move', id, move: t.dataset.mv});
  if (a === 'cancelPower') return void dlAct({action:'cancelPower'}, 'Shutdown cancelled');
  dlAct({action: a, id});
});
async function dlAddTorrentFile(f){
  if (!f) return;
  if (f.size > 16e6) return toast('That file is too big to be a torrent', true);
  const buf = new Uint8Array(await f.arrayBuffer()); let bin = ''; for (let i = 0; i < buf.length; i += 32768) bin += String.fromCharCode.apply(null, buf.subarray(i, i + 32768));
  const torrent = btoa(bin);
  let p; try { p = await api('downloads', {action:'torrentInfo', torrent}); } catch(e){ return toast(e.message, true); }
  const fl = p.files || [];
  dlDialog(`<div class="card" role="dialog" aria-modal="true" aria-label="Add torrent" style="max-width:640px;max-height:92vh;overflow:auto"><h3 style="margin:0;word-break:break-all">${esc(p.name)}</h3>
    <div class="hint2" style="margin:4px 0 10px">${bytes(p.size)} · ${fl.length} file${fl.length===1?'':'s'}${p.private ? ' · private torrent (only its own tracker is used)' : ''}</div>
    ${p.duplicate ? '<div class="note warn" style="margin:0 0 10px"><b>This torrent is already in your list.</b></div>' : ''}
    ${fl.length > 1 ? `<div class="row" style="gap:10px;margin-bottom:6px"><label style="display:flex;gap:6px;align-items:center"><input type="checkbox" id="tpAll" checked> All files</label><span class="hint2" id="tpSum"></span></div>` : ''}
    <div class="scrollbox" style="max-height:260px;overflow:auto;font-size:13px">${fl.map((x,i) => `<label style="display:flex;gap:8px;align-items:center;padding:3px 0"><input type="checkbox" class="tpSel" data-i="${i}" checked> <span style="flex:1;word-break:break-all">${esc(x.name)}</span><span class="hint2">${bytes(x.size)}</span></label>`).join('')}</div>
    <label class="hint2" style="display:block;margin-top:10px">Save in<input class="inp" id="tpDir" value="${esc(p.dir)}" style="width:100%;margin-top:4px"></label>
    <div class="row" style="justify-content:flex-end;gap:8px;margin-top:16px"><button class="btn" id="dlCancel">Cancel</button><button class="btn" id="tpLater">Add, start later</button><button class="btn pri" id="tpGo">Download now</button></div></div>`);
  const sum = () => { const on = $$('.tpSel').filter(b => b.checked); const sz = on.reduce((a,b) => a + fl[+b.dataset.i].size, 0); if ($('#tpSum')) $('#tpSum').textContent = `${on.length} chosen · ${bytes(sz)}`; if ($('#tpAll')) $('#tpAll').checked = on.length === fl.length; };
  sum();
  $$('.tpSel').forEach(b => b.onchange = sum);
  if ($('#tpAll')) $('#tpAll').onchange = e => { $$('.tpSel').forEach(b => b.checked = e.target.checked); sum(); };
  const go = async start => {
    const sel = $$('.tpSel').map(b => b.checked); if (!sel.some(Boolean)) return toast('Choose at least one file', true);
    const body = {action:'add', torrent, start, force: !!p.duplicate};
    if (!sel.every(Boolean)) body.select = sel;
    const dir = $('#tpDir').value.trim(); if (dir && dir !== p.dir) body.dir = dir;
    const r = await dlAct(body, start ? 'Torrent added' : 'Added'); if (r) dlClose();
  };
  $('#tpGo').onclick = () => go(true); $('#tpLater').onclick = () => go(false); $('#dlCancel').onclick = dlClose;
}
document.addEventListener('change', e => {
  if (!DL) return;
  const t = e.target;
  if (t.id === 'dlTorFile') { const f = t.files && t.files[0]; t.value = ''; return void dlAddTorrentFile(f); }
  if (t.dataset && t.dataset.lim) return void dlAct({action:'limit', id: t.dataset.lim, limitBytes: +t.value * 1048576});
  if (t.dataset && t.dataset.cfg) {
    const k = t.dataset.cfg, c = {...DL.config};
    if (k === 'limitAll') c.limitAll = +t.value * 1048576;
    else if (k === 'uploadLimit') c.uploadLimit = +t.value * 1048576;
    else if (k.startsWith('line:')) { c.limitLines = {...(c.limitLines||{})}; const n = k.slice(5); if (+t.value) c.limitLines[n] = +t.value * 1048576; else delete c.limitLines[n]; }
    else if (k === 'afterAll') { if (t.value === 'shutdown' && !confirm('Shut the PC down when all downloads are finished? You get one minute to cancel.')) { paintDL(); return; } c.afterAll = t.value; }
    else c[k] = t.value;
    return void dlAct({action:'config', config: c});
  }
  if (t.id === 'dlWinFrom' || t.id === 'dlWinTo') return void dlAct({action:'config', config: {...DL.config, window: {...DL.config.window, from: $('#dlWinFrom').value || '22:00', to: $('#dlWinTo').value || '07:00'}}});
  if (t.id === 'dlExtMin') return void dlAct({action:'extConfig', extMinMB: +t.value, extOff: !DL.ext.enabled});
  if (e.target.id === 'dlCat') { dlCat = e.target.value; paintDL(); }
  if (e.target.id === 'dlMax') dlAct({action:'config', config: {...DL.config, maxActive: +e.target.value}});
  if (e.target.id === 'dlPer') dlAct({action:'config', config: {...DL.config, maxPerLine: +e.target.value}});
});
document.addEventListener('input', e => { if (e.target.id === 'dlSearch') { dlQ = e.target.value; paintDL(); } });
window.addEventListener('dragover', e => { if (e.dataTransfer && [...(e.dataTransfer.types||[])].some(t => t === 'text/uri-list' || t === 'text/plain' || t === 'Files')) e.preventDefault(); });
window.addEventListener('drop', async e => {
  const dt = e.dataTransfer; if (!dt) return; e.preventDefault();
  let text = dt.getData('text/uri-list') || dt.getData('text/plain');
  if (dt.files && dt.files[0] && /\.torrent$/i.test(dt.files[0].name)) { go('downloads'); await loadDL(true); return void dlAddTorrentFile(dt.files[0]); }
  if (!text && dt.files && dt.files[0] && /\.(txt|csv|lst|m3u8?)$/i.test(dt.files[0].name) && dt.files[0].size < 1e6) text = await dt.files[0].text();
  const links = (text||'').split(/\s*\n\s*/).map(s => s.trim()).filter(s => /^(https?|ftp):\/\/|^magnet:/i.test(s));
  if (!links.length) return;
  go('downloads'); await loadDL(true); const ta = $('#dlUrl'); if (ta) ta.value = links.join('\n');
});
setInterval(() => { if (page === 'downloads' && !document.hidden && !$('#dlDlg')) loadDL(false); }, 1000);
function renderDlCard(){
  const c = $('#dlCard'), d = S && S.downloads; if (!c) return;
  if (!d || (!d.active && !d.queued)) { c.hidden = true; return; }
  c.hidden = false;
  setHTML(c, `<div class="row" style="gap:12px"><div style="flex:1;min-width:0"><h3 style="margin:0 0 6px">Downloading now <span class="hint2" style="font-weight:400">${bytes(d.speed)}/s${d.queued ? ' · ' + d.queued + ' waiting' : ''}</span></h3>
    ${(d.items||[]).map(i => `<div class="hint2" style="margin-top:3px"><b style="color:var(--text)">${esc(i.name)}</b> · ${i.size>0?Math.round(i.done/i.size*100)+'%':bytes(i.done)} · ${bytes(i.speed)}/s</div>`).join('')}</div>
    <button class="btn" id="dlOpen">Open Downloads</button></div>`);
  $('#dlOpen').onclick = () => go('downloads');
}

