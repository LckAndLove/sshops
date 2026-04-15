package display

import (
	"fmt"
	"strings"
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
	headers := []string{"名称", "主机", "端口", "用户", "分组", "标签"}
	rows := make([][]string, 0, len(hosts))
	nameCol := make([]string, 0, len(hosts)+1)
	hostCol := []string{headers[1]}
	portCol := []string{headers[2]}
	userCol := []string{headers[3]}
	groupCol := []string{headers[4]}
	tagCol := []string{headers[5]}

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
		nameText := "* " + h.Name

		rows = append(rows, []string{
			nameText,
			h.Host,
			fmt.Sprintf("%d", h.Port),
			h.User,
			groups,
			tagText,
		})

		nameCol = append(nameCol, nameText)
		hostCol = append(hostCol, h.Host)
		portCol = append(portCol, fmt.Sprintf("%d", h.Port))
		userCol = append(userCol, h.User)
		groupCol = append(groupCol, groups)
		tagCol = append(tagCol, tagText)
	}
	nameCol = append([]string{headers[0]}, nameCol...)

	widths := []int{
		maxWidth(nameCol),
		maxWidth(hostCol),
		maxWidth(portCol),
		maxWidth(userCol),
		maxWidth(groupCol),
		maxWidth(tagCol),
	}

	fmt.Println(drawTop(widths))
	fmt.Printf("| %s | %s | %s | %s | %s | %s |\n",
		padRight(headers[0], widths[0]),
		padRight(headers[1], widths[1]),
		padRight(headers[2], widths[2]),
		padRight(headers[3], widths[3]),
		padRight(headers[4], widths[4]),
		padRight(headers[5], widths[5]),
	)
	fmt.Println(drawMid(widths))

	greenDot := color.New(color.FgGreen).Sprint("*")
	for _, row := range rows {
		name := row[0]
		if strings.HasPrefix(name, "* ") {
			name = greenDot + " " + strings.TrimPrefix(name, "* ")
		}
		fmt.Printf("| %s | %s | %s | %s | %s | %s |\n",
			padRight(name, widths[0]),
			padRight(row[1], widths[1]),
			padRight(row[2], widths[2]),
			padRight(row[3], widths[3]),
			padRight(row[4], widths[4]),
			padRight(row[5], widths[5]),
		)
	}
	fmt.Println(drawBottom(widths))
}

func PrintExecResult(results []runner.Result) {
	headers := []string{"status", "host name", "IP", "exit code", "duration"}
	rows := make([][]string, 0, len(results))

	statusCol := []string{headers[0]}
	hostCol := []string{headers[1]}
	ipCol := []string{headers[2]}
	exitCol := []string{headers[3]}
	durCol := []string{headers[4]}

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
		exit := fmt.Sprintf("%d", r.ExitCode)
		totalDuration += r.Duration

		rows = append(rows, []string{status, hostName, hostIP, exit, dur})
		statusCol = append(statusCol, status)
		hostCol = append(hostCol, hostName)
		ipCol = append(ipCol, hostIP)
		exitCol = append(exitCol, exit)
		durCol = append(durCol, dur)
	}

	widths := []int{
		maxWidth(statusCol),
		maxWidth(hostCol),
		maxWidth(ipCol),
		maxWidth(exitCol),
		maxWidth(durCol),
	}

	fmt.Println(drawTop(widths))
	fmt.Printf("| %s | %s | %s | %s | %s |\n",
		padRight(headers[0], widths[0]),
		padRight(headers[1], widths[1]),
		padRight(headers[2], widths[2]),
		padRight(headers[3], widths[3]),
		padRight(headers[4], widths[4]),
	)
	fmt.Println(drawMid(widths))

	green := color.New(color.FgGreen)
	red := color.New(color.FgRed)
	for _, row := range rows {
		status := row[0]
		statusColor := green.Sprint(status)
		if status == "FAIL" {
			statusColor = red.Sprint(status)
		}

		fmt.Printf("| %s | %s | %s | %s | %s |\n",
			padRight(statusColor, widths[0]),
			padRight(row[1], widths[1]),
			padRight(row[2], widths[2]),
			padRight(row[3], widths[3]),
			padRight(row[4], widths[4]),
		)
	}
	fmt.Println(drawBottom(widths))

	total := len(results)
	var summary string
	if failedCount > 0 {
		summary = fmt.Sprintf("%s %d/%d 成功   %s %d/%d 失败   总耗时 %s",
			green.Sprint("OK"), okCount, total,
			red.Sprint("FAIL"), failedCount, total,
			formatDuration(totalDuration),
		)
	} else {
		summary = fmt.Sprintf("%s %d/%d 成功   总耗时 %s",
			green.Sprint("OK"), okCount, total,
			formatDuration(totalDuration),
		)
	}
	fmt.Println(summary)
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
		fmt.Printf("  %s   ok=%d   failed=%d   duration=%.2fs\n", hostName, r.OkCount, r.FailedCount, r.Duration.Seconds())
	}
}

func PrintAuditLogs(logs []audit.LogEntry) {
	fmt.Printf("%-20s %-10s %-20s %-6s %-10s %-10s\n",
		"时间", "主机", "命令", "退出码", "耗时", "操作人")
	fmt.Println(strings.Repeat("-", 80))
	for _, l := range logs {
		fmt.Printf("%-20s %-10s %-20s %-6d %-7d ms %-10s\n",
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			l.HostName,
			l.Command,
			l.ExitCode,
			l.DurationMS,
			l.Operator,
		)
	}
}

func PrintDiagnosisReport(host string, symptom string, data map[string]string) {
	title := fmt.Sprintf("诊断报告：%s @ %s", host, symptom)
	width := len([]rune(title))
	for k, v := range data {
		if w := len([]rune("[ "+k+" ]")); w > width {
			width = w
		}
		for _, line := range strings.Split(v, "\n") {
			if w := len([]rune(line)); w > width {
				width = w
			}
		}
	}
	width += 2

	fmt.Printf("+%s+\n", strings.Repeat("-", width+2))
	fmt.Printf("| %s |\n", padRight(title, width))
	fmt.Printf("+%s+\n", strings.Repeat("-", width+2))
	for k, v := range data {
		fmt.Printf("| %s |\n", padRight("[ "+k+" ]", width))
		for _, line := range strings.Split(v, "\n") {
			fmt.Printf("| %s |\n", padRight(line, width))
		}
	}
	fmt.Printf("+%s+\n", strings.Repeat("-", width+2))
}

func PrintMetricsCard(host string, metrics map[string]string) {
	bodyWidth := 49
	topRight := bodyWidth - 4 - len([]rune(host))
	if topRight < 1 {
		topRight = 1
	}
	fmt.Printf("+- %s %s+\n", host, strings.Repeat("-", topRight))

	keyWidth := 8
	for _, k := range []string{"cpu", "memory", "disk"} {
		raw, ok := metrics[k]
		if !ok {
			continue
		}
		pct := parsePercent(raw)
		bar := makeBar(pct)
		fmt.Printf("| %-*s [%s] %6.1f%% |\n", keyWidth, strings.ToUpper(k), bar, pct)
	}

	for k, v := range metrics {
		if k == "cpu" || k == "memory" || k == "disk" {
			continue
		}
		line := fmt.Sprintf("%s: %s", k, v)
		fmt.Printf("| %s |\n", padRight(line, bodyWidth))
	}

	fmt.Println("+---------------------------------------------------+")
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

	bar := strings.Repeat("█", filled) + strings.Repeat("░", 16-filled)

	if pct <= 60 {
		return color.New(color.FgGreen).Sprint(bar)
	}
	if pct <= 80 {
		return color.New(color.FgYellow).Sprint(bar)
	}
	return color.New(color.FgRed).Sprint(bar)
}

func maxWidth(items []string) int {
	max := 0
	for _, s := range items {
		w := len([]rune(s))
		if w > max {
			max = w
		}
	}
	return max
}

func padRight(s string, width int) string {
	l := len([]rune(s))
	if l >= width {
		return s
	}
	return s + strings.Repeat(" ", width-l)
}

func drawTop(widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, w := range widths {
		parts = append(parts, strings.Repeat("-", w+2))
	}
	return "+" + strings.Join(parts, "+") + "+"
}

func drawMid(widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, w := range widths {
		parts = append(parts, strings.Repeat("-", w+2))
	}
	return "+" + strings.Join(parts, "+") + "+"
}

func drawBottom(widths []int) string {
	parts := make([]string, 0, len(widths))
	for _, w := range widths {
		parts = append(parts, strings.Repeat("-", w+2))
	}
	return "+" + strings.Join(parts, "+") + "+"
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
