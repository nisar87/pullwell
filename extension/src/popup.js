const api = globalThis.browser || globalThis.chrome;
const $ = (id) => document.getElementById(id);
const ask = (m) => new Promise((res) => api.runtime.sendMessage(m, (r) => res(r || {})));
function show(id) { for (const v of document.querySelectorAll('.v')) v.hidden = v.id !== id; }
function fmt(b) { return b > 1048576 ? (b / 1048576).toFixed(1) + ' MB/s' : Math.round(b / 1024) + ' KB/s'; }

async function paint() {
  const ping = await ask({ type: 'ping' });
  if (ping.status === 404) return show('v-off'); // the program runs, but its download manager is switched off
  if (!ping.ok) return show('v-down');
  const s = await api.storage.local.get(['token', 'localOn', 'ignore', 'types', 'videoBtn']);
  if (!s.token) return show('v-pair');
  const st = await ask({ type: 'refresh' });
  if (!st.ok) return show('v-pair'); // the token was refused: the program forgot this browser
  show('v-on');
  const d = st.data;
  $('stat').textContent = d.active ? d.active + ' downloading · ' + fmt(d.speed) : 'Connected. Nothing downloading now.';
  $('vbtn').checked = s.videoBtn !== false;
  $('on').checked = s.localOn !== false && d.enabled !== false;
  $('on').disabled = d.enabled === false;
  $('min').textContent = d.enabled === false ? 'Switched off in {{NAME}}.' : 'Files of ' + d.minMB + ' MB and more are sent (change this in {{NAME}}, Options).';
  $('ign').value = (s.ignore || []).join('\n');
  $('typ').value = (s.types || []).join(', ');
  try { const has = await api.permissions.contains({ origins: ['<all_urls>'] }); $('perm').hidden = has; } catch (e) { $('perm').hidden = true; }
  paintMedia();
}

async function paintMedia() {
  try {
    const tabs = await api.tabs.query({ active: true, currentWindow: true });
    if (!tabs[0]) return;
    const r = await ask({ type: 'media', tab: tabs[0].id });
    const box = $('mlist');
    box.textContent = '';
    for (const m of (r.list || []).slice(0, 8)) {
      const row = document.createElement('div'); row.className = 'row';
      const name = decodeURIComponent(m.url.split('?')[0].split('/').pop() || m.url).slice(0, 40);
      const label = document.createElement('span'); label.className = 'm'; label.style.flex = '1'; label.style.overflow = 'hidden';
      label.setAttribute('data-nt', '');
      label.textContent = I18N.t(m.kind === 'stream' ? 'Stream' : m.kind === 'audio' ? 'Sound' : 'Video') + ' · ' + (m.size > 0 ? (m.size / 1048576).toFixed(1) + ' MB · ' : '') + name;
      const b = document.createElement('button'); b.textContent = 'Download';
      b.onclick = async () => { b.disabled = true; const x = await ask({ type: 'sendMedia', url: m.url, referrer: tabs[0].url || '' }); b.textContent = x.ok ? (x.duplicate ? 'Already there' : 'Added') : 'Failed'; };
      row.appendChild(label); row.appendChild(b); box.appendChild(row);
    }
    $('media').hidden = !(r.list || []).length;
  } catch (e) { $('media').hidden = true; }
}
$('retry').onclick = paint;
$('retry2').onclick = paint;
$('pair').onclick = async () => {
  if ($('pair').disabled) return;
  $('perr').textContent = '';
  $('pair').disabled = true; $('pair').setAttribute('aria-busy', 'true');
  try {
    const r = await ask({ type: 'pair', code: $('code').value, name: /Edg\//.test(navigator.userAgent) ? 'Edge' : /Firefox\//.test(navigator.userAgent) ? 'Firefox' : 'Chrome' });
    if (r.ok) await paint(); else { $('perr').textContent = r.error || 'Could not connect'; $('code').focus(); }
  } finally { $('pair').disabled = false; $('pair').removeAttribute('aria-busy'); }
};
$('code').onkeydown = (e) => { if (e.key === 'Enter') $('pair').click(); };
$('vbtn').onchange = () => api.storage.local.set({ videoBtn: $('vbtn').checked });
$('on').onchange = () => api.storage.local.set({ localOn: $('on').checked });
$('ign').onchange = async () => {
  const list = $('ign').value.split(/[\s,;]+/).map((s) => s.replace(/^https?:\/\//, '').replace(/\/.*$/, '').trim().toLowerCase()).filter(Boolean).slice(0, 200);
  await api.storage.local.set({ ignore: list });
};
$('typ').onchange = async () => { await api.storage.local.set({ types: NMCore.parseTypes($('typ').value) }); };
$('grant').onclick = async () => { try { await api.permissions.request({ origins: ['<all_urls>'] }); } catch (e) { } paint(); };
$('unpair').onclick = async () => { await ask({ type: 'unpair' }); paint(); };
paint();
