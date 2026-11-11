// Lists the English texts in the window's source files, for translators and for the coverage check.
// Usage: node scripts/i18n_extract.mjs [file ...]   (prints a JSON array)
import fs from 'node:fs';
const files = process.argv.length > 2 ? process.argv.slice(2) : ['ui/index.html', 'ui/app.js', 'ui/downloads-more.js'];
const out = new Set();

// the string literals of a script, with every ${...} replaced by \u0001
function literals(src) {
  const lits = []; let i = 0; const n = src.length;
  function tmpl() { // after the opening backtick; returns the text
    let s = '';
    while (i < n) {
      const c = src[i];
      if (c === '\\') { s += src[i + 1] === 'n' ? ' ' : src[i + 1]; i += 2; continue; }
      if (c === '`') { i++; return s; }
      if (c === '$' && src[i + 1] === '{') { i += 2; s += '\u0001'; code('}'); continue; }
      s += c; i++;
    }
    return s;
  }
  function code(end) { // inside code until the matching end brace
    let depth = 0;
    while (i < n) {
      const c = src[i];
      if (c === '/' && src[i + 1] === '/') { while (i < n && src[i] !== '\n') i++; continue; }
      if (c === '/' && src[i + 1] === '*') { i = src.indexOf('*/', i + 2) + 2; continue; }
      if (c === '`') { i++; lits.push(tmpl()); continue; }
      if (c === "'" || c === '"') {
        let s = ''; i++;
        while (i < n && src[i] !== c && src[i] !== '\n') { if (src[i] === '\\') { s += src[i + 1] === 'n' ? ' ' : src[i + 1]; i += 2; } else s += src[i++]; }
        i++; lits.push(s); continue;
      }
      if (c === '{') depth++;
      if (c === '}') { if (end === '}' && depth === 0) { i++; return; } depth--; }
      i++;
    }
  }
  code(null);
  return lits;
}
function texts(s) { // the readable pieces inside one literal
  const res = [];
  if (s.includes('<')) {
    for (const m of s.matchAll(/(?:title|placeholder|aria-label|alt)="([^"]*)"/g)) res.push(m[1]);
    for (const m of s.matchAll(/>([^<>]+)(?=<|$)/g)) res.push(m[1]);
    const first = s.match(/^([^<]+)</); if (first) res.push(first[1]);
  } else res.push(s);
  return res;
}
function clean(t) {
  let k = 0; t = t.replace(/\u0001/g, () => '{' + (k++) + '}').replace(/&middot;/g, '·').replace(/&amp;/g, '&').replace(/&nbsp;/g, ' ').replace(/&hellip;/g, '…').replace(/\s+/g, ' ').trim();
  return t;
}
function worth(t) {
  if (!/[A-Za-z]{3}/.test(t.replace(/\{\d+\}/g, ''))) return false;
  if (/^[.#\[][\w\-\[\]="'.:> #{}]*$/.test(t)) return false;                  // css selectors
  if (/^[\w.\-/:{}]+$/.test(t) && !/[A-Z][a-z]+$/.test(t)) return false;      // identifiers, paths, mime
  if (/^(https?|ftp|magnet):|^[\w-]+\(|=>|;\s*$|^\s*[{}]|[{;]\s*[a-z-]+:\s/.test(t)) return false;
  if (/^[a-z]+(-[a-z]+)+$/.test(t)) return false;
  if (/\b(var|const|let|function|return)\b|\.length|\.map\(/.test(t)) return false;
  if (/^[a-z][a-zA-Z]*$/.test(t)) return false;                              // a lone lowercase word is a key
  return true;
}
for (const f of files) {
  const src = fs.readFileSync(f, 'utf8');
  const lits = f.endsWith('.html') ? [src] : literals(src);
  for (const l of lits) for (const t of texts(l)) { const c = clean(t); if (worth(c)) out.add(c); }
  if (f.endsWith('.html')) for (const m of src.matchAll(/<u>(\w)<\/u>(\w*)/g)) out.add(clean(m[0].replace(/<\/?u>/g, '')));
}
console.log(JSON.stringify([...out].sort(), null, 0));
