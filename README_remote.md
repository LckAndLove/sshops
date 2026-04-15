# sshops

# 构建
cd sshops
go mod tidy
go build -o sshops.exe .     # Windows
go build -o sshops .          # Linux/macOS

# 测试（替换为真实主机信息）
./sshops exec --host 10.0.0.1 --user root --key ~/.ssh/id_rsa "uname -a && df -h"
./sshops exec --host 10.0.0.1 --user root --password "yourpass" "uptime"

# 验证错误处理
./sshops exec                                  # 应提示缺少 host
./sshops exec --host 10.0.0.1                  # 应提示缺少命令
./sshops exec --host 999.999.999.999 "ls"      # 应提示连接失败

# Phase 1 新增测试
# 连接池验证（连续执行两次，第二次应更快）
.\sshops.exe exec --host 10.0.0.1 --user root --key ~/.ssh/id_rsa "uptime"
.\sshops.exe exec --host 10.0.0.1 --user root --key ~/.ssh/id_rsa "hostname"

# 跳板机测试
.\sshops.exe exec --host 192.168.1.10 --proxy root@jump.example.com:22 --key ~/.ssh/id_rsa "uname -a"

# 多跳跳板机
.\sshops.exe exec --host 10.10.0.5 --proxy "root@jump1.com:22,root@jump2.com:22" --key ~/.ssh/id_rsa "df -h"

# Phase 2 测试
# 添加主机
.\sshops.exe inventory add --name prod-01 --host 159.223.50.31 --user root --key C:\Users\91838\.ssh\id_ed25519 --group prod,web --tag env=prod,role=web

# 列出所有主机
.\sshops.exe inventory list

# 查看单台主机
.\sshops.exe inventory show --name prod-01

# 按分组批量执行
.\sshops.exe exec --group prod "uptime"

# 按标签过滤执行
.\sshops.exe exec --tag env=prod "df -h"

# 删除主机
.\sshops.exe inventory remove --name prod-01

# Phase 3 测试

# 并发执行（当前只有1台主机，验证参数生效）
.\sshops.exe exec --group prod "uptime" --concurrency 5 --retry 1

# 查看审计日志
.\sshops.exe exec logs --limit 10

# 文件上传
.\sshops.exe upload --host prod-01 --src .\README.md --dst /tmp/README.md

# 文件下载
.\sshops.exe download --host prod-01 --src /tmp/README.md --dst .\README_remote.md

# 验证下载文件存在
ls .\README_remote.md
