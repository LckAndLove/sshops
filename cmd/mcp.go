package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/mcp"
	"github.com/yourname/sshops/internal/vault"
)

var (
	mcpTransport     string
	mcpPort          int
	mcpVaultPassword string
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP Server",
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动 MCP 服务",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := currentConfig()

		inv, err := inventory.Load(cfg.InventoryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 加载 inventory 失败")
			os.Exit(1)
		}

		v := vault.NewVault(cfg.VaultPath)
		if strings.TrimSpace(mcpVaultPassword) != "" {
			if err := v.Unlock(mcpVaultPassword); err != nil {
				fmt.Fprintln(os.Stderr, "⚠ Vault 解锁失败，已跳过 vault")
			}
		}

		server := mcp.NewServer(inv, v, cfg)

		switch strings.ToLower(strings.TrimSpace(mcpTransport)) {
		case "", "stdio":
			mcp.RunStdio(server)
		case "sse":
			mcp.RunSSE(server, mcpPort)
		default:
			fmt.Fprintln(os.Stderr, "✗ 不支持的 transport，请使用 stdio 或 sse")
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.AddCommand(mcpServeCmd)

	mcpServeCmd.Flags().StringVarP(&mcpTransport, "transport", "t", "stdio", "传输方式：stdio 或 sse")
	mcpServeCmd.Flags().IntVar(&mcpPort, "port", 3000, "SSE 模式端口")
	mcpServeCmd.Flags().StringVar(&mcpVaultPassword, "vault-password", "", "Vault 主密码")
}
