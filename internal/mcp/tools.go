package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/playbook"
	execrunner "github.com/yourname/sshops/internal/runner"
	sshclient "github.com/yourname/sshops/internal/ssh"
)

func (s *Server) buildToolDefs() []ToolDef {
	return []ToolDef{
		{
			Name:        "exec_command",
			Description: "在远程服务器上执行 shell 命令，返回输出结果",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host":    map[string]interface{}{"type": "string", "description": "主机名称（inventory 中的 name）或 IP"},
					"command": map[string]interface{}{"type": "string", "description": "要执行的 shell 命令"},
					"timeout": map[string]interface{}{"type": "integer", "description": "超时秒数，默认 30"},
				},
				"required": []string{"host", "command"},
			},
		},
		{
			Name:        "upload_file",
			Description: "通过 SFTP 上传本地文件到远程服务器",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host":        map[string]interface{}{"type": "string", "description": "主机名称或 IP"},
					"local_path":  map[string]interface{}{"type": "string", "description": "本地文件路径"},
					"remote_path": map[string]interface{}{"type": "string", "description": "远程目标路径"},
				},
				"required": []string{"host", "local_path", "remote_path"},
			},
		},
		{
			Name:        "download_file",
			Description: "通过 SFTP 从远程服务器下载文件到本地",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host":        map[string]interface{}{"type": "string", "description": "主机名称或 IP"},
					"local_path":  map[string]interface{}{"type": "string", "description": "本地目标路径"},
					"remote_path": map[string]interface{}{"type": "string", "description": "远程文件路径"},
				},
				"required": []string{"host", "local_path", "remote_path"},
			},
		},
		{
			Name:        "list_servers",
			Description: "列出主机清单，支持按分组和标签过滤",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"group": map[string]interface{}{"type": "string", "description": "分组名称，为空则返回所有"},
					"tag":   map[string]interface{}{"type": "string", "description": "标签过滤，格式 key=value,key=value"},
				},
			},
		},
		{
			Name:        "get_metrics",
			Description: "采集远程服务器的系统指标",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host": map[string]interface{}{"type": "string", "description": "主机名称或 IP"},
				},
				"required": []string{"host"},
			},
		},
		{
			Name:        "run_playbook",
			Description: "执行 Playbook 文件，完成多步骤运维任务",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string", "description": "Playbook 文件名或路径"},
					"vars": map[string]interface{}{"type": "object", "description": "Playbook 变量覆盖"},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "check_service",
			Description: "检查远程主机服务状态",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host":    map[string]interface{}{"type": "string", "description": "主机名称（inventory 中的 name）或 IP"},
					"service": map[string]interface{}{"type": "string", "description": "服务名称"},
				},
				"required": []string{"host", "service"},
			},
		},
		{
			Name:        "tail_log",
			Description: "查看远程日志文件末尾内容",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"host":  map[string]interface{}{"type": "string", "description": "主机名称（inventory 中的 name）或 IP"},
					"path":  map[string]interface{}{"type": "string", "description": "远程日志文件路径"},
					"lines": map[string]interface{}{"type": "integer", "description": "返回行数，默认 50"},
				},
				"required": []string{"host", "path"},
			},
		},
		{
			Name:        "batch_exec",
			Description: "在多台主机上并发执行命令",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"group":       map[string]interface{}{"type": "string", "description": "按分组过滤主机"},
					"tag":         map[string]interface{}{"type": "string", "description": "按标签过滤主机，格式 key=value,key=value"},
					"command":     map[string]interface{}{"type": "string", "description": "要执行的 shell 命令"},
					"concurrency": map[string]interface{}{"type": "integer", "description": "并发数，默认 10"},
					"timeout":     map[string]interface{}{"type": "integer", "description": "每台主机超时秒数，默认 connect_timeout"},
				},
				"required": []string{"command"},
			},
		},
	}
}

func (s *Server) callTool(name string, args map[string]interface{}) (string, error) {
	switch name {
	case "exec_command":
		return s.toolExecCommand(args)
	case "upload_file":
		return s.toolUploadFile(args)
	case "download_file":
		return s.toolDownloadFile(args)
	case "list_servers":
		return s.toolListServers(args)
	case "get_metrics":
		return s.toolGetMetrics(args)
	case "run_playbook":
		return s.toolRunPlaybook(args)
	case "check_service":
		return s.toolCheckService(args)
	case "tail_log":
		return s.toolTailLog(args)
	case "batch_exec":
		return s.toolBatchExec(args)
	default:
		return "", fmt.Errorf("未知 tool: %s", name)
	}
}

func (s *Server) toolExecCommand(args map[string]interface{}) (string, error) {
	hostArg := getStringArg(args, "host")
	command := getStringArg(args, "command")
	if hostArg == "" || command == "" {
		return "", errors.New("host 和 command 为必填参数")
	}

	timeout := getIntArg(args, "timeout", 30)
	h, err := s.resolveHost(hostArg)
	if err != nil {
		return "", err
	}

	client, err := s.openClient(h, timeout)
	if err != nil {
		return "", err
	}
	defer client.Close()

	stdout, stderr, exitCode, duration, err := runCommandSilent(client, command, timeout)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(stdout) == "" {
		stdout = "（无）"
	}
	if strings.TrimSpace(stderr) == "" {
		stderr = "（无）"
	}

	result := fmt.Sprintf("exit_code: %d\nduration: %s\n\n[stdout]\n%s\n\n[stderr]\n%s", exitCode, duration.Round(time.Millisecond), stdout, stderr)
	if hasDangerousPattern(command) {
		result += "\n⚠ 警告：检测到高危命令模式，请确认操作意图"
	}
	return result, nil
}

func (s *Server) toolUploadFile(args map[string]interface{}) (string, error) {
	hostArg := getStringArg(args, "host")
	localPath := getStringArg(args, "local_path")
	remotePath := getStringArg(args, "remote_path")
	if hostArg == "" || localPath == "" || remotePath == "" {
		return "", errors.New("host、local_path、remote_path 为必填参数")
	}

	h, err := s.resolveHost(hostArg)
	if err != nil {
		return "", err
	}

	client, sftpClient, err := s.openSFTPClient(h)
	if err != nil {
		return "", err
	}
	defer client.Close()
	defer sftpClient.Close()

	in, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer in.Close()

	if err := sftpClient.MkdirAll(path.Dir(remotePath)); err != nil {
		return "", err
	}
	out, err := sftpClient.Create(remotePath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	written, err := io.Copy(out, in)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("已上传 %s -> %s:%s (%d bytes)", localPath, h.Host, remotePath, written), nil
}

func (s *Server) toolDownloadFile(args map[string]interface{}) (string, error) {
	hostArg := getStringArg(args, "host")
	localPath := getStringArg(args, "local_path")
	remotePath := getStringArg(args, "remote_path")
	if hostArg == "" || localPath == "" || remotePath == "" {
		return "", errors.New("host、local_path、remote_path 为必填参数")
	}

	h, err := s.resolveHost(hostArg)
	if err != nil {
		return "", err
	}

	client, sftpClient, err := s.openSFTPClient(h)
	if err != nil {
		return "", err
	}
	defer client.Close()
	defer sftpClient.Close()

	in, err := sftpClient.Open(remotePath)
	if err != nil {
		return "", err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return "", err
	}
	out, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	written, err := io.Copy(out, in)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("已下载 %s:%s -> %s (%d bytes)", h.Host, remotePath, localPath, written), nil
}

func (s *Server) toolListServers(args map[string]interface{}) (string, error) {
	group := getStringArg(args, "group")
	tag := getStringArg(args, "tag")
	if s.inventory == nil {
		return "[]", nil
	}
	hosts := inventory.FilterByGroupAndTags(s.inventory.List(), group, tag)
	data, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Server) toolGetMetrics(args map[string]interface{}) (string, error) {
	hostArg := getStringArg(args, "host")
	if hostArg == "" {
		return "", errors.New("host 为必填参数")
	}

	h, err := s.resolveHost(hostArg)
	if err != nil {
		return "", err
	}

	metricsCmds := []struct {
		label string
		cmd   string
	}{
		{"CPU", "top -bn1 | grep \"Cpu(s)\" | awk '{print $2}'"},
		{"MEM", "free -h | awk '/^Mem:/{print $2,$3,$4}'"},
		{"DISK", "df -h | grep -v tmpfs"},
		{"LOAD", "uptime"},
		{"PROC", "ps aux | wc -l"},
	}

	values := map[string]string{}
	for _, item := range metricsCmds {
		out, err := s.toolExecCommand(map[string]interface{}{"host": h.Name, "command": item.cmd, "timeout": 30})
		if err != nil {
			return "", err
		}
		values[item.label] = out
	}

	return fmt.Sprintf("=== 系统指标 %s (%s) ===\nCPU 使用率:  %s\n内存:        %s\n磁盘:\n%s\n负载:       %s\n进程数:     %s",
		h.Name,
		h.Host,
		extractStdout(values["CPU"]),
		extractStdout(values["MEM"]),
		extractStdout(values["DISK"]),
		extractStdout(values["LOAD"]),
		extractStdout(values["PROC"]),
	), nil
}

func (s *Server) toolRunPlaybook(args map[string]interface{}) (string, error) {
	name := getStringArg(args, "name")
	if name == "" {
		return "", errors.New("name 为必填参数")
	}
	if s.inventory == nil {
		return "", errors.New("inventory 未初始化")
	}

	vars, err := getStringMapArg(args, "vars")
	if err != nil {
		return "", err
	}

	pb, err := playbook.Load(name)
	if err != nil {
		return "", err
	}

	timeout := s.config.ConnectTimeout
	if timeout <= 0 {
		timeout = 30
	}
	r := playbook.NewPlaybookRunner(s.inventory, execrunner.NewRunner(10, timeout, 0))
	r.KeyPath = strings.TrimSpace(s.config.DefaultKeyPath)
	r.Vars = vars

	var out bytes.Buffer
	r.Out = &out
	if err := r.Run(pb); err != nil {
		prefix := strings.TrimSpace(out.String())
		if prefix == "" {
			return "", err
		}
		return "", fmt.Errorf("%s\n\n%s", prefix, err)
	}

	text := strings.TrimSpace(out.String())
	if text == "" {
		return "Playbook 执行完成", nil
	}
	return text, nil
}

func (s *Server) toolCheckService(args map[string]interface{}) (string, error) {
	hostArg := getStringArg(args, "host")
	service := getStringArg(args, "service")
	if hostArg == "" || service == "" {
		return "", errors.New("host 和 service 为必填参数")
	}

	command := fmt.Sprintf(
		"if command -v systemctl >/dev/null 2>&1; then systemctl is-active %s; systemctl status %s --no-pager -l | tail -n 80; elif command -v service >/dev/null 2>&1; then service %s status; else ps -ef | grep -F %s | grep -v grep; fi",
		shellQuote(service),
		shellQuote(service),
		shellQuote(service),
		shellQuote(service),
	)
	return s.toolExecCommand(map[string]interface{}{
		"host":    hostArg,
		"command": command,
		"timeout": 30,
	})
}

func (s *Server) toolTailLog(args map[string]interface{}) (string, error) {
	hostArg := getStringArg(args, "host")
	logPath := getStringArg(args, "path")
	if hostArg == "" || logPath == "" {
		return "", errors.New("host 和 path 为必填参数")
	}

	lines := getIntArg(args, "lines", 50)
	if lines <= 0 {
		lines = 50
	}

	command := fmt.Sprintf("tail -n %d %s", lines, shellQuote(logPath))
	return s.toolExecCommand(map[string]interface{}{
		"host":    hostArg,
		"command": command,
		"timeout": 30,
	})
}

func (s *Server) toolBatchExec(args map[string]interface{}) (string, error) {
	command := getStringArg(args, "command")
	if command == "" {
		return "", errors.New("command 为必填参数")
	}
	if s.inventory == nil {
		return "", errors.New("inventory 未初始化")
	}

	group := getStringArg(args, "group")
	tag := getStringArg(args, "tag")
	hosts := inventory.FilterByGroupAndTags(s.inventory.List(), group, tag)
	if len(hosts) == 0 {
		return "", errors.New("未找到匹配的主机")
	}

	concurrency := getIntArg(args, "concurrency", 10)
	if concurrency <= 0 {
		concurrency = 10
	}
	timeout := getIntArg(args, "timeout", s.config.ConnectTimeout)
	if timeout <= 0 {
		timeout = 30
	}

	tasks := make([]execrunner.Task, 0, len(hosts))
	for _, h := range hosts {
		if h == nil {
			continue
		}
		keyPath, password := s.resolveCredential(h)
		tasks = append(tasks, execrunner.Task{
			Host:     h,
			Command:  command,
			KeyPath:  keyPath,
			Password: password,
		})
	}
	if len(tasks) == 0 {
		return "", errors.New("没有可执行的主机任务")
	}

	r := execrunner.NewRunner(concurrency, timeout, 0)
	start := time.Now()
	results := r.Run(tasks)
	duration := time.Since(start).Round(100 * time.Millisecond)

	success := 0
	failed := 0
	lines := make([]string, 0, len(results)+1)
	for _, res := range results {
		if res.Host == nil {
			failed++
			lines = append(lines, "[FAIL] unknown: 结果缺少主机信息")
			continue
		}
		if res.Error == nil && res.ExitCode == 0 {
			success++
			lines = append(lines, fmt.Sprintf("[OK] %s (%s) exit=%d duration=%s", res.Host.Name, res.Host.Host, res.ExitCode, res.Duration.Round(time.Millisecond)))
			continue
		}

		failed++
		errText := "命令执行失败"
		if res.Error != nil {
			errText = strings.TrimSpace(res.Error.Error())
		}
		lines = append(lines, fmt.Sprintf("[FAIL] %s (%s) exit=%d duration=%s error=%s",
			res.Host.Name,
			res.Host.Host,
			res.ExitCode,
			res.Duration.Round(time.Millisecond),
			errText,
		))
	}

	header := fmt.Sprintf("batch_exec 完成: total=%d success=%d failed=%d duration=%s", len(results), success, failed, duration)
	return header + "\n" + strings.Join(lines, "\n"), nil
}

func extractStdout(execResult string) string {
	marker := "[stdout]"
	idx := strings.Index(execResult, marker)
	if idx < 0 {
		return strings.TrimSpace(execResult)
	}
	part := execResult[idx+len(marker):]
	stderrIdx := strings.Index(part, "[stderr]")
	if stderrIdx >= 0 {
		part = part[:stderrIdx]
	}
	return strings.TrimSpace(part)
}

func (s *Server) resolveHost(hostArg string) (*inventory.Host, error) {
	hostArg = strings.TrimSpace(hostArg)
	if hostArg == "" {
		return nil, errors.New("host 不能为空")
	}

	if s.inventory != nil {
		if h, err := s.inventory.Get(hostArg); err == nil {
			return h, nil
		}
	}

	return &inventory.Host{
		Name: hostArg,
		Host: hostArg,
		Port: s.config.DefaultPort,
		User: s.config.DefaultUser,
	}, nil
}

func (s *Server) openClient(h *inventory.Host, timeout int) (*sshclient.Client, error) {
	if h == nil {
		return nil, errors.New("主机信息为空")
	}
	if timeout <= 0 {
		timeout = 30
	}

	port := h.Port
	if port <= 0 {
		port = s.config.DefaultPort
	}
	user := strings.TrimSpace(h.User)
	if user == "" {
		user = s.config.DefaultUser
	}

	keyPath, password := s.resolveCredential(h)
	client := sshclient.NewClient(h.Host, port, user, timeout)
	if strings.TrimSpace(keyPath) != "" {
		if err := client.WithKey(keyPath); err != nil {
			if strings.TrimSpace(password) != "" {
				if pErr := client.WithPassword(password); pErr != nil {
					return nil, err
				}
			} else {
				return nil, err
			}
		}
	} else if strings.TrimSpace(password) != "" {
		if err := client.WithPassword(password); err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("无法解析认证信息")
	}

	if strings.TrimSpace(h.ProxyChain) != "" {
		proxies, _ := parseMCPProxyChain(h.ProxyChain, user, keyPath, password)
		client.Proxies = proxies
	}

	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("连接失败: %w", err)
	}
	return client, nil
}

func (s *Server) openSFTPClient(h *inventory.Host) (*sshclient.Client, *sftp.Client, error) {
	client, err := s.openClient(h, 30)
	if err != nil {
		return nil, nil, err
	}

	raw := client.Raw()
	if raw == nil {
		client.CloseForce()
		return nil, nil, errors.New("SSH 会话不可用")
	}

	sftpClient, err := sftp.NewClient(raw)
	if err != nil {
		client.CloseForce()
		return nil, nil, err
	}
	return client, sftpClient, nil
}

func (s *Server) resolveCredential(h *inventory.Host) (string, string) {
	keyPath := ""
	password := ""

	if s.vault != nil && h != nil {
		if cred, err := s.vault.Get(h.Name); err == nil && cred != nil {
			if strings.TrimSpace(cred.KeyPath) != "" {
				keyPath = strings.TrimSpace(cred.KeyPath)
			}
			if strings.TrimSpace(cred.Password) != "" {
				password = strings.TrimSpace(cred.Password)
			}
		}
	}

	if keyPath == "" && h != nil && strings.TrimSpace(h.KeyPath) != "" {
		keyPath = strings.TrimSpace(h.KeyPath)
	}
	if keyPath == "" {
		keyPath = strings.TrimSpace(s.config.DefaultKeyPath)
	}

	return keyPath, password
}

func runCommandSilent(client *sshclient.Client, command string, timeout int) (stdout, stderr string, exitCode int, duration time.Duration, err error) {
	if client == nil || client.Raw() == nil {
		return "", "", 1, 0, errors.New("SSH 连接不可用")
	}
	sess, err := client.Raw().NewSession()
	if err != nil {
		return "", "", 1, 0, err
	}
	defer sess.Close()
	defer client.Close()

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- sess.Run(command)
	}()

	if timeout <= 0 {
		timeout = 30
	}

	select {
	case runErr := <-done:
		duration = time.Since(start)
		stdout = outBuf.String()
		stderr = errBuf.String()
		if runErr == nil {
			return stdout, stderr, 0, duration, nil
		}
		var exitErr interface{ ExitStatus() int }
		if errors.As(runErr, &exitErr) {
			return stdout, stderr, exitErr.ExitStatus(), duration, nil
		}
		return stdout, stderr, 1, duration, runErr
	case <-time.After(time.Duration(timeout) * time.Second):
		duration = time.Since(start)
		client.CloseForce()
		return outBuf.String(), errBuf.String(), 1, duration, fmt.Errorf("连接或执行超时：%ds", timeout)
	}
}

func getStringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func getIntArg(args map[string]interface{}, key string, def int) int {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return def
		}
		return n
	default:
		return def
	}
}

func getStringMapArg(args map[string]interface{}, key string) (map[string]string, error) {
	result := map[string]string{}
	if args == nil {
		return result, nil
	}
	v, ok := args[key]
	if !ok || v == nil {
		return result, nil
	}

	switch t := v.(type) {
	case map[string]string:
		for k, val := range t {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			result[k] = strings.TrimSpace(val)
		}
		return result, nil
	case map[string]interface{}:
		for k, val := range t {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			result[k] = strings.TrimSpace(fmt.Sprintf("%v", val))
		}
		return result, nil
	default:
		return nil, fmt.Errorf("%s 必须是 object", key)
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func parseMCPProxyChain(raw string, defaultUser string, keyPath string, password string) ([]sshclient.ProxyConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	segments := strings.Split(trimmed, ",")
	proxies := make([]sshclient.ProxyConfig, 0, len(segments))
	for _, seg := range segments {
		item := strings.TrimSpace(seg)
		if item == "" {
			return nil, errors.New("跳板机参数格式错误：存在空项")
		}

		user := defaultUser
		hostPort := item
		if strings.Contains(item, "@") {
			parts := strings.SplitN(item, "@", 2)
			if strings.TrimSpace(parts[0]) != "" {
				user = strings.TrimSpace(parts[0])
			}
			hostPort = strings.TrimSpace(parts[1])
		}

		host := hostPort
		port := 22
		if strings.Contains(hostPort, ":") {
			idx := strings.LastIndex(hostPort, ":")
			if idx <= 0 || idx >= len(hostPort)-1 {
				return nil, errors.New("跳板机参数格式错误：host:port 无效")
			}
			host = strings.TrimSpace(hostPort[:idx])
			p, err := strconv.Atoi(strings.TrimSpace(hostPort[idx+1:]))
			if err != nil || p <= 0 {
				return nil, errors.New("跳板机参数格式错误：端口无效")
			}
			port = p
		}

		proxies = append(proxies, sshclient.ProxyConfig{
			Host:     host,
			Port:     port,
			User:     user,
			KeyPath:  keyPath,
			Password: password,
		})
	}
	return proxies, nil
}

func hasDangerousPattern(command string) bool {
	cmd := strings.ToLower(command)
	patterns := []string{
		"rm -rf /",
		"> /dev/sda",
		"dd if=/dev/zero",
	}
	for _, p := range patterns {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	return false
}
