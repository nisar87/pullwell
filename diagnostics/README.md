# Problem reports (diagnostics)

Pullwell can send its log to the developer so errors on other computers can be found and fixed. What is sent, and
what never is, is written in the [README](../README.md#privacy) and the [privacy policy](../docs/privacy.md). This
folder holds the cloud side.

## What the program sends

`report.go` makes one report about every 6 hours (and one after a crash) and adds it to the Firestore collection
`reports` of the Firebase project `pullwell-app` with a plain HTTPS request (REST, no SDK):

| field | content |
| --- | --- |
| `install` | random number made on first use, kept in `data/app.json`; not linked to the person or the computer |
| `app`, `version`, `os`, `cpu`, `lang` | `pullwell`, program version, Windows build, processor count, window language |
| `kind` | `log`, or `crash` when a crash text is attached |
| `settings` | a few on/off settings, never a folder, site or address |
| `log` | the log lines since the last report, scrubbed (see `diag.go`), repeated lines folded, newest 120 000 bytes |
| `crash` | the crash text of a run that crashed, scrubbed, up to 40 000 bytes |
| `at` | time of the report |

A test run names its own install (`test-…`); the daily check ignores those.

## The rules

`firestore.rules` is what is published in the Firebase console (Firestore → Rules). The web key inside the program is
public by design: it only names the project, and the rules are the protection. They allow only **adding** a report
with exactly these fields and size limits, and refuse every read, change and delete. Anything else in the project is
refused too. Checked with real requests: a valid report 200, an unknown field 403, a wrong `kind` 403, a read 403.

## Reading the reports

Only the developer's Google account can read them. See the reading workflow (added next to this file).
