// Presentation-only components. Download operations stay in app.js / downloads-more.js.
'use strict';
const UI = (() => {
  const paths = {
    Programs:'<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8M12 17v4M7 8l3 3-3 3M13 14h4"/>',
    Archives:'<rect x="4" y="3" width="16" height="18" rx="3"/><path d="M10 3v3h4v3h-4v3h4v3M10 17h4v3h-4z"/>',
    Videos:'<rect x="3" y="4" width="18" height="16" rx="3"/><path d="m10 8 6 4-6 4z"/>',
    Music:'<path d="M9 18V5l11-2v13M9 9l11-2"/><ellipse cx="6" cy="18" rx="3" ry="3"/><ellipse cx="17" cy="16" rx="3" ry="3"/>',
    Documents:'<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9zM14 3v6h6M8 13h8M8 17h5"/>',
    Images:'<rect x="3" y="3" width="18" height="18" rx="3"/><circle cx="8" cy="8" r="1.5"/><path d="m21 15-5-5L5 21"/>',
    Torrents:'<circle cx="12" cy="5" r="2"/><circle cx="5" cy="18" r="2"/><circle cx="19" cy="18" r="2"/><path d="m11 7-5 9m7-9 5 9M7 18h10"/>',
    Other:'<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9zM14 3v6h6"/>',
    browser:'<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M3 9h18M7 6.5h.01M10 6.5h.01M12 12v5m-2-2 2 2 2-2"/>',
    density:'<rect x="4" y="4" width="16" height="6" rx="1.5"/><rect x="4" y="14" width="16" height="6" rx="1.5"/>',
    close:'<path d="m6 6 12 12M6 18 18 6"/>',
    search:'<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 4 4"/>',
    menu:'<path d="M4 6h16M4 12h16M4 18h16"/>',
    arrow:'<path d="M12 4v12m-4-4 4 4 4-4M5 20h14"/>',
  };
  const icon = k => `<svg viewBox="0 0 24 24" aria-hidden="true">${paths[k] || paths.Other}</svg>`;
  const fileBadge = v => `<span class="fi ${esc(v.category || 'Other')}" aria-hidden="true">${icon(v.category)}<span>${esc(extOf(v))}</span></span>`;
  function title(vw, queues){
    return ({all:'All downloads', active:'Downloading', unfinished:'Unfinished', finished:'Finished', errors:'Needs attention'})[vw.k]
      || (vw.k === 'cat' ? vw.v : vw.k === 'queue' ? (queues.find(q => q.id === vw.v)?.name || 'Main queue') : 'All downloads');
  }
  function workspace(){
    const current = title(view, DL.config.queues || []), count = shownItems().length;
    const titleEl = $('#viewTitle');
    if (titleEl.textContent !== current) titleEl.textContent = current;
    titleEl.toggleAttribute('data-nt', view.k === 'queue' && !!view.v);
    $('#viewCount').textContent = count;
    const hint = q.trim() ? I18N.t('Search results for') + ' “' + q.trim() + '”' : ({
      all:'Your files, moving in the right direction.', active:'A little momentum. Everything moving, right here.',
      unfinished:'Pick up where you left off.', finished:'All wrapped up. Your files are ready.',
      errors:'A little help to get things moving again.', cat:'A place for every kind of file.', queue:'Downloads on your schedule.',
    })[view.k] || 'Your files, moving in the right direction.';
    const hintEl = $('#viewHint');
    const translated = q.trim() ? hint : I18N.t(hint);
    if (hintEl.textContent !== translated) hintEl.textContent = translated;
    hintEl.toggleAttribute('data-nt', !!q.trim());
    const active = DL.items.filter(v => v.state === 'downloading').length;
    setHTML($('#transferSummary'), `<span class="transfer-dot${active ? ' running' : ''}" aria-hidden="true"></span><span>${active ? `<b>${active}</b> ${I18N.t('downloading')}` : I18N.t('Ready when you are')}<small data-nt>${active ? bytes(DL.speed || 0) + '/s' : I18N.t('All systems quiet')}</small></span>`);
    $('#densityToggle').setAttribute('aria-pressed', String(!!uiPrefs.dense));
    $('#densityToggle').title = I18N.t(uiPrefs.dense ? 'Comfortable' : 'Compact');
    $('#main').setAttribute('aria-busy', 'false');
    $('#loading').hidden = true;
  }
  function emptyState(){
    const search = !!q.trim(), filtered = DL.items.length > 0;
    const heading = search ? 'No matches this time' : filtered ? 'Nothing here. Yet.' : 'Good things are on the way';
    const description = search ? 'Try a different name or clear your search to see your files.' : filtered
      ? 'Downloads that belong here will appear automatically.' : 'Add your first link. We’ll take it from here.';
    return `<div class="empty-art${search ? ' searching' : ''}" aria-hidden="true"><span class="art-orbit"></span><span class="art-file">${icon(search ? 'search' : 'arrow')}</span><span class="art-spark one"></span><span class="art-spark two"></span></div>
      <h2>${heading}</h2><p>${description}</p><div class="empty-actions">${search
        ? '<button class="btn pri" id="clearSearch">Clear search</button>'
        : '<button class="btn pri" id="emptyAdd">New download</button>' + (filtered ? '<button class="btn" id="showAll">All downloads</button>' : '')}</div>
      ${!filtered && !search ? '<div class="empty-tip"><kbd>Ctrl</kbd> + <kbd>V</kbd> <span>to paste a link, or drop a file here</span></div>' : ''}
      ${!filtered && !search && !(DL.ext?.browsers || []).length ? `<button class="browser-connect" id="emptyExt">${icon('browser')}<span>Connect a browser…</span><span aria-hidden="true">↗</span></button>` : ''}`;
  }
  // Label legacy fields from their visible label/row; no placeholder-only fields.
  function nameFields(card){
    card.querySelectorAll('input,textarea,select').forEach(el => {
      if (el.type === 'hidden' || el.hasAttribute('aria-label') || el.labels?.length) return;
      const row = el.closest('.dnsrow'), label = row?.querySelector('b');
      const names = {addUrl:'Link', rnName:'File name', ibVal:card.querySelector('h3')?.textContent};
      const name = names[el.id] || label?.textContent || el.title || el.placeholder;
      if (name) el.setAttribute('aria-label', name);
    });
    card.querySelectorAll('.tog').forEach(el => {
      el.setAttribute('role', 'switch');
      el.setAttribute('aria-checked', String(el.classList.contains('on')));
      const label = el.closest('.dnsrow')?.querySelector('b');
      if (label && !el.hasAttribute('aria-label')) el.setAttribute('aria-label', label.textContent);
    });
  }
  return {icon, fileBadge, workspace, emptyState, nameFields};
})();
