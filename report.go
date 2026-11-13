package main

// Diagnostics: the program's own log, already scrubbed (see diag.go), can be sent to the developer so problems that
// happen on other computers can be found and fixed. It is on by default, shown to the person once before anything
// is sent, and can be turned off in Options.
//
// What is sent: a random install id (made here, tied to nothing about the person or the computer), the program
// version, the Windows version, the number of CPUs, the window language, a few settings (on/off, never folders or
// addresses), the log lines written since the last report and, after a crash, the crash text.
// What is never sent: links, web sites, file names, folders, user or computer names (the log never holds them), the
// download list, cookies, saved logins.
//
// Where it goes: one new document per report in a Firestore collection of the developer's Firebase project. The
// project's rules only allow adding a document with these fields and sizes; nothing can be read back with the key
// that is in this file (see diagnostics/README.md).

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The key only names the Firebase project; the project's rules decide what it may do (add reports, nothing else).
const diagKey = "AIzaSyAGeRQdEbAtwerylI4rWIuqlw6ZcWkyzSo"

var (
	diagURL    = "https://firestore.googleapis.com/v1/projects/pullwell-app/databases/(default)/documents/reports?key=" + diagKey // tests point it at a local server
	diagSend   = true                                                                                                             // tests switch the background sending off
	diagMu     sync.Mutex                                                                                                         // one report at a time
	diagClient = &http.Client{Timeout: 40 * time.Second}
)

const (
	diagLogMax   = 120000 // bytes of log in one report (the rules allow 200000)
	diagCrashMax = 40000  // bytes of crash text (the rules allow 50000)
	diagEvery    = 6 * time.Hour
	diagRetry    = 30 * time.Minute
)

// diagInstallID is the random id of this installation, made on first use. A test run names its own (it must start
// with "test-", so the developer's daily check ignores it).
func diagInstallID() string {
	if v := os.Getenv("PULLWELL_DIAG_INSTALL"); v != "" {
		return v
	}
	appSetMu.Lock()
	id := appSet.InstallID
	appSetMu.Unlock()
	if id != "" {
		return id
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	id = hex.EncodeToString(b)
	_ = saveAppSettings(func(a *appSettings) {
		if a.InstallID == "" {
			a.InstallID = id
		} else {
			id = a.InstallID
		}
	})
	return id
}

// diagOn is whether reports may be sent now: the person has not turned it off, has seen the notice, and this is
// not a test copy with its own data folder (those only send when a test names its install id).
func diagOn() bool {
	appSetMu.Lock()
	off, noticed := appSet.NoDiag, appSet.DiagNoticed
	appSetMu.Unlock()
	if off || !noticed {
		return false
	}
	return !customRoot || strings.HasPrefix(os.Getenv("PULLWELL_DIAG_INSTALL"), "test-")
}

func diagMarkFile() string  { return filepath.Join(appRoot, "data", "report.sent") }
func diagCrashPath() string { return filepath.Join(appRoot, "data", "crash.1.txt") }

// ---- building a report ----------------------------------------------------------------------------------------------

var reCountKV = regexp.MustCompile(`([a-z][a-z0-9._-]*)=(\d+)`)

// thinLog turns log text into what is sent: only lines after mark (a "2006-01-02 15:04:05" time), the per-minute
// counts added up into one line, repeated lines folded, the report's own lines left out, the newest max bytes kept.
// markN is how many lines with the time mark were already sent (log times have one-second steps). It returns the
// text, the time of the newest line seen and how many lines have that time.
func thinLog(text, mark string, markN, max int) (string, string, int) {
	const tsLen = 19
	type entry struct {
		ts, msg string
		n       int
	}
	var list []entry
	newest, newestN, atMark := mark, markN, 0
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		if len(l) < tsLen+1 || l[4] != '-' || l[10] != ' ' || l[13] != ':' {
			if n := len(list); n > 0 { // a line that continues the one before
				list[n-1].msg += " ⏎ " + l
			}
			continue
		}
		ts, msg := l[:tsLen], strings.TrimSpace(l[tsLen:])
		if ts < mark {
			continue
		}
		if ts == mark {
			if atMark++; atMark <= markN {
				continue
			}
		}
		if ts > newest {
			newest, newestN = ts, 0
		}
		newestN++
		if strings.HasPrefix(msg, "diagnostics:") {
			continue
		}
		list = append(list, entry{ts, msg, 1})
	}
	sums, minutes, first := map[string]int{}, 0, ""
	var out []entry
	for _, e := range list {
		if rest, ok := strings.CutPrefix(e.msg, "counts in the last minute:"); ok {
			for _, m := range reCountKV.FindAllStringSubmatch(rest, -1) {
				n, _ := strconv.Atoi(m[2])
				sums[m[1]] += n
			}
			minutes++
			if first == "" {
				first = e.ts
			}
			continue
		}
		if k := len(out); k > 0 && out[k-1].msg == e.msg {
			out[k-1].n++
			continue
		}
		out = append(out, e)
	}
	var b strings.Builder
	if minutes > 0 {
		keys := make([]string, 0, len(sums))
		for k := range sums {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s=%d", k, sums[k])
		}
		fmt.Fprintf(&b, "%s counts over %d minute(s), added up: %s\n", first, minutes, strings.Join(parts, " "))
	}
	lines := make([]string, len(out))
	for i, e := range out {
		lines[i] = e.ts + " " + e.msg
		if e.n > 1 {
			lines[i] += fmt.Sprintf(" (×%d)", e.n)
		}
	}
	total := b.Len()
	for _, l := range lines {
		total += len(l) + 1
	}
	skip := 0
	for total > max && skip < len(lines)-1 { // drop the oldest lines first
		total -= len(lines[skip]) + 1
		skip++
	}
	if skip > 0 {
		fmt.Fprintf(&b, "… %d older line(s) left out\n", skip)
	}
	for _, l := range lines[skip:] {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String(), newest, newestN
}

// diagSettings is a short summary of settings: on/off and kinds, never a folder, a site or an address.
func diagSettings() string {
	onoff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	appSetMu.Lock()
	a := appSet
	appSetMu.Unlock()
	pm := a.Proxy.Mode
	if pm == "" {
		pm = "system"
	}
	updCh := updCfg.Channel
	return fmt.Sprintf("clipboard=%s keepAwake=%s showDone=%s proxy=%s updates=%s autoUpdate=%s", onoff(a.Clipboard), onoff(!a.NoKeepAwake), onoff(a.ShowDone), pm, updCh, onoff(!updCfg.NoAuto))
}

func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.ToValidUTF8(s[len(s)-max:], "")
}

func fsStr(v string) map[string]string { return map[string]string{"stringValue": v} }

type reportBody struct {
	payload []byte
	newest  string // time of the newest log line in it
	newestN int    // how many lines have that time
	crash   bool
}

// buildReport makes the report to send, or ok=false when there is nothing new to say.
func buildReport() (rb reportBody, ok bool) {
	mark, markN := "", 0
	if b, err := os.ReadFile(diagMarkFile()); err == nil && len(b) >= 19 { // "<time> <lines already sent at that time>"
		mark = string(b[:19])
		markN, _ = strconv.Atoi(strings.TrimSpace(string(b[19:])))
	}
	if mark == "" {
		mark = time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04:05")
	}
	var text []byte
	logMu.Lock()
	p := logPath
	logMu.Unlock()
	if p != "" {
		old, _ := os.ReadFile(p + ".old")
		cur, _ := os.ReadFile(p)
		text = append(old, cur...)
	}
	logText, newest, newestN := thinLog(string(text), mark, markN, diagLogMax)
	var crash string
	if b, err := os.ReadFile(diagCrashPath()); err == nil && len(bytes.TrimSpace(b)) > 0 {
		crash = clip(scrub(string(b)), diagCrashMax)
	}
	if strings.TrimSpace(logText) == "" && crash == "" {
		return rb, false
	}
	kind := "log"
	if crash != "" {
		kind = "crash"
	}
	if logText == "" {
		logText = "(no new log lines)\n"
	}
	f := map[string]any{
		"install":  fsStr(diagInstallID()),
		"app":      fsStr("pullwell"),
		"version":  fsStr(clip(appVersion, 30)),
		"os":       fsStr(clip(osVersion()+" "+runtime.GOARCH, 60)),
		"cpu":      fsStr(strconv.Itoa(runtime.NumCPU())),
		"kind":     fsStr(kind),
		"settings": fsStr(clip(diagSettings(), 3900)),
		"log":      fsStr(logText),
		"at":       map[string]string{"timestampValue": time.Now().UTC().Format(time.RFC3339)},
	}
	langMu.Lock()
	lc := langCode
	langMu.Unlock()
	if len(lc) <= 16 {
		f["lang"] = fsStr(lc)
	}
	if crash != "" {
		f["crash"] = fsStr(crash)
	}
	b, _ := json.Marshal(map[string]any{"fields": f})
	return reportBody{payload: b, newest: newest, newestN: newestN, crash: crash != ""}, true
}

// ---- sending ----------------------------------------------------------------------------------------------------------

var errNothingNew = fmt.Errorf("nothing new to report")

// sendReport sends one report now. It returns errNothingNew when there are no new log lines.
func sendReport() error {
	diagMu.Lock()
	defer diagMu.Unlock()
	rb, ok := buildReport()
	if !ok {
		return errNothingNew
	}
	req, err := http.NewRequest("POST", diagURL, bytes.NewReader(rb.payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c := *diagClient
	c.Transport = updClient.Transport // the same proxy rules as everything else
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach the server (%s)", dlErrWord(err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != 200 {
		return fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	if rb.newest != "" {
		_ = os.WriteFile(diagMarkFile(), []byte(fmt.Sprintf("%s %d", rb.newest, rb.newestN)), 0o644)
	}
	if rb.crash {
		_ = os.Rename(diagCrashPath(), filepath.Join(appRoot, "data", "crash.sent.txt"))
	}
	logf("diagnostics: report sent (%d bytes%s)", len(rb.payload), map[bool]string{true: ", with a crash", false: ""}[rb.crash])
	return nil
}

// dlErrWord is a short word for a network error, without the address in it.
func dlErrWord(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "dns"
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline"):
		return "timeout"
	case strings.Contains(s, "refused"):
		return "refused"
	case strings.Contains(s, "certificate"):
		return "certificate"
	}
	return "network"
}

var diagLoopOnce sync.Once

// diagReportLoop sends a report a few minutes after the start (a crash report from the last run goes then too) and
// every six hours after that, trying again sooner when the server could not be reached. It does nothing while the
// switch is off or the notice has not been shown yet.
func diagReportLoop() {
	if !diagSend {
		return
	}
	diagLoopOnce.Do(func() {
		go func() {
			wait := 3 * time.Minute
			for {
				time.Sleep(wait)
				wait = diagEvery
				if !diagOn() {
					wait = time.Minute // the person may turn it on or close the notice any time
					continue
				}
				if err := sendReport(); err != nil && err != errNothingNew {
					logf("diagnostics: not sent: %v", err)
					wait = diagRetry
				}
			}
		}()
	})
}

// diagTurn switches sending on or off. Turning it off keeps an unsent crash report from being sent later.
func diagTurn(on bool) error {
	err := saveAppSettings(func(a *appSettings) { a.NoDiag = !on; a.DiagNoticed = true })
	if err == nil && !on {
		_ = os.Rename(diagCrashPath(), filepath.Join(appRoot, "data", "crash.sent.txt"))
	}
	logf("diagnostics: %s", map[bool]string{true: "on", false: "off"}[on])
	return err
}

// diagState is what the window needs: whether reporting is on, whether the notice was shown, when the last report went.
func diagState() map[string]any {
	appSetMu.Lock()
	off, noticed := appSet.NoDiag, appSet.DiagNoticed
	appSetMu.Unlock()
	last := int64(0)
	if st, err := os.Stat(diagMarkFile()); err == nil {
		last = st.ModTime().Unix()
	}
	return map[string]any{"on": !off, "noticed": noticed, "last": last}
}
