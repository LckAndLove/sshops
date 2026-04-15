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
