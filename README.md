# SSH Port Forward

基于 Wails v3 的 Windows 托盘端口转发管理工具。

## 功能

- 左键点击托盘图标，显示 `~/.ssh/config` 中的 Host。
- 每个 Host 用红/绿状态灯表示当前是否有端口转发在运行。
- 点击 Host 可查看已有转发、启动或停止转发、新增和删除转发。
- 支持本地转发、远端转发和动态 SOCKS5 代理。
- 转发配置保存在用户配置目录的 `ssh-port-forward/forwards.json`。
- 标记为“应用启动时自动转发”的条目会在下次启动后自动恢复。

## 环境

- Windows 10/11
- Go 1.26 或更高版本
- Microsoft Edge WebView2 Runtime
- Windows OpenSSH Client（`ssh.exe` 已加入 `PATH`）

SSH 使用 `BatchMode=yes` 运行，后台进程不能交互输入密码。请提前配置好密钥，并至少成功连接一次以确认 Host Key。

## 开发运行

```powershell
$env:Path = "C:\Programs\Go\bin;$env:Path"
go run .
```

程序启动后不会弹出主窗口，请在系统托盘中找到 SSH Port Forward 图标。

## 构建

```powershell
$env:Path = "C:\Programs\Go\bin;$env:Path"
go test ./...
go build -tags production -trimpath -ldflags="-H windowsgui -s -w" -o bin/ssh-port-forward.exe .
```

生成后可执行文件位于 `bin/ssh-port-forward.exe`。

仓库内的 `wails.exe.syso` 包含 Windows 高 DPI 清单和程序图标，直接使用 `go build` 时也会自动链接，请勿删除。

## 快捷键

- 左键托盘：显示或隐藏 Host 列表。
- 右键托盘：刷新 Host、隐藏窗口或退出。
- `Esc`：关闭 Host 详情或隐藏弹窗。
