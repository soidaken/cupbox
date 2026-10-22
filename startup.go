package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Normal Explorer launches need diagnostics too: an environment variable
// cannot be enabled retroactively after an intermittent slow startup.
// Keep only startup events in automatic logs, bounded to 20 small files.
var startupLog struct {
	sync.Mutex
	initialized bool
	automatic   bool
	file        *os.File
	written     int
}

func logf_(format string, a ...any) {
	startupLog.Lock()
	defer startupLog.Unlock()
	if !startupLog.initialized {
		startupLog.initialized = true
		name := os.Getenv("CUPBOX_LOG")
		startupLog.automatic = name == ""
		if name == "" {
			root, err := os.UserCacheDir()
			if err != nil {
				return
			}
			dir := filepath.Join(root, "Cupbox", "logs")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return
			}
			trimStartupLogs(dir)
			name = filepath.Join(dir, fmt.Sprintf("startup-%s-%d.log", time.Now().Format("20060102-150405.000"), os.Getpid()))
		}
		startupLog.file, _ = os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	if startupLog.file == nil {
		return
	}
	if startupLog.automatic && (!strings.HasPrefix(format, "startup:") && !strings.HasPrefix(format, "fatal:") &&
		!strings.HasPrefix(format, "no audio:") && !strings.HasPrefix(format, "hw:") || startupLog.written >= 64*1024) {
		return
	}
	n, _ := fmt.Fprintf(startupLog.file, "%s +%v %s\n", time.Now().Format(time.RFC3339Nano), time.Since(t0), fmt.Sprintf(format, a...))
	startupLog.written += n
}

func trimStartupLogs(dir string) {
	entries, _ := os.ReadDir(dir) // sorted by filename (timestamp first)
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "startup-") && strings.HasSuffix(entry.Name(), ".log") {
			names = append(names, entry.Name())
		}
	}
	for len(names) >= 20 {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

func logStartupProcess(entered time.Time) {
	exe, _ := os.Executable()
	logf_("startup: executable=%q pid=%d", exe, os.Getpid())
	var created, exited, kernel, user syscall.Filetime
	getTimes := syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessTimes")
	ok, _, _ := getTimes.Call(^uintptr(0), uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if ok != 0 {
		birth := time.Unix(0, created.Nanoseconds())
		logf_("startup: OS process created at %s; process-to-main-entry=%v (loader/runtime/Go initialization)", birth.Format(time.RFC3339Nano), entered.Sub(birth))
	}
}
