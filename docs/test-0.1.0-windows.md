# Windows test for the first build (0.1.0)

For the owner or Codex, on the Windows Sandbox or a PC. About 15 minutes. Report each line as **PASS**, **FAIL**
(what happened) or **BLOCKED** (why it could not be tried). If something fails, send `data\pullwell.log` from the
program's folder.

Setup: make a new folder (for example `Desktop\Pullwell`), put `Pullwell.exe` in it and nothing else.

| # | Do this | Expected |
| --- | --- | --- |
| 1 | Double-click `Pullwell.exe`. If Windows says "Windows protected your PC", choose More info, then Run anyway (the program is not signed yet). | A window titled Pullwell opens with the Downloads page. A round icon with a down arrow appears in the notification area (it may be under the ^ arrow). A `data` folder appears next to the exe. |
| 2 | Paste `https://go.dev/dl/go1.27.3.windows-amd64.msi` (the Go installer, 63 MB; it redirects to Google's download server) into the box and press Add. | The download starts, shows speed and progress, and finishes. A notification "Download finished" appears near the clock. |
| 3 | Hover over the tray icon while a download runs (add another link if the first one is done). | The tooltip says "Pullwell - 1 downloading, ... MB/s". The icon is green while downloading, grey when idle. |
| 4 | On a finished download, press **Show in folder**. | Explorer opens **in front**, with the file selected. |
| 5 | Press **Open** on a finished download. | The file opens in its normal program (or Windows asks which program to use). |
| 6 | Close the Pullwell window (X) while a download runs. | The window closes, the tray icon stays, the download continues (tooltip). |
| 7 | Click the tray icon. | The window opens again and shows the running download. |
| 8 | With the window open behind another program, double-click `Pullwell.exe` again. | No second tray icon. The existing window comes to the front. |
| 9 | Right-click the tray icon, choose **Pause all**, then **Resume all**. | Downloads pause, then continue. |
| 10 | During a download, right-click the tray icon and choose **Quit Pullwell**. Then start `Pullwell.exe` again. | The icon disappears and the program ends. After the restart the download continues from where it was, not from 0. |
| 11 | Check the file from step 2. In PowerShell: `Get-FileHash <path to go1.27.3.windows-amd64.msi>`. | The hash is `89EC8325F667C674FE1B94A5A88E26932AA12323589881927FF54DA99D4B7FAA` (as published on go.dev/dl on 2026-11-10). |
| 12 | In the Downloads settings, change **Save downloads in** to another folder, Save, download something. | The file lands in the new folder. |

Known for this build (not bugs): the window is an Edge or Chrome "app" window, not Pullwell's own window yet;
notifications are the small tray kind, without buttons; there is no installer, no "Start with Windows" and no
browser extension yet.
