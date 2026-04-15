package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/yourname/sshops/internal/config"
)

const AppName = "sshops"

var (
	cfgFile   string
	appConfig *config.Config
	rootCmd   = &cobra.Command{
		Use:   AppName,
		Short: "SSH 运维命令行工具",
	}
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "✗ 执行失败：命令参数不正确")
		os.Exit(1)
	}
}

func init() {
	defaultConfigPath := defaultConfigFilePath()

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", defaultConfigPath, "配置文件路径")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(playbookCmd)
	cobra.OnInitialize(initConfig)
}

func initConfig() {
	configDir := filepath.Dir(cfgFile)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "✗ 初始化失败：无法创建配置目录")
		os.Exit(1)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗ 配置加载失败：请检查配置文件格式是否正确")
		os.Exit(1)
	}

	appConfig = cfg
}

func currentConfig() *config.Config {
	if appConfig == nil {
		return config.Default()
	}
	return appConfig
}

func defaultConfigFilePath() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = "."
		}
		return filepath.Join(appData, AppName, "config.yaml")
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".sshops", "config.yaml")
	}
	return filepath.Join(home, ".sshops", "config.yaml")
}
