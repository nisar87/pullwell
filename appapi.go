package main

// /api/app: settings that belong to the program rather than to the downloads (start with Windows).

import (
	"encoding/json"
	"errors"
	"net/http"
)

// customRoot is true when the program was started with -root: such a copy (a test or development copy) must not
// register itself to start with Windows, or it would replace the real installation's entry (NetMerge 3.11.12).
var customRoot bool

var errCustomRoot = errors.New("this copy was started with its own data folder (-root), so it cannot start with Windows")

func appHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		var in struct {
			Autostart *bool `json:"autostart"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || in.Autostart == nil {
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad request"})
			return
		}
		var err error
		if *in.Autostart && customRoot {
			err = errCustomRoot
		} else {
			err = autoStartSet(*in.Autostart)
		}
		if err != nil {
			logf("start with Windows: %v", err)
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		logf("start with Windows: %v", *in.Autostart)
	}
	on, err := autoStartGet()
	_ = json.NewEncoder(w).Encode(map[string]any{"autostart": on, "canAutostart": err == nil && !customRoot, "version": appVersion})
}
