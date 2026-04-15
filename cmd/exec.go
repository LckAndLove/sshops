package cmd

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/yourname/sshops/internal/config"
	"github.com/yourname/sshops/internal/inventory"
	sshclient "github.com/yourname/sshops/internal/ssh"
	"github.com/yourname/sshops/internal/vault"
)

var (
	execHost     string
	execPort     int
	execUser     string
	execKey      string
	execPassword string
	execTimeout  int
	execProxy    string
	execGroup    string
	execTag      string
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

		if strings.TrimSpace(execHost) != "" {
			execSingleHost(cmd, cfg, command)
			return
		}

		if strings.TrimSpace(execGroup) == "" && strings.TrimSpace(execTag) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供目标主机或分组，例如：sshops exec --host 10.0.0.1 \"uname -a\" 或 sshops exec --group prod \"uptime\"")
			os.Exit(1)
		}

		execBatchHosts(cmd, cfg, command)
	},
}

func init() {
	rootCmd.AddCommand(execCmd)

	execCmd.Flags().StringVarP(&execHost, "host", "H", "", "目标主机 IP 或域名")
	execCmd.Flags().IntVarP(&execPort, "port", "p", 0, "SSH 端口")
	execCmd.Flags().StringVarP(&execUser, "user", "u", "", "登录用户名")
	execCmd.Flags().StringVarP(&execKey, "key", "i", "", "SSH 私钥路径")
	execCmd.Flags().StringVar(&execPassword, "password", "", "SSH 密码")
	execCmd.Flags().IntVar(&execTimeout, "timeout", 0, "连接超时秒数")
	execCmd.Flags().StringVarP(&execProxy, "proxy", "P", "", "跳板机，格式 user@host:port，多跳用逗号分隔")
	execCmd.Flags().StringVarP(&execGroup, "group", "g", "", "按分组批量执行")
	execCmd.Flags().StringVar(&execTag, "tag", "", "按标签过滤批量执行（配合 --group 使用）")
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

func execBatchHosts(cmd *cobra.Command, cfg *config.Config, command string) {
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

	total := len(hosts)
	success := 0
	failed := 0
	startAll := time.Now()

	for _, h := range hosts {
		if h == nil {
			continue
		}

		fmt.Printf("--- [%s] %s ---\n", h.Name, h.Host)

		hostPort := h.Port
		if hostPort <= 0 {
			hostPort = cfg.DefaultPort
		}
		hostUser := strings.TrimSpace(h.User)
		if hostUser == "" {
			hostUser = cfg.DefaultUser
		}
		hostTimeout := execTimeout
		if hostTimeout <= 0 {
			hostTimeout = cfg.ConnectTimeout
		}

		cred := findVaultCredential(v, h.Name)
		keyPath, password := resolveBatchAuth(cmd, cfg, h, cred)
		proxyChain := strings.TrimSpace(h.ProxyChain)
		if cmd.Flags().Changed("proxy") {
			proxyChain = strings.TrimSpace(execProxy)
		}

		client := sshclient.NewClient(h.Host, hostPort, hostUser, hostTimeout)
		proxies, proxyErr := parseProxyChain(proxyChain, hostUser, keyPath, password)
		if proxyErr != nil {
			fmt.Fprintf(os.Stderr, "%s\n", proxyErr.Error())
			failed++
			continue
		}
		client.Proxies = proxies

		authErr := configureClientAuth(client, keyPath, password)
		if authErr != nil {
			fmt.Fprintf(os.Stderr, "%s\n", humanizeError(authErr, h.Host, hostPort, hostTimeout, keyPath))
			failed++
			continue
		}

		if err := client.Connect(); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", humanizeError(err, h.Host, hostPort, hostTimeout, keyPath))
			failed++
			continue
		}

		exitCode, runErr := client.RunWithPrefix(command, h.Name)
		if runErr != nil {
			fmt.Fprintf(os.Stderr, "%s\n", humanizeError(runErr, h.Host, hostPort, hostTimeout, keyPath))
			failed++
			continue
		}
		if exitCode == 0 {
			success++
		} else {
			failed++
		}
	}

	duration := time.Since(startAll).Round(100 * time.Millisecond)
	if failed == 0 {
		fmt.Printf("✓ %d/%d 成功  耗时 %s\n", success, total, duration)
		os.Exit(0)
	}

	fmt.Printf("✓ %d/%d 成功  ✗ %d/%d 失败  耗时 %s\n", success, total, failed, total, duration)
	os.Exit(1)
}

func resolveBatchAuth(cmd *cobra.Command, cfg *config.Config, h *inventory.Host, cred *vault.Credential) (keyPath string, password string) {
	if cmd.Flags().Changed("key") && strings.TrimSpace(execKey) != "" {
		keyPath = strings.TrimSpace(execKey)
	} else {
		keyPath = strings.TrimSpace(h.KeyPath)
		if keyPath == "" && cred != nil && strings.TrimSpace(cred.KeyPath) != "" {
			keyPath = strings.TrimSpace(cred.KeyPath)
		}
	}

	if cmd.Flags().Changed("password") && strings.TrimSpace(execPassword) != "" {
		password = strings.TrimSpace(execPassword)
	} else if cred != nil && keyPath == "" && strings.TrimSpace(cred.Password) != "" {
		password = strings.TrimSpace(cred.Password)
	}

	if keyPath == "" && password == "" {
		keyPath = cfg.DefaultKeyPath
	}
	return keyPath, password
}

func configureClientAuth(client *sshclient.Client, keyPath string, password string) error {
	if strings.TrimSpace(keyPath) != "" {
		return client.WithKey(keyPath)
	}
	if strings.TrimSpace(password) != "" {
		return client.WithPassword(password)
	}
	return sshclient.ErrAuthFailed
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
		return "✗ 操作失败：请检查网络、认证信息和主机配置"
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
