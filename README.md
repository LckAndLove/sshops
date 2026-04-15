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
