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
	sshclient "github.com/yourname/sshops/internal/ssh"
)

var (
	execHost     string
	execPort     int
	execUser     string
	execKey      string
	execPassword string
	execTimeout  int
	execProxy    string
)

var execCmd = &cobra.Command{
	Use:   "exec [flags] \"命令字符串\"",
	Short: "在远程主机执行命令",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		command := strings.TrimSpace(strings.Join(args, " "))
		if strings.TrimSpace(execHost) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供目标主机，例如：sshops exec --host 10.0.0.1 \"uname -a\"")
			os.Exit(1)
		}
		if command == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供要执行的命令，例如：sshops exec --host 10.0.0.1 \"uname -a\"")
			os.Exit(1)
		}

		cfg := currentConfig()
		applyExecDefaults(cmd, cfg)

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
