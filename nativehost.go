package main

// Starting the program from the browser. The extension cannot start a program by itself, but a browser can start a
// "native messaging host" that is registered for it. The host is this same program, started with an extra argument
// (the browser adds the extension's address): it checks whether the real program is running, starts it in the
// notification area when it is not, answers with the port, and ends. The program registers the host for the current
// user on every start (no administrator rights), pointing at its own path, so this works for a copy that was never
// installed, and it follows the program when the folder is moved.

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

type hostDeps struct {
	ports []int
	start func() error  // starts the program (in the notification area)
	wait  time.Duration // how long to wait for it to answer
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
		default:
			reply["error"] = "unknown command"
		}
		if writeHostMsg(out, reply) != nil {
			return
		}
	}
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
