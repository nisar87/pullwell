// Run with: node --test extension/
const test = require('node:test');
const assert = require('node:assert');
const C = require('./src/core.js');
const cfg = { paired: true, enabled: true, localOn: true, minMB: 5, ignore: [] };
const it = (o) => Object.assign({ url: 'https://cdn.example.com/a.zip', fileSize: 50e6, incognito: false }, o);

test('big files are caught, small ones are not', () => {
  assert.equal(C.shouldCatch(it({}), cfg).catch, true);
  assert.equal(C.shouldCatch(it({ fileSize: 1e6 }), cfg).catch, false);
  assert.equal(C.shouldCatch(it({ fileSize: 5 * 1048576 }), cfg).catch, true);
  assert.equal(C.shouldCatch(it({ fileSize: 5 * 1048576 - 1 }), cfg).catch, false);
  assert.equal(C.shouldCatch(it({ fileSize: -1, totalBytes: 80e6 }), cfg).catch, true);
});
test('unknown size: only big file types', () => {
  assert.equal(C.shouldCatch(it({ fileSize: -1, totalBytes: -1 }), cfg).catch, true); // .zip
  assert.equal(C.shouldCatch(it({ url: 'https://x.com/report.pdf', fileSize: -1, totalBytes: -1 }), cfg).catch, false);
  assert.equal(C.shouldCatch(it({ url: 'https://x.com/dl?id=3', filename: 'C:\\Users\\a\\movie.MKV', fileSize: 0, totalBytes: 0 }), cfg).catch, true);
  assert.equal(C.shouldCatch(it({ url: 'https://x.com/dl?id=3', fileSize: 0 }), cfg).catch, false);
});
test('minimum size 0 catches everything', () => {
  assert.equal(C.shouldCatch(it({ fileSize: 10, url: 'https://x.com/a.txt' }), Object.assign({}, cfg, { minMB: 0 })).catch, true);
});
test('a link without a file ending but with an archive content type is taken, a web page is not', () => {
  const u = 'https://testfile.org/files-5GB';
  assert.equal(C.shouldCatch(it({ url: u, fileSize: -1, mime: 'application/octet-stream' }), cfg).catch, true);
  assert.equal(C.shouldCatch(it({ url: u, fileSize: -1, mime: 'application/zip' }), cfg).catch, true);
  assert.equal(C.shouldCatch(it({ url: u, fileSize: -1, mime: 'text/html' }), cfg).catch, false);
  assert.equal(C.shouldCatch(it({ url: u, fileSize: -1 }), cfg).catch, false);
});
test('never touches what the program would refuse or what is private', () => {
  for (const u of ['http://localhost/a.zip', 'http://127.0.0.1/a.zip', 'http://192.168.1.5/a.zip', 'http://10.0.0.2/a.zip', 'http://172.20.1.1/a.zip',
    'http://169.254.1.1/a.zip', 'http://[::1]/a.zip', 'http://nas/a.zip', 'http://printer.local/a.zip', 'http://[fd00::1]/a.zip', 'http://[fe80::1]/a.zip',
    'http://100.64.0.1/a.zip', 'http://[::ffff:127.0.0.1]/a.zip', 'blob:https://x.com/uuid', 'data:text/plain,hi', 'file:///c/a.zip', 'ftp://x.com/a.zip', 'chrome://x'])
    assert.equal(C.shouldCatch(it({ url: u }), cfg).catch, false, u);
  for (const u of ['https://172.32.0.1/a.zip', 'https://8.8.8.8/a.zip', 'https://example.com:8443/a.zip'])
    assert.equal(C.shouldCatch(it({ url: u }), cfg).catch, true, u);
});
test('private windows, other extensions, ignored sites, switches', () => {
  assert.equal(C.shouldCatch(it({ incognito: true }), cfg).catch, false);
  assert.equal(C.shouldCatch(it({ byExtensionId: 'abc' }), cfg).catch, false);
  assert.equal(C.shouldCatch(it({}), Object.assign({}, cfg, { ignore: ['example.com'] })).catch, false);
  assert.equal(C.shouldCatch(it({ url: 'https://notexample.com/a.zip' }), Object.assign({}, cfg, { ignore: ['example.com'] })).catch, true);
  assert.equal(C.shouldCatch(it({}), Object.assign({}, cfg, { enabled: false })).catch, false);
  assert.equal(C.shouldCatch(it({}), Object.assign({}, cfg, { localOn: false })).catch, false);
  assert.equal(C.shouldCatch(it({}), Object.assign({}, cfg, { paired: false })).catch, false);
  assert.equal(C.shouldCatch(it({}), null).catch, false);
  assert.equal(C.shouldCatch({}, cfg).catch, false);
});
test('helpers', () => {
  assert.deepEqual(C.parseIgnore('https://a.com/x, B.org\n c.net;'), ['a.com', 'b.org', 'c.net']);
  assert.equal(C.cookieHeader([{ name: 'a', value: '1' }, { name: 'b', value: '2' }, {}]), 'a=1; b=2');
  assert.equal(C.cleanName('C:\\x\\y\\file name.zip'), 'file name.zip');
  assert.equal(C.cleanName('/home/a/b.iso'), 'b.iso');
  assert.equal(C.cleanName(''), '');
  assert.equal(C.cleanName('bad<name>.zip'), '');
});

test('media sniffer: playlists and files yes, stream parts and small things no', () => {
  assert.equal(C.mediaKind('https://v.example.com/a/master.m3u8?x=1', 'application/vnd.apple.mpegurl', -1), 'stream');
  assert.equal(C.mediaKind('https://v.example.com/a/manifest.mpd', 'application/dash+xml', 1000), 'stream');
  assert.equal(C.mediaKind('https://v.example.com/a/movie.mp4', 'video/mp4', 50e6), 'video');
  assert.equal(C.mediaKind('https://v.example.com/a/song', 'audio/mpeg', 5e6), 'audio');
  assert.equal(C.mediaKind('https://v.example.com/a/seg-1.ts', 'video/mp2t', 900000), '');
  assert.equal(C.mediaKind('https://v.example.com/a/seg-1.m4s', 'video/mp4', 900000), '');
  assert.equal(C.mediaKind('https://v.example.com/a/click.mp3', 'audio/mpeg', 30000), '');
  assert.equal(C.mediaKind('https://v.example.com/a/page.html', 'text/html', 90000), '');
  assert.equal(C.mediaKind('http://192.168.1.5/a/movie.mp4', 'video/mp4', 50e6), '');
});

test('file types on the list are always sent', () => {
  const c2 = Object.assign({}, cfg, { types: C.parseTypes('PDF, .epub; x y!') });
  assert.deepEqual(C.parseTypes('PDF, .epub; x y!'), ['pdf', 'epub', 'x']);
  assert.equal(C.shouldCatch(it({ url: 'https://x.com/book.pdf', fileSize: 1000 }), c2).catch, true);
  assert.equal(C.shouldCatch(it({ url: 'https://x.com/book.txt', fileSize: 1000 }), c2).catch, false);
  assert.equal(C.shouldCatch(it({ url: 'http://192.168.0.2/book.pdf', fileSize: 1000 }), c2).catch, false);
});
