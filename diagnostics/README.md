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

Nothing can read them with the key in the program. They are read by the GitHub workflow
[`diagnostics.yml`](../.github/workflows/diagnostics.yml), started with `diagnostics/read.sh [days]`:

1. `read.sh` makes a one-time RSA key pair in a temporary folder and starts the workflow with the public key.
2. The workflow gets a short-lived Google token **without any stored secret** (Workload Identity Federation: Google
   trusts only this repository's `main` branch) and acts as the service account `diagnostics-reader`, which has the
   role *Cloud Datastore Viewer* and nothing else. It reads the reports of the last N days (`fetch.py`), encrypts them
   (AES-256 with a random key, itself encrypted with the public key) and force-pushes the encrypted file to the branch
   `diagnostics-data`. The workflow log shows counts only.
3. `read.sh` fetches the branch, decrypts with the private key, deletes the key, and leaves `reports.jsonl`.
4. `summary.py reports.jsonl` shows what stands out (crashes, runs that did not end, recovered crashes, problems per
   cause and number of installs). Test installs (`test-…`) are ignored.

The reports are private data of the people who sent them: `reports.jsonl` is never committed, pasted into an issue or
a pull request; only causes, versions and numbers are quoted when fixing something.

### Google Cloud setup (done once, in the console of project `pullwell-app`)

- Workload Identity Pool `github`, OIDC provider `github`, issuer `https://token.actions.githubusercontent.com`,
  mapping `google.subject = assertion.sub`, condition `assertion.repository=='nisar87/pullwell' && assertion.ref=='refs/heads/main'`.
- Service account `diagnostics-reader@pullwell-app.iam.gserviceaccount.com`, role *Cloud Datastore Viewer*; the pool
  identity `repo:nisar87@42030325/pullwell@1452114117:ref:refs/heads/main` (GitHub puts the owner's and repository's
  numeric ids in the subject) may impersonate it (*Workload Identity User*). If the repository is ever renamed or
  moved, the subject changes and this grant must follow.
- APIs enabled: IAM Service Account Credentials, Security Token Service (and Firestore).
- To stop all reading: disable the pool provider or delete the service account. To stop all reports arriving: change
  the Firestore rule to `allow create: if false;`.
