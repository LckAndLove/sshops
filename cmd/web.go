package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/yourname/sshops/internal/audit"
	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/vault"
	"github.com/yourname/sshops/internal/web"
)

var (
	portFlag     int
	passwordFlag string
	openFlag     bool
)

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Web 服务",
}

var webServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "启动 Web UI",
	Run: func(cmd *cobra.Command, args []string) {
		// 加载组件
		cfg := currentConfig()

		inv, err := inventory.Load(cfg.InventoryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 加载 inventory 失败")
			return
		}

		v := vault.NewVault(cfg.VaultPath)

		auditLog, err := audit.NewLogger(cfg.AuditDBPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ 初始化审计日志失败: %v\n", err)
			return
		}

		server := web.NewWebServer(inv, v, cfg, auditLog, passwordFlag)

		url := fmt.Sprintf("http://localhost:%d", portFlag)
		fmt.Println("sshops Web UI 已启动")
		fmt.Printf("访问地址：%s\n", url)
		fmt.Println("按 Ctrl+C 停止服务")

		if openFlag {
			var err error
			switch runtime.GOOS {
			case "windows":
				err = exec.Command("cmd", "/c", "start", url).Start()
			case "darwin":
				err = exec.Command("open", url).Start()
			case "linux":
				err = exec.Command("xdg-open", url).Start()
			}
			if err != nil {
				fmt.Printf("打开浏览器失败: %v\n", err)
			}
		}

		server.Start(portFlag)
	},
}

func init() {
	webCmd.AddCommand(webServeCmd)
	rootCmd.AddCommand(webCmd)
	webServeCmd.Flags().IntVarP(&portFlag, "port", "p", 8080, "监听端口")
	webServeCmd.Flags().StringVar(&passwordFlag, "password", "", "访问密码")
	webServeCmd.Flags().BoolVarP(&openFlag, "open", "", false, "启动后打开浏览器")
}
