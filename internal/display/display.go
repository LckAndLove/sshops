package display

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/yourname/sshops/internal/audit"
	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/runner"
)

type PlaybookHostResult struct {
	HostName    string
	OkCount     int
	FailedCount int
	Duration    time.Duration
}

func PrintHostTable(hosts []*inventory.Host) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tHOST\tPORT\tUSER\tGROUPS\tTAGS")
	fmt.Fprintln(w, "----\t----\t----\t----\t------\t----")

	for _, h := range hosts {
		if h == nil {
			continue
		}

		tags := make([]string, 0, len(h.Tags))
		for k, v := range h.Tags {
			tags = append(tags, fmt.Sprintf("%s=%s", k, v))
		}

		groups := strings.Join(h.Groups, ",")
		tagText := strings.Join(tags, ",")
		nameText := color.New(color.FgGreen).Sprint(" *") + " " + h.Name

		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
			nameText, h.Host, h.Port, h.User, groups, tagText)
	}
	w.Flush()
}

func PrintExecResult(results []runner.Result) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STATUS\tHOST NAME\tIP\tEXIT\tDUR")
	fmt.Fprintln(w, "------\t---------\t--\t----\t---")

	okCount := 0
	failedCount := 0
	var totalDuration time.Duration

	for _, r := range results {
		hostName := ""
		hostIP := ""
		if r.Host != nil {
			hostName = r.Host.Name
			hostIP = r.Host.Host
		}

		status := "FAIL"
		if r.ExitCode == 0 {
			status = "OK"
			okCount++
		} else {
			failedCount++
		}

		dur := formatDuration(r.Duration)
		totalDuration += r.Duration

		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
			status, hostName, hostIP, r.ExitCode, dur)
	}
	w.Flush()

	total := len(results)
	if failedCount > 0 {
		fmt.Printf("%s %d/%d   %s %d/%d   total %s\n",
			color.New(color.FgGreen).Sprint("OK"), okCount, total,
			color.New(color.FgRed).Sprint("FAIL"), failedCount, total,
			formatDuration(totalDuration),
		)
	} else {
		fmt.Printf("%s %d/%d   total %s\n",
			color.New(color.FgGreen).Sprint("OK"), okCount, total,
			formatDuration(totalDuration),
		)
	}
}

func PrintPlaybookTask(taskName string, total, current int) {
	fmt.Printf("TASK [%s] %s (%d/%d)\n", taskName, strings.Repeat("-", 20), current, total)
}

func PrintPlaybookRecap(results map[string]*PlaybookHostResult) {
	fmt.Printf("PLAY RECAP %s\n", strings.Repeat("-", 30))
	for host, r := range results {
		if r == nil {
			continue
		}
		hostName := r.HostName
		if strings.TrimSpace(hostName) == "" {
			hostName = host
		}
		fmt.Printf("  %s   ok=%d   failed=%d   dur=%.2fs\n", hostName, r.OkCount, r.FailedCount, r.Duration.Seconds())
	}
}

func PrintAuditLogs(logs []audit.LogEntry) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tHOST\tCMD\tEXIT\tDUR\tBY")
	fmt.Fprintln(w, "----\t----\t---\t----\t---\t--")

	for _, l := range logs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%dms\t%s\n",
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			l.HostName,
			l.Command,
			l.ExitCode,
			l.DurationMS,
			l.Operator,
		)
	}
	w.Flush()
}

func PrintDiagnosisReport(host string, symptom string, data map[string]string) {
	title := fmt.Sprintf("Diagnosis: %s @ %s", host, symptom)
	width := len(title)
	if width < 40 {
		width = 40
	}
	sep := strings.Repeat("-", width)

	fmt.Printf("+%s+\n", sep)
	fmt.Printf("| %s |\n", title)
	fmt.Printf("+%s+\n", sep)
	for k, v := range data {
		fmt.Printf("| %s |\n", "[ "+k+" ]")
		for _, line := range strings.Split(v, "\n") {
			fmt.Printf("| %s |\n", line)
		}
	}
	fmt.Printf("+%s+\n", sep)
}

func PrintMetricsCard(host string, metrics map[string]string) {
	fmt.Printf("+- %s %s+\n", host, strings.Repeat("-", 30))

	for _, k := range []string{"cpu", "memory", "disk"} {
		raw, ok := metrics[k]
		if !ok {
			continue
		}
		pct := parsePercent(raw)
		bar := makeBar(pct)
		fmt.Printf("| %-8s [%s] %6.1f%% |\n", strings.ToUpper(k), bar, pct)
	}

	for k, v := range metrics {
		if k == "cpu" || k == "memory" || k == "disk" {
			continue
		}
		fmt.Printf("| %s: %s |\n", k, v)
	}

	fmt.Println("+--------------------------------------------+")
}

func makeBar(pct float64) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	filled := int(pct * 16 / 100)
	if filled > 16 {
		filled = 16
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("#", filled) + strings.Repeat("-", 16-filled)

	if pct <= 60 {
		return color.New(color.FgGreen).Sprint(bar)
	}
	if pct <= 80 {
		return color.New(color.FgYellow).Sprint(bar)
	}
	return color.New(color.FgRed).Sprint(bar)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func parsePercent(raw string) float64 {
	clean := strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	var pct float64
	_, _ = fmt.Sscanf(clean, "%f", &pct)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
