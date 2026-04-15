package runner

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"

	"github.com/yourname/sshops/internal/inventory"
)

type StatusEntry struct {
	Name     string
	Host     string
	Status   string
	Duration time.Duration
}

type Display struct {
	mu      sync.Mutex
	entries []*StatusEntry
	done    chan struct{}

	ansi         bool
	printedLines int
	stopped      bool
}

func NewDisplay(hosts []*inventory.Host) *Display {
	entries := make([]*StatusEntry, 0, len(hosts))
	for _, h := range hosts {
		if h == nil {
			continue
		}
		entries = append(entries, &StatusEntry{
			Name:   h.Name,
			Host:   h.Host,
			Status: "pending",
		})
	}
	return &Display{
		entries: entries,
		done:    make(chan struct{}),
		ansi:    supportsANSI(),
	}
}

func (d *Display) SetStatus(name, status string, duration ...time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, entry := range d.entries {
		if entry != nil && entry.Name == name {
			entry.Status = status
			if len(duration) > 0 {
				entry.Duration = duration[0]
			}
			return
		}
	}
}

func (d *Display) Start() {
	if d == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.render(false)
			case <-d.done:
				d.render(true)
				return
			}
		}
	}()
}

func (d *Display) Stop() {
	if d == nil {
		return
	}

	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	d.mu.Unlock()

	close(d.done)
}

func (d *Display) render(final bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ansi && d.printedLines > 0 {
		fmt.Printf("\033[%dA", d.printedLines)
	}

	lines := make([]string, 0, len(d.entries))
	for _, entry := range d.entries {
		if entry == nil {
			continue
		}
		lines = append(lines, formatEntry(entry))
	}

	for _, line := range lines {
		fmt.Println(line)
	}

	if d.ansi {
		d.printedLines = len(lines)
	}
	if !d.ansi && !final {
		// ANSI 不可用时保持追加模式，不回溯重绘。
	}
}

func formatEntry(entry *StatusEntry) string {
	icon := "○"
	statusColor := color.New(color.FgHiBlack)
	durationText := ""

	switch entry.Status {
	case "running":
		icon = "●"
		statusColor = color.New(color.FgCyan)
	case "ok":
		icon = "✓"
		statusColor = color.New(color.FgGreen)
	case "fail":
		icon = "✗"
		statusColor = color.New(color.FgRed)
	default:
		icon = "○"
		statusColor = color.New(color.FgHiBlack)
	}

	if entry.Duration > 0 {
		durationText = fmt.Sprintf("  %s", entry.Duration.Round(100*time.Millisecond))
	}

	statusLabel := statusColor.Sprintf("%s", entry.Status)
	return fmt.Sprintf("%s %-12s %-18s %-8s%s", icon, entry.Name, entry.Host, statusLabel, durationText)
}

func supportsANSI() bool {
	if runtime.GOOS != "windows" {
		return true
	}
	if os.Getenv("WT_SESSION") != "" {
		return true
	}
	if os.Getenv("ANSICON") != "" {
		return true
	}
	if strings.EqualFold(os.Getenv("ConEmuANSI"), "ON") {
		return true
	}
	if os.Getenv("TERM") != "" {
		return true
	}
	return false
}
