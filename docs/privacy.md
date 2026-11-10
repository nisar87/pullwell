# Pullwell Downloads: privacy policy

Last updated: 2026-11-11

Pullwell Downloads is a browser extension that hands downloads to the Pullwell program running on **your own
computer**. It has no server of its own and no account.

**What the extension handles**
- The address of a download you start in the browser, the page it came from, the cookies for that address (so a
  download that needs you to be signed in still works) and your browser's name and version.
- For videos: the address of a video or stream the page loads, and where a video sits on the page, so the "Download
  with Pullwell" button can be placed over it.

**Where it goes**
Only to the Pullwell program on your own computer, over the local address `127.0.0.1`. Nothing is sent to the
developer, to Anthropic, or to any other server. There is no analytics, no advertising and no tracking, and no data is
sold or shared.

**What is stored**
Inside your browser, the extension keeps its own settings (on/off, minimum size, sites to ignore, a pairing token for
your Pullwell program). Pullwell keeps the downloads list on your computer. Private (incognito) windows are not
touched.

**Permissions and why**
- *downloads*: notice a download the browser starts, so it can be handed to Pullwell instead.
- *storage*: remember the settings and the pairing token.
- *contextMenus*: the "Download with Pullwell" right-click entry.
- *alarms*: check once a minute that Pullwell is running.
- *cookies*: pass the sign-in cookie of a download link to Pullwell, so protected downloads work.
- *webRequest*: see the video and stream addresses a page loads, for the video list and the video button.
- *scripting*: add the video button to pages (only after you choose "Allow for all sites").
- *Access to all sites* (optional, asked only when you click "Allow for all sites"): needed to read cookies for any
  download link, to see video requests and to show the video button.
- *Access to 127.0.0.1*: to talk to the Pullwell program on your computer.

**Contact**
Open an issue at https://github.com/nisar87/pullwell/issues
