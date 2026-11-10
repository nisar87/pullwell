# Problems found in the download code that also affect NetMerge

Written for the owner in plain words. Pullwell's copy is fixed; NetMerge's is not changed from here.

## 1. The file name you type is ignored when the website suggests its own (2026-11-10)

**What happens.** In the "Download file info" dialog you can change the file name before starting. If the website
sends its own file name (most download sites do, with a `Content-Disposition` header), the download is saved under
the website's name and your name is thrown away. Nothing tells you.

**Why.** In `dm/dl_manager.go`, `pickPath` takes the website's name unless `nameFixed()` says the name was chosen,
and `nameFixed()` always returns `false`.

**Fix in Pullwell.** A download added with a name remembers that (`Named`, saved with the list), and `nameFixed()`
returns it. Without a typed name nothing changes. Test: `TestDLChosenNameKept` (fails on the old code with
"typed name lost").

## 2. Two Windows errors shown as "The download could not continue" (2026-11-10)

Not a bug, but the message gives no hint what to do:

- a path longer than Windows allows ("The filename or extension is too long"), and
- a download folder that no longer exists, e.g. a removed USB drive ("The system cannot find the path specified",
  "The device is not ready").

Pullwell's `dlFriendly` now says what happened and what to do; the three cases are in `TestDLFriendlyMessages`.
