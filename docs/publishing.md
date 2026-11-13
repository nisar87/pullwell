# Publishing: the steps only the owner can take

Pullwell works without any of these. Each one removes a hurdle for people installing it, but every one of them
needs the owner's own account, and some cost money. Nothing here has been started.

## 1. Browser extension in the stores (biggest gain)

Today the extension is added by hand, with Developer mode switched on. Chrome then warns about it now and then, and
Firefox forgets a temporary add-on when it closes. From a store, the extension installs with one click and updates
by itself.

| Store | Account | Cost | Upload |
| --- | --- | --- | --- |
| Chrome Web Store | developer account at chrome.google.com/webstore/devconsole | one-time USD 5 | `extension/dist/Pullwell-extension-chrome-edge.zip` |
| Microsoft Edge Add-ons | Partner Center (partner.microsoft.com), free | free | the same zip |
| Firefox Add-ons (AMO) | addons.mozilla.org account | free | `extension/dist/Pullwell-extension-firefox.zip` |

The zips are made by `python3 extension/build.py`. The stores ask for:

- a short and a long description (take them from the README)
- the privacy practice: no data collected by the extension. It talks only to Pullwell on 127.0.0.1 and to the site of a
  download, which it passes on. (The Pullwell program's own problem reports are described in the privacy policy.)
- why it needs its permissions: downloads, cookies for the download's site, context menu, storage, alarms (a check
  once a minute that Pullwell is running), and webRequest
  for "Videos on this page"
- at least one screenshot (`docs/screenshots/`)

Once a store version exists, **Connect a browser** should link to it instead of saving a zip. That is a small change
in `ui/app.js`, `extConnect()`.

## 2. winget (`winget install Pullwell`)

1. Fork `microsoft/winget-pkgs`.
2. Add `manifests/n/nisar87/Pullwell/<version>/` with the three files that `wingetcreate new` produces. Run it
   against the release's `Pullwell.exe` URL, with installer type `portable` and command `pullwell`.
3. Open a pull request.

Microsoft checks the file automatically. Later versions can be sent with `wingetcreate update`. Pullwell also
updates itself, so winget matters only for the first install.

## 3. Code signing (no SmartScreen warning)

An unsigned program gets "Windows protected your PC" until enough people have run it. A code-signing certificate
removes that sooner:

- an OV certificate costs roughly USD 200–400 a year
- Azure Trusted Signing costs about USD 10 a month for individuals in supported countries

Signing would be one more step in `.github/workflows/release.yml`, with the certificate kept as a repository secret.
`verifyProgram` in `update.go` could then also check the signature before an update is installed.

## 4. Name

"Pullwell" is the final name (decided by the owner on 2026-11-13). No formal trademark search has been made, only a
quick web search that found no obvious conflict. That is not legal clearance: a search of the registers (USPTO,
EUIPO, UK, Pakistan; classes 9 and 42) is still the owner's to do, ideally before a store listing. The name lives
only in `appname.go`, so a rename stays cheap until the stores and winget carry it.
