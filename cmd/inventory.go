package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/yourname/sshops/internal/display"
	"github.com/yourname/sshops/internal/inventory"
	"github.com/yourname/sshops/internal/vault"
)

var (
	invName         string
	invHost         string
	invPort         int
	invUser         string
	invKey          string
	invGroup        string
	invTag          string
	invProxy        string
	invSavePassword bool
)

var inventoryCmd = &cobra.Command{
	Use:   "inventory",
	Short: "主机清单管理",
}

var inventoryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "添加主机",
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(invName) == "" || strings.TrimSpace(invHost) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供 --name 和 --host")
			os.Exit(1)
		}

		cfg := currentConfig()
		inv, err := inventory.Load(cfg.InventoryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 加载主机清单失败：请检查 inventory 文件格式")
			os.Exit(1)
		}

		groups := parseGroups(invGroup)
		tags, err := parseTagsStrict(invTag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 标签格式错误：请使用 key=value,key=value")
			os.Exit(1)
		}

		h := &inventory.Host{
			Name:       strings.TrimSpace(invName),
			Host:       strings.TrimSpace(invHost),
			Port:       invPort,
			User:       strings.TrimSpace(invUser),
			KeyPath:    strings.TrimSpace(invKey),
			Groups:     groups,
			Tags:       tags,
			ProxyChain: strings.TrimSpace(invProxy),
		}
		if h.Port <= 0 {
			h.Port = 22
		}
		if h.User == "" {
			h.User = "root"
		}

		if err := inv.Add(h); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s\n", err.Error())
			os.Exit(1)
		}
		if err := inv.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "✗ 保存主机清单失败：请检查文件权限")
			os.Exit(1)
		}

		if invSavePassword {
			v := vault.NewVault(cfg.VaultPath)
			master, err := readSecret("请输入 Vault 主密码：")
			if err == nil && strings.TrimSpace(master) != "" {
				if err := v.Unlock(master); err == nil {
					pwd, pwdErr := readSecret("请输入 SSH 密码：")
					if pwdErr == nil && strings.TrimSpace(pwd) != "" {
						_ = v.Set(&vault.Credential{Name: h.Name, Password: pwd, KeyPath: h.KeyPath})
					}
					v.Lock()
				}
			}
		}

		fmt.Printf("✓ 已添加主机 %s (%s:%d)\n", h.Name, h.Host, h.Port)
	},
}

var inventoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有主机",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := currentConfig()
		inv, err := inventory.Load(cfg.InventoryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 加载主机清单失败：请检查 inventory 文件格式")
			os.Exit(1)
		}

		hosts := inv.List()
		sort.Slice(hosts, func(i, j int) bool { return hosts[i].Name < hosts[j].Name })
		display.PrintHostTable(hosts)
	},
}

var inventoryRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "删除主机",
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(invName) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供 --name")
			os.Exit(1)
		}

		cfg := currentConfig()
		inv, err := inventory.Load(cfg.InventoryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 加载主机清单失败：请检查 inventory 文件格式")
			os.Exit(1)
		}

		if err := inv.Remove(invName); err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s\n", err.Error())
			os.Exit(1)
		}
		if err := inv.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "✗ 保存主机清单失败：请检查文件权限")
			os.Exit(1)
		}

		v := vault.NewVault(cfg.VaultPath)
		master, err := readSecret("请输入 Vault 主密码：")
		if err == nil && strings.TrimSpace(master) != "" {
			if err := v.Unlock(master); err == nil {
				_ = v.Delete(strings.TrimSpace(invName))
				v.Lock()
			}
		}

		fmt.Printf("✓ 已删除主机 %s\n", strings.TrimSpace(invName))
	},
}

var inventoryShowCmd = &cobra.Command{
	Use:   "show",
	Short: "显示单台主机详情",
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(invName) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供 --name")
			os.Exit(1)
		}

		cfg := currentConfig()
		inv, err := inventory.Load(cfg.InventoryPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "✗ 加载主机清单失败：请检查 inventory 文件格式")
			os.Exit(1)
		}

		h, err := inv.Get(invName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s\n", err.Error())
			os.Exit(1)
		}

		groups := "（无）"
		if len(h.Groups) > 0 {
			groups = strings.Join(h.Groups, ", ")
		}
		tags := "（无）"
		if len(h.Tags) > 0 {
			tags = tagsToTextWithSep(h.Tags, ", ")
		}
		proxy := h.ProxyChain
		if strings.TrimSpace(proxy) == "" {
			proxy = "（无）"
		}

		fmt.Printf("名称:\t%s\n", h.Name)
		fmt.Printf("主机:\t%s\n", h.Host)
		fmt.Printf("端口:\t%d\n", h.Port)
		fmt.Printf("用户:\t%s\n", h.User)
		fmt.Printf("分组:\t%s\n", groups)
		fmt.Printf("标签:\t%s\n", tags)
		fmt.Printf("跳板机:\t%s\n", proxy)
	},
}

func init() {
	rootCmd.AddCommand(inventoryCmd)
	inventoryCmd.AddCommand(inventoryAddCmd, inventoryListCmd, inventoryRemoveCmd, inventoryShowCmd)

	inventoryAddCmd.Flags().StringVarP(&invName, "name", "n", "", "主机别名")
	inventoryAddCmd.Flags().StringVarP(&invHost, "host", "H", "", "IP 或域名")
	inventoryAddCmd.Flags().IntVarP(&invPort, "port", "p", 22, "端口")
	inventoryAddCmd.Flags().StringVarP(&invUser, "user", "u", "root", "用户名")
	inventoryAddCmd.Flags().StringVarP(&invKey, "key", "i", "", "私钥路径")
	inventoryAddCmd.Flags().StringVarP(&invGroup, "group", "g", "", "分组，多个用逗号分隔")
	inventoryAddCmd.Flags().StringVarP(&invTag, "tag", "t", "", "标签，格式 key=value,key=value")
	inventoryAddCmd.Flags().StringVar(&invProxy, "proxy", "", "跳板机链")
	inventoryAddCmd.Flags().BoolVar(&invSavePassword, "password", false, "是否保存密码到 vault")

	inventoryRemoveCmd.Flags().StringVarP(&invName, "name", "n", "", "主机别名")
	inventoryShowCmd.Flags().StringVarP(&invName, "name", "n", "", "主机别名")
}

func parseGroups(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}

	parts := strings.Split(trimmed, ",")
	groups := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			groups = append(groups, v)
		}
	}
	return groups
}

func parseTagsStrict(raw string) (map[string]string, error) {
	tags := map[string]string{}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return tags, nil
	}

	parts := strings.Split(trimmed, ",")
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		kv := strings.SplitN(item, "=", 2)
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
			return nil, errors.New("invalid tag")
		}
		tags[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return tags, nil
}

func tagsToText(tags map[string]string) string {
	return tagsToTextWithSep(tags, ",")
}

func tagsToTextWithSep(tags map[string]string, sep string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, tags[k]))
	}
	return strings.Join(parts, sep)
}

func readSecret(prompt string) (string, error) {
	fmt.Print(prompt)
	buf, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}
