package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/yourname/sshops/internal/audit"
	"github.com/yourname/sshops/internal/config"
	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/runner"
	sshclient "github.com/yourname/sshops/internal/ssh"
	"github.com/yourname/sshops/internal/vault"
)

var (
	execHost        string
	execPort        int
	execUser        string
	execKey         string
	execPassword    string
	execTimeout     int
	execProxy       string
	execGroup       string
	execTag         string
	execConcurrency int
	execRetry       int
	execLogLimit    int
)

var execCmd = &cobra.Command{
	Use:   "exec [flags] \"命令字符串\"",
	Short: "在远程主机执行命令",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		command := strings.TrimSpace(strings.Join(args, " "))
		if command == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供要执行的命令，例如：sshops exec --host 10.0.0.1 \"uname -a\"")
			os.Exit(1)
		}

		cfg := currentConfig()
		applyExecDefaults(cmd, cfg)
		keyFlag := cmd.Flags().Lookup("key")
		userSpecifiedKey := keyFlag != nil && keyFlag.Changed

		if strings.TrimSpace(execHost) != "" {
			execSingleHost(cmd, cfg, command)
			return
		}

		if strings.TrimSpace(execGroup) == "" && strings.TrimSpace(execTag) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供目标主机或分组，例如：sshops exec --host 10.0.0.1 \"uname -a\" 或 sshops exec --group prod \"uptime\"")
			os.Exit(1)
		}

		execBatchHosts(cfg, command, userSpecifiedKey)
	},
}

var execLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "查看审计日志",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := currentConfig()
		logger, err := audit.NewLogger(cfg.AuditDBPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 审计日志初始化失败")
			os.Exit(1)
		}
		defer logger.Close()

		logs, err := logger.Query(execLogLimit)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 查询审计日志失败")
			os.Exit(1)
		}

		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TIME\tHOST\tCOMMAND\tEXIT\tDURATION\tOPERATOR")
		for _, item := range logs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
				item.CreatedAt.Format("2006-01-02 15:04:05"),
				item.HostName,
				item.Command,
				item.ExitCode,
				fmt.Sprintf("%dms", item.DurationMS),
				item.Operator,
			)
		}
		_ = w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(execCmd)
	execCmd.AddCommand(execLogsCmd)

	execCmd.Flags().StringVarP(&execHost, "host", "H", "", "目标主机 IP 或域名")
	execCmd.Flags().IntVarP(&execPort, "port", "p", 0, "SSH 端口")
	execCmd.Flags().StringVarP(&execUser, "user", "u", "", "登录用户名")
	execCmd.Flags().StringVarP(&execKey, "key", "i", "", "SSH 私钥路径")
	execCmd.Flags().StringVar(&execPassword, "password", "", "SSH 密码")
	execCmd.Flags().IntVar(&execTimeout, "timeout", 0, "连接超时秒数")
	execCmd.Flags().StringVarP(&execProxy, "proxy", "P", "", "跳板机，格式 user@host:port，多跳用逗号分隔")
	execCmd.Flags().StringVarP(&execGroup, "group", "g", "", "按分组批量执行")
	execCmd.Flags().StringVar(&execTag, "tag", "", "按标签过滤批量执行（配合 --group 使用）")
	execCmd.Flags().IntVarP(&execConcurrency, "concurrency", "c", 10, "并发数")
	execCmd.Flags().IntVar(&execRetry, "retry", 0, "失败重试次数")

	execLogsCmd.Flags().IntVar(&execLogLimit, "limit", 20, "显示最近 N 条审计日志")
}

func execSingleHost(cmd *cobra.Command, cfg *config.Config, command string) {
	client := sshclient.NewClient(execHost, execPort, execUser, execTimeout)

	if execKey != "" {
		if err := client.WithKey(execKey); err != nil {
			fmt.Fprintln(os.Stderr, humanizeError(err, execHost, execPort, execTimeout, execKey))
			os.Exit(1)
		}
	} else if execPassword != "" {
		if err := client.WithPassword(execPassword); err != nil {
			fmt.Fprintln(os.Stderr, "✗ 认证设置失败：无法使用密码认证")
			os.Exit(1)
		}
	} else {
		fmt.Fprintln(os.Stderr, "✗ 认证失败：请提供 --key 或 --password")
		os.Exit(1)
	}

	proxies, err := parseProxyChain(execProxy, execUser, execKey, execPassword)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	client.Proxies = proxies

	if err := client.Connect(); err != nil {
		fmt.Fprintln(os.Stderr, humanizeError(err, execHost, execPort, execTimeout, execKey))
		os.Exit(1)
	}

	start := time.Now()
	exitCode, err := client.Run(command)
	duration := time.Since(start).Round(time.Millisecond)

	if err != nil {
		fmt.Fprintln(os.Stderr, humanizeError(err, execHost, execPort, execTimeout, execKey))
		os.Exit(1)
	}

	if exitCode == 0 {
		color.New(color.FgGreen).Printf("✓ exit %d  duration %s\n", exitCode, duration)
		os.Exit(0)
	}

	color.New(color.FgRed).Printf("✗ exit %d  duration %s\n", exitCode, duration)
	os.Exit(exitCode)
}

func execBatchHosts(cfg *config.Config, command string, userSpecifiedKey bool) {
	inv, err := inventory.Load(cfg.InventoryPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ 加载主机清单失败：请检查 inventory 文件格式")
		os.Exit(1)
	}

	hosts := inventory.FilterByGroupAndTags(inv.List(), execGroup, execTag)
	if len(hosts) == 0 {
		fmt.Fprintln(os.Stderr, "✗ 未找到匹配的主机，请检查分组或标签")
		os.Exit(1)
	}

	v := unlockVaultOptional(cfg.VaultPath)
	if v != nil {
		defer v.Lock()
	}

	tasks := make([]runner.Task, 0, len(hosts))
	for _, h := range hosts {
		if h == nil {
			continue
		}

		keyPath, password := resolveTaskAuth(cfg, h, findVaultCredential(v, h.Name), userSpecifiedKey)
		tasks = append(tasks, runner.Task{
			Host:     h,
			Command:  command,
			KeyPath:  keyPath,
			Password: password,
		})
	}

	display := runner.NewDisplay(hosts)
	display.Start()

	logger, logErr := audit.NewLogger(cfg.AuditDBPath)
	if logErr != nil {
		fmt.Fprintln(os.Stderr, "⚠ 审计日志初始化失败：已跳过日志记录")
	}
	if logger != nil {
		defer logger.Close()
	}

	r := runner.NewRunner(execConcurrency, execTimeout, execRetry)
	r.Progress = display
	r.Audit = logger

	startAll := time.Now()
	results := r.Run(tasks)
	display.Stop()
	duration := time.Since(startAll).Round(100 * time.Millisecond)

	total := len(results)
	success := 0
	failed := 0
	for _, res := range results {
		if res.Error == nil && res.ExitCode == 0 {
			success++
		} else {
			failed++
		}
	}

	if failed == 0 {
		fmt.Printf("✓ %d/%d 成功  耗时 %s\n", success, total, duration)
		os.Exit(0)
	}

	fmt.Printf("✓ %d/%d 成功  ✗ %d/%d 失败  耗时 %s\n", success, total, failed, total, duration)
	for _, res := range results {
		if res.Error == nil && res.ExitCode == 0 {
			continue
		}
		if res.Host == nil {
			continue
		}
		errText := humanizeError(res.Error, res.Host.Host, res.Host.Port, execTimeout, "")
		fmt.Printf("✗ %s (%s): %s\n", res.Host.Name, res.Host.Host, strings.TrimPrefix(errText, "✗ "))
	}
	os.Exit(1)
}

func resolveTaskAuth(cfg *config.Config, h *inventory.Host, cred *vault.Credential, userSpecifiedKey bool) (string, string) {
	keyPath := ""
	password := ""

	// KeyPath 优先级：
	// 1) --key
	// 2) vault.KeyPath
	// 3) inventory.Host.KeyPath
	// 4) config.DefaultKeyPath
	if userSpecifiedKey {
		keyPath = strings.TrimSpace(execKey)
	} else if cred != nil && strings.TrimSpace(cred.KeyPath) != "" {
		keyPath = strings.TrimSpace(cred.KeyPath)
	} else if h != nil && strings.TrimSpace(h.KeyPath) != "" {
		keyPath = strings.TrimSpace(h.KeyPath)
	} else {
		keyPath = strings.TrimSpace(cfg.DefaultKeyPath)
		if keyPath == "" {
			keyPath = "id_rsa"
		}
	}

	if cred != nil && strings.TrimSpace(cred.Password) != "" {
		password = strings.TrimSpace(cred.Password)
	}
	if password == "" && strings.TrimSpace(execPassword) != "" {
		password = strings.TrimSpace(execPassword)
	}

	return keyPath, password
}

func findVaultCredential(v *vault.Vault, name string) *vault.Credential {
	if v == nil {
		return nil
	}
	cred, err := v.Get(name)
	if err != nil {
		return nil
	}
	return cred
}

func unlockVaultOptional(path string) *vault.Vault {
	v := vault.NewVault(path)
	master, err := readSecret("请输入 Vault 主密码：")
	if err != nil {
		fmt.Fprintln(os.Stderr, "⚠ Vault 解锁失败：无法读取主密码，已跳过")
		return nil
	}
	if strings.TrimSpace(master) == "" {
		return nil
	}
	if err := v.Unlock(master); err != nil {
		fmt.Fprintln(os.Stderr, "⚠ Vault 解锁失败：已跳过 Vault 凭据")
		return nil
	}
	return v
}

func applyExecDefaults(cmd *cobra.Command, cfg *config.Config) {
	passwordProvided := cmd.Flags().Changed("password") && strings.TrimSpace(execPassword) != ""
	keyProvided := cmd.Flags().Changed("key") && strings.TrimSpace(execKey) != ""

	if !cmd.Flags().Changed("port") || execPort <= 0 {
		execPort = cfg.DefaultPort
	}
	if !cmd.Flags().Changed("user") || strings.TrimSpace(execUser) == "" {
		execUser = cfg.DefaultUser
	}
	if !cmd.Flags().Changed("timeout") || execTimeout <= 0 {
		execTimeout = cfg.ConnectTimeout
	}
	if keyProvided {
		return
	}
	if passwordProvided {
		execKey = ""
		return
	}
	if strings.TrimSpace(execKey) == "" {
		execKey = cfg.DefaultKeyPath
	}
}

func humanizeError(err error, host string, port int, timeout int, keyPath string) string {
	if err == nil {
		return ""
	}
	if hopErr, ok := sshclient.IsProxyHopError(err); ok {
		return fmt.Sprintf("✗ 跳板机连接失败（第%d跳 %s）：%s", hopErr.Hop, hopErr.Node, proxyReasonToChinese(hopErr.Reason))
	}

	switch {
	case errors.Is(err, sshclient.ErrPrivateKeyNotFound):
		return fmt.Sprintf("✗ 私钥文件不存在：%s", keyPath)
	case errors.Is(err, sshclient.ErrAuthFailed):
		return "✗ 认证失败：用户名或密钥不正确"
	case errors.Is(err, sshclient.ErrConnectTimeout):
		return fmt.Sprintf("✗ 连接超时：%ds 内无法连接到 %s", timeout, host)
	case errors.Is(err, sshclient.ErrConnectionRefused):
		return fmt.Sprintf("✗ 连接失败：主机 %s:%d 拒绝连接，请检查 SSH 服务是否运行", host, port)
	case errors.Is(err, sshclient.ErrInvalidPrivateKey):
		return "✗ 私钥解析失败：请检查私钥格式或口令是否正确"
	case errors.Is(err, sshclient.ErrCommandRunFailed):
		return "✗ 命令执行失败：远程会话异常中断"
	case errors.Is(err, sshclient.ErrConnectFailed):
		return "✗ 连接失败：请检查网络连通性和主机地址配置"
	default:
		return "✗ " + err.Error()
	}
}

func proxyReasonToChinese(err error) string {
	switch {
	case errors.Is(err, sshclient.ErrAuthFailed):
		return "认证失败"
	case errors.Is(err, sshclient.ErrConnectTimeout):
		return "连接超时"
	case errors.Is(err, sshclient.ErrConnectionRefused):
		return "连接被拒绝"
	default:
		return "连接失败"
	}
}

func parseProxyChain(raw string, defaultUser string, keyPath string, password string) ([]sshclient.ProxyConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	segments := strings.Split(trimmed, ",")
	proxies := make([]sshclient.ProxyConfig, 0, len(segments))
	for _, seg := range segments {
		item := strings.TrimSpace(seg)
		if item == "" {
			return nil, errors.New("✗ 跳板机参数格式错误：存在空的跳板机配置")
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
		if user == "" {
			return nil, fmt.Errorf("✗ 跳板机参数格式错误：%s 缺少用户名", item)
		}

		host, port, err := splitHostPortWithDefault(hostPort, 22)
		if err != nil {
			return nil, fmt.Errorf("✗ 跳板机参数格式错误：%s", item)
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

func splitHostPortWithDefault(hostPort string, defaultPort int) (string, int, error) {
	value := strings.TrimSpace(hostPort)
	if value == "" {
		return "", 0, errors.New("empty host")
	}

	if strings.Contains(value, ":") {
		host, portStr, err := net.SplitHostPort(value)
		if err == nil {
			port, parseErr := strconv.Atoi(portStr)
			if parseErr != nil || port <= 0 {
				return "", 0, errors.New("invalid port")
			}
			if strings.TrimSpace(host) == "" {
				return "", 0, errors.New("invalid host")
			}
			return host, port, nil
		}

		lastColon := strings.LastIndex(value, ":")
		if lastColon <= 0 || lastColon >= len(value)-1 {
			return "", 0, errors.New("invalid host:port")
		}
		host = strings.TrimSpace(value[:lastColon])
		portStr = strings.TrimSpace(value[lastColon+1:])
		port, parseErr := strconv.Atoi(portStr)
		if parseErr != nil || port <= 0 {
			return "", 0, errors.New("invalid port")
		}
		if host == "" {
			return "", 0, errors.New("invalid host")
		}
		return host, port, nil
	}

	return value, defaultPort, nil
}
