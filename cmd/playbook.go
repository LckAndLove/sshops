package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/playbook"
	execrunner "github.com/yourname/sshops/internal/runner"
)

type playbookSummary struct {
	Name  string `yaml:"name"`
	Hosts string `yaml:"hosts"`
	Tasks []any  `yaml:"tasks"`
}

var (
	playbookVars []string
)

var playbookCmd = &cobra.Command{
	Use:   "playbook",
	Short: "Playbook 编排执行",
}

var playbookRunCmd = &cobra.Command{
	Use:   "run <file>",
	Short: "执行 playbook 文件",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pb, err := playbook.Load(strings.TrimSpace(args[0]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ 加载 playbook 失败：%s\n", err.Error())
			os.Exit(1)
		}

		cfg := currentConfig()
		inv, err := inventory.Load(cfg.InventoryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 加载主机清单失败：请检查 inventory 文件格式")
			os.Exit(1)
		}

		vars, err := parseKVList(playbookVars)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ 变量格式错误：%s\n", err.Error())
			os.Exit(1)
		}

		r := playbook.NewPlaybookRunner(inv, execrunner.NewRunner(10, cfg.ConnectTimeout, 0))
		r.KeyPath = strings.TrimSpace(cfg.DefaultKeyPath)
		r.Vars = vars

		if err := r.Run(pb); err != nil {
			fmt.Fprintf(os.Stderr, "✗ Playbook 执行失败：%s\n", err.Error())
			os.Exit(1)
		}
	},
}

var playbookListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出内置与自定义 playbook 文件",
	Run: func(cmd *cobra.Command, args []string) {
		type row struct {
			File    string
			Type    string
			Summary *playbookSummary
		}

		rows := make([]row, 0, 16)
		for _, file := range playbook.BuiltinPlaybookFiles() {
			path := playbook.BuiltinPlaybookPath(file)
			summary, err := loadPlaybookSummary(path)
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ 解析内置 playbook 失败 %s: %s\n", path, err.Error())
				os.Exit(1)
			}
			rows = append(rows, row{
				File:    path,
				Type:    "[内置]",
				Summary: summary,
			})
		}

		matches, err := filepath.Glob("*.yml")
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 扫描 yml 文件失败")
			os.Exit(1)
		}
		for _, file := range matches {
			summary, err := loadPlaybookSummary(file)
			if err != nil {
				fmt.Fprintf(os.Stderr, "✗ 解析文件失败 %s: %s\n", file, err.Error())
				os.Exit(1)
			}
			rows = append(rows, row{
				File:    file,
				Type:    "[用户]",
				Summary: summary,
			})
		}

		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Type != rows[j].Type {
				return rows[i].Type < rows[j].Type
			}
			return rows[i].File < rows[j].File
		})

		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "TYPE\tFILE\tNAME\tHOSTS\tTASKS")
		for _, item := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", item.Type, item.File, valueOrDefault(item.Summary.Name, "-"), valueOrDefault(item.Summary.Hosts, "-"), len(item.Summary.Tasks))
		}
		_ = w.Flush()
	},
}

var playbookInitCmd = &cobra.Command{
	Use:   "init <name>",
	Short: "初始化 playbook 模板文件",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := strings.TrimSpace(args[0])
		if name == "" {
			fmt.Fprintln(os.Stderr, "✗ 请提供模板名称")
			os.Exit(1)
		}

		target := name + ".yml"
		if _, err := os.Stat(target); err == nil {
			fmt.Fprintf(os.Stderr, "✗ 文件已存在：%s\n", target)
			os.Exit(1)
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "✗ 检查文件状态失败：%s\n", err.Error())
			os.Exit(1)
		}

		template := fmt.Sprintf(`name: %s
hosts: all
vars:
  app_name: demo
tasks:
  - name: check uptime
    command: "echo {{ .vars.app_name }} && uptime"
`, name)

		if err := os.WriteFile(target, []byte(template), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "✗ 创建模板失败：%s\n", err.Error())
			os.Exit(1)
		}

		fmt.Printf("✓ 已创建 %s\n", target)
	},
}

func init() {
	playbookCmd.AddCommand(playbookRunCmd, playbookListCmd, playbookInitCmd)
	playbookRunCmd.Flags().StringArrayVarP(&playbookVars, "var", "v", []string{}, "Variables in key=value format, can be used multiple times")
}

func parseKVList(values []string) (map[string]string, error) {
	vars := map[string]string{}
	for _, raw := range values {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		kv := strings.SplitN(item, "=", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("无效项 %q，请使用 key=value", item)
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		if key == "" {
			return nil, fmt.Errorf("无效项 %q，key 不能为空", item)
		}
		vars[key] = val
	}
	return vars, nil
}

func loadPlaybookSummary(path string) (*playbookSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	summary := &playbookSummary{}
	if err := yaml.Unmarshal(data, summary); err != nil {
		return nil, err
	}
	if summary.Tasks == nil {
		summary.Tasks = []any{}
	}
	return summary, nil
}

func valueOrDefault(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}
