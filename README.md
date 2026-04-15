# sshops

> 基于 SSH 的跨平台远程运维工具，无需在目标服务器安装任何软件。
> 支持作为 MCP Server 接入 Claude Code、Cursor 等 AI 编程工具。

## 特性

- 无侵入：目标服务器只需开启 SSH，无需安装 Agent
- 跨平台：Windows / macOS / Linux 单二进制，开箱即用
- AI 友好：内置 MCP Server，Claude Code 可直接调用
- 批量执行：并发多主机执行，实时进度显示
- 安全存储：AES-256-GCM 加密凭据，本地 vault
- 完整审计：SQLite 记录所有操作历史

## 安装

### 直接下载

从 GitHub Releases 下载对应平台的二进制文件：
https://github.com/LckAndLove/sshops/releases

Windows:
下载 sshops-windows-amd64.zip，解压后将 sshops.exe 放入 PATH

macOS:
下载 sshops-darwin-arm64.tar.gz（Apple Silicon）
或 sshops-darwin-amd64.tar.gz（Intel）

```bash
tar -xzf sshops-darwin-*.tar.gz
sudo mv sshops /usr/local/bin/
```

Linux:
下载 sshops-linux-amd64.tar.gz

```bash
tar -xzf sshops-linux-amd64.tar.gz
sudo mv sshops /usr/local/bin/
```

### 从源码构建

```bash
git clone https://github.com/LckAndLove/sshops.git
cd sshops
go build -o sshops.exe .   # Windows
go build -o sshops .       # Linux/macOS
```

带版本信息注入构建：

```bash
go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o sshops.exe .
```

## 快速上手（5 分钟）

### 1. 添加服务器

```bash
sshops inventory add \
  --name prod-01 \
  --host 159.223.50.31 \
  --user root \
  --key ~/.ssh/id_ed25519 \
  --group prod \
  --tag env=prod
```

### 2. 执行命令

```bash
# 单台
sshops exec --host prod-01 "uptime"

# 按分组批量执行
sshops exec --group prod "df -h" --concurrency 10
```

### 3. 文件传输

```bash
sshops upload --host prod-01 --src ./app.tar.gz --dst /tmp/app.tar.gz
sshops download --host prod-01 --src /var/log/app.log --dst ./app.log
```

### 4. 接入 Claude Code

```bash
# 注册 MCP Server
claude mcp add sshops -- /path/to/sshops mcp serve
```

然后在 Claude Code 中直接用自然语言：

```text
帮我检查 prod 组所有服务器的磁盘使用情况
```

## 命令参考

- `sshops version`：查看版本、提交哈希、构建时间和 Go 版本
- `sshops inventory add`：添加主机到清单
- `sshops inventory list`：列表显示所有主机
- `sshops inventory show`：查看单台主机详情
- `sshops inventory remove`：删除主机
- `sshops exec --host ... "cmd"`：单台主机执行命令
- `sshops exec --group ... "cmd"`：分组批量并发执行命令
- `sshops exec logs --limit N`：查看最近审计日志
- `sshops upload --host ... --src ... --dst ...`：上传文件或目录
- `sshops download --host ... --src ... --dst ...`：下载文件或目录
- `sshops mcp serve --transport stdio|sse`：启动 MCP Server

## 配置文件

默认配置文件路径：

- Windows: `%APPDATA%\sshops\config.yaml`
- Linux/macOS: `~/.sshops/config.yaml`

可配置项：

- `default_user`: 默认 SSH 用户
- `default_port`: 默认 SSH 端口
- `default_key_path`: 默认私钥路径
- `connect_timeout`: 默认连接超时（秒）
- `inventory_path`: 主机清单文件路径
- `vault_path`: 凭据加密文件路径
- `audit_db_path`: 审计数据库路径

## MCP Server 配置

stdio 模式：

```bash
sshops mcp serve --transport stdio --vault-password your-password
```

Claude Desktop 配置示例：

```json
{
  "mcpServers": {
    "sshops": {
      "command": "C:\\path\\to\\sshops.exe",
      "args": ["mcp", "serve", "--vault-password", "your-password"]
    }
  }
}
```

SSE 模式：

```bash
sshops mcp serve --transport sse --port 3000
```

SSE 配置示例：

```json
{"mcpServers":{"sshops":{"url":"http://localhost:3000/sse"}}}
```

## 许可证

MIT
