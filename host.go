package main

// The one place where the program talks to the download engine (package dm).

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"pullwell/dm"
)

// normalLine is the computer's ordinary internet connection; all downloads go out through it.
var normalLine = dm.Line{
	Name: "Computer",
	Kind: "ethernet",
	Dial: (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
}

func setupEngine(root string, port int) {
	dm.Setup(dm.Host{
		Name:    AppName,
		Repo:    appRepo,
		Root:    root,
		Version: appVersion,
		Lines:   func() dm.Lines { return dm.Lines{Use: []dm.Line{normalLine}} },
		Guard:   func(h http.HandlerFunc) http.HandlerFunc { return apiGuard(port)(h) },
		Log:     logf,
		Note: func(kind, format string, a ...any) {
			logf("["+kind+"] "+format, a...)
		},
		Notify:           notify,
		Power:            sysPower,
		PowerCancel:      sysPowerCancel,
		OpenPath:         sysOpenPath,
		Proxy:            proxyFor,
		FreeSpace:        sysFreeSpace,
		MarkFromInternet: sysMarkFromInternet,
		Run: func(args []string) error {
			if len(args) == 0 {
				return errors.New("no command")
			}
			return sysRun(context.Background(), args)
		},
	})
}
