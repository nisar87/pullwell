# Changelog

## 0.1.0 (not released)

- Repository started: licence, README, design of the window, tray and notifications, and the product name kept in
  one place (`appname.go`) so a rename is cheap.
- The download engine (`dm/`) copied unchanged from NetMerge 3.11.20 (commit `0bfab33`), including the 3.11.20 fix:
  when a server refuses a download and no referer was given, it asks once more with the site's own address as the
  referer.
- The parts of the window, tray and notification design that need no Windows, each with tests:
  - `pullwell://` links from toast buttons. Only five actions exist; everything except "show" needs a one-time
    random token that Pullwell itself put in the toast, so a web page cannot use the link to open your files; and a
    program file (`.exe`, `.msi`, scripts and similar) is never opened from a link.
  - Notification grouping: downloads that finish together give one notification ("5 downloads finished"), and
    problems are shown separately. New setting levels: all, problems only, off.
  - The saved window position is checked against the monitors that exist, so the window never opens off screen.
