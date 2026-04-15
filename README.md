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
