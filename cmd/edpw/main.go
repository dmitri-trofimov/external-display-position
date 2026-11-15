//go:build windows

package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"

	"external-display-position/internal/config"
	"external-display-position/internal/watcher"
)

const maxLogBytes = 128 * 1024

func main() {
	cfgFlag := flag.String("config", "", "path to config.json")
	logFlag := flag.String("log", "", "path to log file (default: %LOCALAPPDATA%\\ExternalDisplayPosition\\edp.log)")
	flag.Parse()

	release, err := watcher.AcquireSingleInstance()
	if err != nil {
		// Another watcher is already active; exit quietly.
		return
	}
	defer release()

	configPath := *cfgFlag
	if configPath == "" {
		configPath = config.DefaultPath()
	}

	logPath := *logFlag
	if logPath == "" {
		logPath = config.DefaultLogPath()
	}

	logOut, closeLog := openLogWriter(logPath)
	defer closeLog()

	_ = watcher.Run(configPath, logOut)
}

func openLogWriter(path string) (io.Writer, func()) {
	if path == "" || path == "nul" {
		return io.Discard, func() {}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return io.Discard, func() {}
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return io.Discard, func() {}
	}
	return f, func() { _ = f.Close() }
}
