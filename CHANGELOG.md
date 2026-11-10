# Changelog

## 0.1.0 (not released)

- Repository started: licence, README, design of the window, tray and notifications, and the product name kept in
  one place (`appname.go`) so a rename is cheap.
- The download engine (`dm/`) copied unchanged from NetMerge 3.11.20 (commit `0bfab33`), including the 3.11.20 fix:
  when a server refuses a download and no referer was given, it asks once more with the site's own address as the
  referer.
