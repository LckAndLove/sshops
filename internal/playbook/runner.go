package playbook

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/yourname/sshops/internal/display"
	"github.com/yourname/sshops/internal/inventory"
	execrunner "github.com/yourname/sshops/internal/runner"
)

type PlaybookRunner struct {
	Inventory *inventory.Inventory
	Runner    *execrunner.Runner
	Vars      map[string]string
	KeyPath   string
	Password  string
	Out       io.Writer
}

type hostRecap struct {
	OK       int
	Failed   int
	Duration time.Duration
}

func NewPlaybookRunner(inv *inventory.Inventory, r *execrunner.Runner) *PlaybookRunner {
	return &PlaybookRunner{
		Inventory: inv,
		Runner:    r,
		Vars:      map[string]string{},
		Out:       os.Stdout,
	}
}

func (r *PlaybookRunner) Run(pb *Playbook) error {
	if pb == nil {
		return errors.New("playbook 不能为空")
	}
	if r == nil {
		return errors.New("PlaybookRunner 不能为空")
	}
	if r.Inventory == nil {
		return errors.New("inventory 不能为空")
	}
	if r.Runner == nil {
		return errors.New("runner 不能为空")
	}

	out := r.Out
	if out == nil {
		out = os.Stdout
	}

	finalVars := mergeVars(pb.Vars, r.Vars)
	hosts := inventory.FilterByGroup(r.Inventory.List(), pb.Hosts)
	if len(hosts) == 0 {
		return fmt.Errorf("分组 %s 中没有主机", pb.Hosts)
	}

	recap := make(map[string]*hostRecap, len(hosts))
	for _, h := range hosts {
		if h == nil {
			continue
		}
		recap[h.Name] = &hostRecap{}
	}

	register := map[string]map[string]any{}
	var runErr error

	for i := range pb.Tasks {
		task := pb.Tasks[i]
		display.PrintPlaybookTask(task.Name, len(pb.Tasks), i+1)

		command, err := Render(pb, &task, finalVars)
		if err != nil {
			runErr = fmt.Errorf("任务 %q 渲染失败: %w", task.Name, err)
			break
		}

		ok, err := evaluateWhen(task.When, finalVars, register)
		if err != nil {
			runErr = fmt.Errorf("任务 %q when 条件解析失败: %w", task.Name, err)
			break
		}
		if !ok {
			continue
		}

		jobs := make([]execrunner.Task, 0, len(hosts))
		for _, h := range hosts {
			if h == nil {
				continue
			}
			keyPath := strings.TrimSpace(h.KeyPath)
			if keyPath == "" {
				keyPath = strings.TrimSpace(r.KeyPath)
			}
			jobs = append(jobs, execrunner.Task{
				Host:     h,
				Command:  command,
				KeyPath:  keyPath,
				Password: strings.TrimSpace(r.Password),
			})
		}

		results := r.Runner.Run(jobs)
		taskFailed := false
		firstErr := error(nil)

		registerHosts := map[string]map[string]any{}
		registerExitCode := 0

		for _, res := range results {
			if res.Host == nil {
				taskFailed = true
				if firstErr == nil {
					firstErr = errors.New("返回结果缺少主机信息")
				}
				continue
			}

			hRecap, ok := recap[res.Host.Name]
			if !ok {
				hRecap = &hostRecap{}
				recap[res.Host.Name] = hRecap
			}
			hRecap.Duration += res.Duration

			if res.Error == nil && res.ExitCode == 0 {
				hRecap.OK++
			} else {
				hRecap.Failed++
				taskFailed = true
				if registerExitCode == 0 {
					if res.ExitCode != 0 {
						registerExitCode = res.ExitCode
					} else {
						registerExitCode = 1
					}
				}
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", res.Host.Name, res.Error)
				}
			}

			errText := ""
			if res.Error != nil {
				errText = res.Error.Error()
			}
			registerHosts[res.Host.Name] = map[string]any{
				"exit_code": res.ExitCode,
				"output":    res.Output,
				"error":     errText,
				"duration":  res.Duration.String(),
			}
		}

		if task.Register != "" {
			register[task.Register] = map[string]any{
				"exit_code": registerExitCode,
				"hosts":     registerHosts,
			}
		}

		if taskFailed && !task.IgnoreError {
			if firstErr == nil {
				firstErr = fmt.Errorf("任务 %q 执行失败", task.Name)
			}
			runErr = firstErr
			break
		}
	}

	printRecap(out, hosts, recap)
	return runErr
}

func mergeVars(low, high map[string]string) map[string]string {
	merged := make(map[string]string, len(low)+len(high))
	for k, v := range low {
		merged[k] = v
	}
	for k, v := range high {
		merged[k] = v
	}
	return merged
}

func evaluateWhen(expr string, vars map[string]string, register map[string]map[string]any) (bool, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return true, nil
	}

	parts := strings.SplitN(trimmed, "==", 2)
	if len(parts) != 2 {
		return false, fmt.Errorf("仅支持 \"==\" 条件表达式: %s", expr)
	}

	left, err := renderExpressionPart(parts[0], vars, register)
	if err != nil {
		return false, err
	}
	right, err := renderExpressionPart(parts[1], vars, register)
	if err != nil {
		return false, err
	}

	if ln, lErr := strconv.ParseFloat(left, 64); lErr == nil {
		if rn, rErr := strconv.ParseFloat(right, 64); rErr == nil {
			return ln == rn, nil
		}
	}
	return left == right, nil
}

func renderExpressionPart(part string, vars map[string]string, register map[string]map[string]any) (string, error) {
	raw := strings.TrimSpace(part)
	if raw == "" {
		return "", nil
	}
	raw = strings.Trim(raw, `"'`)
	if !strings.Contains(raw, "{{") {
		return raw, nil
	}

	tpl, err := template.New("when").Option("missingkey=error").Parse(raw)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	data := map[string]any{
		"vars":     vars,
		"register": register,
	}
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return strings.Trim(strings.TrimSpace(out.String()), `"'`), nil
}

func printRecap(_ io.Writer, hosts []*inventory.Host, recap map[string]*hostRecap) {
	results := make(map[string]*display.PlaybookHostResult, len(hosts))
	for _, h := range hosts {
		if h == nil {
			continue
		}
		item := recap[h.Name]
		if item == nil {
			item = &hostRecap{}
		}
		results[h.Name] = &display.PlaybookHostResult{
			HostName:    h.Name,
			OkCount:     item.OK,
			FailedCount: item.Failed,
			Duration:    item.Duration,
		}
	}
	display.PrintPlaybookRecap(results)
}
