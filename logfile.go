package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The program's log: data/<name>.log, kept below about 2 MB (the older half is in .log.old).

const logMax = 2 << 20

var (
	logMu   sync.Mutex
	logPath string
)

func logInit(root string) {
	logMu.Lock()
	defer logMu.Unlock()
	dir := filepath.Join(root, "data")
	_ = os.MkdirAll(dir, 0o755)
	logPath = filepath.Join(dir, appScheme+".log")
}

func logf(format string, a ...any) {
	line := time.Now().Format("2006-01-02 15:04:05 ") + strings.TrimRight(fmt.Sprintf(format, a...), "\n") + "\n"
	logMu.Lock()
	defer logMu.Unlock()
	if logPath == "" {
		fmt.Fprint(os.Stderr, line)
		return
	}
	if st, err := os.Stat(logPath); err == nil && st.Size() > logMax {
		_ = os.Rename(logPath, logPath+".old")
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}
