package main

// Starting the program from the browser. The extension cannot start a program by itself, but a browser can start a
// "native messaging host" that is registered for it. The host is this same program, started with an extra argument
// (the browser adds the extension's address): it checks whether the real program is running, starts it in the
// notification area when it is not, answers with the port, and ends. The program registers the host for the current
// user on every start (no administrator rights), pointing at its own path, so this works for a copy that was never
// installed, and it follows the program when the folder is moved.

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"pullwell/dm"
)

// extChromeID is the address of the Chrome and Edge extension. Its manifest carries a fixed public key, so the
// address is the same on every computer.
const extChromeID = "lkhdafbofbljmkmcpgmbeopfifpgedff"

// extStoreID is the same extension as the Chrome Web Store hands it out: the store signs it with its own key, so
// its address differs from extChromeID. Both are trusted to start the program.
const extStoreID = dm.ExtStoreID

func extFirefoxID() string { return "downloads@" + appScheme + ".app" }

// nativeHostName is the name the extension asks for. Lowercase letters, digits, dots and underscores only.
func nativeHostName() string { return appScheme + ".launcher" }

// isNativeHostCall tells whether the program was started by a browser as the native messaging host: Chrome, Edge and
// Brave pass the extension's address; Firefox passes the path of the host's manifest and the add-on's id.
func isNativeHostCall(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if strings.HasPrefix(args[0], "chrome-extension://") {
		return true
	}
	return len(args) >= 2 && strings.HasSuffix(strings.ToLower(args[0]), ".json") && args[1] == extFirefoxID()
}

func chromeOrigin(id string) string { return "chrome-extension://" + id + "/" }

// hostManifests makes the two manifest files the browsers read. origins are extension addresses allowed to start the
// program besides the built-in one.
func hostManifests(exe string, origins []string) (chrome, firefox []byte) {
	set := map[string]bool{chromeOrigin(extChromeID): true, chromeOrigin(extStoreID): true}
	for _, o := range origins {
		if strings.HasPrefix(o, "chrome-extension://") && len(o) < 100 && !strings.ContainsAny(o[len("chrome-extension://"):len(o)-1], "/\\\"") {
			set[strings.TrimRight(o, "/")+"/"] = true
		}
	}
	list := make([]string, 0, len(set))
	for o := range set {
		list = append(list, o)
	}
	sort.Strings(list)
	if len(list) > 8 {
		list = list[:8]
	}
	c := map[string]any{"name": nativeHostName(), "description": AppName + " starter for the browser extension", "path": exe, "type": "stdio", "allowed_origins": list}
	f := map[string]any{"name": nativeHostName(), "description": AppName + " starter for the browser extension", "path": exe, "type": "stdio", "allowed_extensions": []string{extFirefoxID()}}
	chrome, _ = json.MarshalIndent(c, "", "  ")
	firefox, _ = json.MarshalIndent(f, "", "  ")
	return
}

// ---- the messages: a 4 byte length (little endian) followed by that many bytes of JSON ----

const maxHostMsg = 1 << 20

func readHostMsg(r io.Reader) ([]byte, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, err
	}
	l := binary.LittleEndian.Uint32(n[:])
	if l == 0 || l > maxHostMsg {
		return nil, fmt.Errorf("message of %d bytes", l)
	}
	b := make([]byte, l)
	_, err := io.ReadFull(r, b)
	return b, err
}

func writeHostMsg(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var n [4]byte
	binary.LittleEndian.PutUint32(n[:], uint32(len(b)))
	_, err = w.Write(append(n[:], b...))
	return err
}

// pingPorts finds the running program: the first port whose /ext/ping answers as this program.
func pingPorts(client *http.Client, ports []int) (int, bool) {
	for _, p := range ports {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/ext/ping", p))
		if err != nil {
			continue
		}
		var j struct {
			App string `json:"app"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&j)
		resp.Body.Close()
		if err == nil && resp.StatusCode == 200 && j.App == strings.ToLower(AppName) {
			return p, true
		}
	}
	return 0, false
}

// resolveRoot finds the folder that holds the data folder: the -root given, else the program's own folder, else (a
// protected folder such as Program Files) the person's app data folder. custom is true for a separate copy.
func resolveRoot(flagRoot string) (dir string, custom bool, err error) {
	dir, custom = flagRoot, flagRoot != ""
	if dir == "" {
		exe, e := os.Executable()
		if e != nil {
			return "", false, e
		}
		dir = filepath.Dir(exe)
		// a file named "separate-copy" next to the program makes it a separate copy (a test copy, for example),
		// exactly as if it had been started with -root on its own folder: its own data, and it leaves the Start
		// menu entry, the link registration and Start with Windows of the installed copy alone
		if _, e := os.Stat(filepath.Join(dir, separateMarker)); e == nil {
			custom = true
		} else if !writableDir(filepath.Join(dir, "data")) {
			// a protected folder (Program Files, for example): keep the data in the person's own app data folder
			if la := os.Getenv("LOCALAPPDATA"); la != "" {
				dir = filepath.Join(la, appDataDir)
			}
		}
	}
	dir, _ = filepath.Abs(dir)
	return dir, custom, nil
}

// ---- the key that lets the starter pair a browser without a typed code ----

func hostKeyPath(root string) string { return filepath.Join(root, "data", "host.key") }

// loadHostKey reads the key, or makes it when asked to (the program does; the starter only reads).
func loadHostKey(root string, create bool) string {
	p := hostKeyPath(root)
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b))
	}
	if !create {
		return ""
	}
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		return ""
	}
	key := hex.EncodeToString(k)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if os.WriteFile(p, []byte(key), 0o600) != nil {
		return ""
	}
	return key
}

// autoPairOrigin: the extensions that may pair without a code. The browser has already checked that the caller is
// one of the addresses in the starter's manifest; the built-in and store addresses and the ones that paired by code
// before are accepted, and Firefox's add-on.
func autoPairOrigin(o string) bool {
	o = strings.TrimRight(o, "/") + "/"
	if o == chromeOrigin(extChromeID) || o == chromeOrigin(extStoreID) || o == "firefox:"+extFirefoxID()+"/" {
		return true
	}
	originMu.Lock()
	defer originMu.Unlock()
	return knownOrigins[o]
}

// hostOrigin is who the browser says started the starter: Chrome-like browsers pass the extension's address,
// Firefox the manifest path and the add-on id.
func hostOrigin(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if strings.HasPrefix(args[0], "chrome-extension://") {
		return args[0]
	}
	if len(args) >= 2 {
		return "firefox:" + args[1]
	}
	return ""
}

type hostDeps struct {
	origin string        // who started the starter (hostOrigin)
	key    func() string // the pairing key from the data folder
	ports  []int
	start  func() error  // starts the program (in the notification area)
	wait   time.Duration // how long to wait for it to answer
}

// serveHost answers the browser's messages until it closes the pipe. {cmd:"ping"} only looks; {cmd:"start"} starts
// the program when it is not running and answers once it is up (or the wait is over).
func serveHost(in io.Reader, out io.Writer, d hostDeps) {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	for {
		raw, err := readHostMsg(in)
		if err != nil {
			return
		}
		var msg struct {
			Cmd string `json:"cmd"`
		}
		_ = json.Unmarshal(raw, &msg)
		reply := map[string]any{"ok": false, "version": appVersion}
		switch msg.Cmd {
		case "ping", "start":
			port, up := pingPorts(client, d.ports)
			started := false
			if !up && msg.Cmd == "start" {
				if err := d.start(); err != nil {
					reply["error"] = err.Error()
				} else {
					started = true
					for end := time.Now().Add(d.wait); time.Now().Before(end) && !up; {
						time.Sleep(150 * time.Millisecond)
						port, up = pingPorts(client, d.ports)
					}
				}
			}
			reply["ok"], reply["port"], reply["started"] = up, port, started
		case "pair":
			var pm struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(raw, &pm)
			port, up := pingPorts(client, d.ports)
			switch {
			case !up:
				reply["error"] = "not running"
			case d.key == nil || d.key() == "":
				reply["error"] = "no key"
			default:
				tok, err := hostPair(client, port, d.key(), d.origin, pm.Name)
				if err != nil {
					reply["error"] = err.Error()
				} else {
					reply["ok"], reply["token"], reply["port"] = true, tok, port
				}
			}
		default:
			reply["error"] = "unknown command"
		}
		if writeHostMsg(out, reply) != nil {
			return
		}
	}
}

func hostPair(client *http.Client, port int, key, origin, name string) (string, error) {
	body, _ := json.Marshal(map[string]string{"key": key, "origin": origin, "name": name})
	resp, err := client.Post(fmt.Sprintf("http://127.0.0.1:%d/ext/hostpair", port), "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var j struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&j) != nil || resp.StatusCode != 200 || j.Token == "" {
		return "", errors.New("the program refused")
	}
	return j.Token, nil
}

var errHostNoStart = errors.New("could not start the program")

// ---- the origins of extensions that have talked to the program ----
// A copy of the extension made without the fixed key (loaded by hand from the older zip) has another address. The
// program sees it on its requests and allows it in the manifest too.

var originMu sync.Mutex
var knownOrigins = map[string]bool{}

// learnOrigin remembers an extension address; it returns true when it is new (the manifest then needs a rewrite).
func learnOrigin(o string) bool {
	if !strings.HasPrefix(o, "chrome-extension://") || len(o) > 100 {
		return false
	}
	o = strings.TrimRight(o, "/") + "/"
	if o == chromeOrigin(extChromeID) || o == chromeOrigin(extStoreID) {
		return false
	}
	originMu.Lock()
	defer originMu.Unlock()
	if knownOrigins[o] || len(knownOrigins) >= 6 {
		return false
	}
	knownOrigins[o] = true
	return true
}

func originList() []string {
	originMu.Lock()
	defer originMu.Unlock()
	out := make([]string, 0, len(knownOrigins))
	for o := range knownOrigins {
		out = append(out, o)
	}
	return out
}
