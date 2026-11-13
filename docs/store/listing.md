# Chrome Web Store listing: Pullwell Downloads

**Name:** Pullwell Downloads
**Category:** Productivity (Tools)
**Language:** English

**Summary (132 characters at most):**
Send big downloads and videos to the Pullwell download manager: several connections, resume, and no lost files.

**Description:**
Pullwell Downloads connects Chrome to the Pullwell download manager for Windows (a separate program you install first).

• Big downloads go to Pullwell, which splits them into parts, downloads them with several connections at once and picks up where it stopped after an interruption or restart.
• Like IDM, Pullwell asks first: you see the file name, folder and size, and press Start download. A wrong click does not start anything.
• A small "Download with Pullwell" button appears over videos, for direct video links and streams (HLS).
• Right-click any link, image, video or sound and choose "Download with Pullwell".
• Small files stay in the browser. You choose the size limit, the sites to leave alone and file types to always send.
• If Pullwell is not running, the browser downloads as usual.

Set up once: install Pullwell, open Options → Browser → "Connect a browser", and type the code in the extension. Your downloads stay on your computer: the extension sends nothing to any server (the Pullwell program can send anonymous problem reports, which you can turn off; see the privacy policy).

**Privacy policy URL:** https://github.com/nisar87/pullwell/blob/main/docs/privacy.md
**Support / homepage URL:** https://github.com/nisar87/pullwell

**Single purpose:** Hand the downloads you start in the browser (and videos on web pages) over to the Pullwell download manager running on your own computer.

**Permission justifications**
- downloads: notices a download the browser starts so it can be handed to Pullwell and cancelled in the browser.
- storage: keeps the extension's settings and the pairing token.
- contextMenus: the "Download with Pullwell" right-click entry.
- alarms: a once-a-minute check that Pullwell is running.
- cookies: passes the sign-in cookie of a download link to Pullwell so downloads that need a sign-in work.
- webRequest: sees the video and stream addresses a page loads (for the video list and the video button). Nothing is stored or sent anywhere except to Pullwell on this computer.
- scripting: adds the video button to pages, only after the person chooses "Allow for all sites".
- Host permission 127.0.0.1 (ports 9197, 9297, 9397, 9497): talks to the Pullwell program on the same computer.
- Optional access to all sites: requested only when the person clicks "Allow for all sites"; needed to read cookies for any download link, to see video requests and to show the button on any site.

**Remote code:** No. All code is inside the package.
**Data use certification:** the extension handles web history-like data (download and video addresses) and authentication information (cookies for the download link). It is used only for the single purpose, sent only to the program on the same computer, never sold or used for unrelated purposes.

**Assets** (this folder): screenshot-1-connect.png, screenshot-2-video-button.png, screenshot-3-confirm.png (1280x800), promo-small-440x280.png, marquee-promo-1400x560.png, store-icon-128x128.png (the store icon: 96 px artwork inside a 16 px transparent margin). Rebuild the upload package with scripts/store/build.sh; the pictures with scripts/store/shots.mjs and scripts/store/tiles.mjs.
