package cmd

import (
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
	"github.com/spf13/cobra"

	"github.com/yourname/sshops/internal/config"
	"github.com/yourname/sshops/internal/inventory"
	sshclient "github.com/yourname/sshops/internal/ssh"
)

var (
	transferHost  string
	transferGroup string
	transferUser  string
	transferKey   string
	transferPort  int
	transferSrc   string
	transferDst   string
)

var uploadCmd = &cobra.Command{
	Use:   "upload",
	Short: "上传文件或目录到远程主机",
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(transferSrc) == "" || strings.TrimSpace(transferDst) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供 --src 和 --dst")
			os.Exit(1)
		}

		targets, err := resolveTransferTargets(currentConfig(), transferHost, transferGroup)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s\n", err.Error())
			os.Exit(1)
		}

		for _, t := range targets {
			if err := uploadToTarget(currentConfig(), t, transferSrc, transferDst); err != nil {
				fmt.Fprintf(os.Stderr, "✗ %s (%s): %s\n", t.Name, t.Host, err.Error())
			}
		}
	},
}

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "从远程主机下载文件或目录",
	Run: func(cmd *cobra.Command, args []string) {
		if strings.TrimSpace(transferSrc) == "" || strings.TrimSpace(transferDst) == "" {
			_ = cmd.Usage()
			fmt.Fprintln(os.Stderr, "✗ 请提供 --src 和 --dst")
			os.Exit(1)
		}

		targets, err := resolveTransferTargets(currentConfig(), transferHost, transferGroup)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ %s\n", err.Error())
			os.Exit(1)
		}

		for _, t := range targets {
			if err := downloadFromTarget(currentConfig(), t, transferSrc, transferDst); err != nil {
				fmt.Fprintf(os.Stderr, "✗ %s (%s): %s\n", t.Name, t.Host, err.Error())
			}
		}
	},
}

func init() {
	rootCmd.AddCommand(uploadCmd, downloadCmd)

	bindTransferFlags(uploadCmd)
	bindTransferFlags(downloadCmd)
}

func bindTransferFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&transferHost, "host", "H", "", "主机 IP 或清单中的名称")
	cmd.Flags().StringVarP(&transferUser, "user", "u", "", "用户名")
	cmd.Flags().StringVarP(&transferKey, "key", "i", "", "私钥路径")
	cmd.Flags().IntVarP(&transferPort, "port", "p", 0, "端口")
	cmd.Flags().StringVar(&transferSrc, "src", "", "源路径")
	cmd.Flags().StringVar(&transferDst, "dst", "", "目标路径")
	cmd.Flags().StringVarP(&transferGroup, "group", "g", "", "按分组批量传输")
}

func resolveTransferTargets(cfg *config.Config, hostOrName string, group string) ([]*inventory.Host, error) {
	inv, err := inventory.Load(cfg.InventoryPath)
	if err != nil {
		return nil, errors.New("加载主机清单失败")
	}

	if strings.TrimSpace(group) != "" {
		hosts := inventory.FilterByGroup(inv.List(), group)
		if len(hosts) == 0 {
			return nil, errors.New("未找到匹配分组主机")
		}
		return hosts, nil
	}

	if strings.TrimSpace(hostOrName) == "" {
		return nil, errors.New("请提供 --host 或 --group")
	}

	if h, err := inv.Get(strings.TrimSpace(hostOrName)); err == nil {
		return []*inventory.Host{h}, nil
	}

	port := transferPort
	if port <= 0 {
		port = cfg.DefaultPort
	}
	user := strings.TrimSpace(transferUser)
	if user == "" {
		user = cfg.DefaultUser
	}
	return []*inventory.Host{{
		Name: hostOrName,
		Host: hostOrName,
		Port: port,
		User: user,
	}}, nil
}

func uploadToTarget(cfg *config.Config, h *inventory.Host, src string, dst string) error {
	client, sftpClient, err := openSFTP(cfg, h)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return uploadDir(sftpClient, src, dst, h)
	}
	return uploadFile(sftpClient, src, dst, h)
}

func downloadFromTarget(cfg *config.Config, h *inventory.Host, src string, dst string) error {
	client, sftpClient, err := openSFTP(cfg, h)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()

	st, err := sftpClient.Stat(src)
	if err != nil {
		return err
	}

	if st.IsDir() {
		return downloadDir(sftpClient, src, dst, h)
	}
	return downloadFile(sftpClient, src, dst, h)
}

func openSFTP(cfg *config.Config, h *inventory.Host) (*sshclient.Client, *sftp.Client, error) {
	if h == nil {
		return nil, nil, errors.New("主机信息为空")
	}

	port := h.Port
	if transferPort > 0 {
		port = transferPort
	}
	if port <= 0 {
		port = cfg.DefaultPort
	}

	user := strings.TrimSpace(h.User)
	if strings.TrimSpace(transferUser) != "" {
		user = strings.TrimSpace(transferUser)
	}
	if user == "" {
		user = cfg.DefaultUser
	}

	keyPath := strings.TrimSpace(transferKey)
	if keyPath == "" {
		keyPath = strings.TrimSpace(h.KeyPath)
	}
	if keyPath == "" {
		keyPath = cfg.DefaultKeyPath
	}

	client := sshclient.NewClient(h.Host, port, user, cfg.ConnectTimeout)
	if err := client.WithKey(keyPath); err != nil {
		return nil, nil, err
	}

	if strings.TrimSpace(h.ProxyChain) != "" {
		proxies, err := parseTransferProxyChain(h.ProxyChain, user, keyPath)
		if err == nil {
			client.Proxies = proxies
		}
	}

	if err := client.Connect(); err != nil {
		return nil, nil, err
	}

	raw := client.Raw()
	if raw == nil {
		return nil, nil, errors.New("SSH 连接不可用")
	}

	sftpClient, err := sftp.NewClient(raw)
	if err != nil {
		client.CloseForce()
		return nil, nil, err
	}
	return client, sftpClient, nil
}

func uploadDir(c *sftp.Client, localDir string, remoteDir string, h *inventory.Host) error {
	return filepath.Walk(localDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		target := path.Join(remoteDir, rel)
		if info.IsDir() {
			return c.MkdirAll(target)
		}
		return uploadFile(c, p, target, h)
	})
}

func uploadFile(c *sftp.Client, localFile string, remoteFile string, h *inventory.Host) error {
	in, err := os.Open(localFile)
	if err != nil {
		return err
	}
	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return err
	}

	if err := c.MkdirAll(path.Dir(remoteFile)); err != nil {
		return err
	}
	out, err := c.Create(remoteFile)
	if err != nil {
		return err
	}
	defer out.Close()

	start := time.Now()
	progress := newTransferProgress(localFile, st.Size())
	if _, err := io.Copy(out, io.TeeReader(in, progress)); err != nil {
		return err
	}
	progress.Done()

	fmt.Printf("✓ 已上传 %s → %s@%s:%s (%s, %s)\n", filepath.Base(localFile), h.User, h.Host, remoteFile, humanBytes(st.Size()), time.Since(start).Round(100*time.Millisecond))
	return nil
}

func downloadDir(c *sftp.Client, remoteDir string, localDir string, h *inventory.Host) error {
	walker := c.Walk(remoteDir)
	for walker.Step() {
		if err := walker.Err(); err != nil {
			return err
		}

		rel := strings.TrimPrefix(filepath.ToSlash(walker.Path()), strings.TrimSuffix(filepath.ToSlash(remoteDir), "/"))
		rel = strings.TrimPrefix(rel, "/")
		target := filepath.Join(localDir, filepath.FromSlash(rel))

		if walker.Stat().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := downloadFile(c, walker.Path(), target, h); err != nil {
			return err
		}
	}
	return nil
}

func downloadFile(c *sftp.Client, remoteFile string, localFile string, h *inventory.Host) error {
	in, err := c.Open(remoteFile)
	if err != nil {
		return err
	}
	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(localFile), 0o755); err != nil {
		return err
	}
	out, err := os.Create(localFile)
	if err != nil {
		return err
	}
	defer out.Close()

	start := time.Now()
	progress := newTransferProgress(filepath.Base(remoteFile), st.Size())
	if _, err := io.Copy(out, io.TeeReader(in, progress)); err != nil {
		return err
	}
	progress.Done()

	fmt.Printf("✓ 已下载 %s@%s:%s → %s (%s, %s)\n", h.User, h.Host, remoteFile, localFile, humanBytes(st.Size()), time.Since(start).Round(100*time.Millisecond))
	return nil
}

type transferProgress struct {
	name        string
	total       int64
	transferred int64
	start       time.Time
}

func newTransferProgress(name string, total int64) *transferProgress {
	return &transferProgress{name: name, total: total, start: time.Now()}
}

func (p *transferProgress) Write(b []byte) (int, error) {
	n := len(b)
	p.transferred += int64(n)
	p.render()
	return n, nil
}

func (p *transferProgress) Done() {
	p.render()
	fmt.Println()
}

func (p *transferProgress) render() {
	if p.total <= 0 {
		return
	}
	percent := float64(p.transferred) / float64(p.total)
	if percent > 1 {
		percent = 1
	}
	barWidth := 20
	filled := int(percent * float64(barWidth))
	if filled >= barWidth {
		filled = barWidth - 1
	}
	bar := "[" + strings.Repeat("=", filled) + ">" + strings.Repeat(" ", barWidth-filled-1) + "]"
	elapsed := time.Since(p.start).Seconds()
	speed := float64(p.transferred)
	if elapsed > 0 {
		speed = speed / elapsed
	}
	fmt.Printf("\r%s %3.0f%%  %s/%s  %s/s", bar, percent*100, humanBytes(p.transferred), humanBytes(p.total), humanBytes(int64(speed)))
}

func humanBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	value := float64(n)
	idx := 0
	for value >= 1024 && idx < len(units)-1 {
		value /= 1024
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%d%s", n, units[idx])
	}
	return fmt.Sprintf("%.1f%s", value, units[idx])
}

func parseTransferProxyChain(raw string, defaultUser string, keyPath string) ([]sshclient.ProxyConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	out := make([]sshclient.ProxyConfig, 0, len(parts))
	for _, p := range parts {
		item := strings.TrimSpace(p)
		if item == "" {
			continue
		}
		user := defaultUser
		hostPort := item
		if strings.Contains(item, "@") {
			s := strings.SplitN(item, "@", 2)
			if strings.TrimSpace(s[0]) != "" {
				user = strings.TrimSpace(s[0])
			}
			hostPort = strings.TrimSpace(s[1])
		}

		host := hostPort
		port := 22
		if strings.Contains(hostPort, ":") {
			idx := strings.LastIndex(hostPort, ":")
			host = strings.TrimSpace(hostPort[:idx])
			if pInt, err := strconv.Atoi(strings.TrimSpace(hostPort[idx+1:])); err == nil && pInt > 0 {
				port = pInt
			}
		}
		out = append(out, sshclient.ProxyConfig{Host: host, Port: port, User: user, KeyPath: keyPath})
	}
	return out, nil
}
