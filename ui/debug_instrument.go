package ui

// Render-loop instrumentation. Set SMC_TUI_DEBUG to a log file path to record
// every non-tick Update message and a line-level diff of each frame whose
// rendered content changed; inert otherwise. The TUI runs on the alt-screen,
// so debug output must go to a file, never stdout.
//
// This is the tool that pinned down the applications-page blinking (issue
// router-refactor/05): an idle TUI should log ~zero "FRAME CHANGED" lines —
// any steady stream of them means some model input is churning, and the
// logged line diffs show exactly which strip/param. Pairs with the daemon's
// SMC_ROUTER_DEBUG / SMC_POLLER_DEBUG and the SIGUSR1 page-switch hook; see
// docs/DAEMON_AND_AUDIO.md "Debug instrumentation" for the full playbook.

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

var (
	dbgMu     sync.Mutex
	dbgLog    *log.Logger
	dbgLast   string
	dbgViews  int
	dbgDiffed int
)

func init() {
	path := os.Getenv("SMC_TUI_DEBUG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	dbgLog = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	dbgLog.Printf("=== TUI debug session start ===")
}

func dbgMsg(msg any) {
	if dbgLog == nil {
		return
	}
	switch msg.(type) {
	case snapshotMsg:
		// 20 Hz tick; only log these when the frame later changes.
	default:
		dbgMu.Lock()
		dbgLog.Printf("update: %T", msg)
		dbgMu.Unlock()
	}
}

// dbgView records a rendered frame and logs which lines changed vs the
// previous frame. Returns v unchanged so it can wrap View()'s result.
func dbgView(v string) string {
	if dbgLog == nil {
		return v
	}
	dbgMu.Lock()
	defer dbgMu.Unlock()
	dbgViews++
	if v == dbgLast {
		return v
	}
	dbgDiffed++
	oldLines := strings.Split(dbgLast, "\n")
	newLines := strings.Split(v, "\n")
	n := max(len(oldLines), len(newLines))
	var changed []int
	for i := 0; i < n; i++ {
		var o, nl string
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			nl = newLines[i]
		}
		if o != nl {
			changed = append(changed, i)
		}
	}
	dbgLog.Printf("FRAME CHANGED (#%d of %d views): %d lines differ: %v", dbgDiffed, dbgViews, len(changed), changed)
	for _, i := range changed {
		if len(changed) > 8 && i != changed[0] && i != changed[1] {
			break // log detail only for the first two lines when many differ
		}
		var o, nl string
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			nl = newLines[i]
		}
		dbgLog.Printf("  line %d old: %s", i, fmt.Sprintf("%q", o))
		dbgLog.Printf("  line %d new: %s", i, fmt.Sprintf("%q", nl))
	}
	dbgLast = v
	return v
}
