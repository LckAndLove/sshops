package cmd

import (
	"errors"
	"fmt"
	"os"
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
