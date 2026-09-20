# WorkBuddy 积分小工具 v1.2.0

在 macOS 菜单栏 / Windows 系统托盘实时显示 WorkBuddy 积分余额，点开即可看积分明细与签到状态。

## 下载

| 平台 | 文件 | 说明 |
| --- | --- | --- |
| macOS | `WorkBuddyStatus-1.2.0-macos-universal.zip` | Universal 二进制，Intel 与 Apple 芯片通用 |
| Windows | `WorkBuddyStatus-1.2.0-windows-amd64.zip` | 单文件 exe，64 位，无需运行库 |
| Linux | — | 见仓库 `linux/` 目录，Python 3 + XFCE genmon |

---

## macOS

```bash
unzip WorkBuddyStatus-1.2.0-macos-universal.zip
xattr -dr com.apple.quarantine WorkBuddyStatus.app
cp -R WorkBuddyStatus.app /Applications/ && open /Applications/WorkBuddyStatus.app
```

装好后菜单栏会出现 ⚡ 图标，后面跟着当前剩余积分。

**首次打开被系统拦截**：右键 App → 打开。本项目只做 ad-hoc 签名，没有 Apple 开发者证书，
Gatekeeper 会提示「无法验证开发者」，属正常现象。

也可以用仓库里的安装脚本一键完成（自动清理隔离属性并处理登录自启）：

```bash
./install.sh WorkBuddyStatus-1.2.0-macos-universal.zip   # 安装并启动
./install.sh --uninstall                                 # 卸载（含登录自启）
```

## Windows

解压后双击 `WorkBuddyStatus.exe` 即可，会在右下角托盘出现图标，
并在桌面右上角显示一个可拖动的积分悬浮窗。

首次运行建议先在终端跑一次诊断：

```powershell
.\WorkBuddyStatus.exe --check
```

会逐个列出凭据目录、令牌有效期、接口连通性与积分明细，
并写入 `%USERPROFILE%\.workbuddy-status\last-check.txt`。

> Windows 版是 Go 写的，界面层无法在 macOS 上执行验证，只做了交叉编译与
> PE / 资源结构的字节级校验。第一次运行若异常，请把 `--check` 的输出和
> `%USERPROFILE%\.workbuddy-status\last-error.log` 一并发出来。

## 功能

- **托盘 / 菜单栏常驻**：以 `⚡ 1,494` 形式显示剩余积分，支持数值 / 百分比 / 仅图标三种模式
- **自动读取登录态**：复用 WorkBuddy 桌面端已登录的令牌，无需手动填 token，
  令牌在 WorkBuddy 里刷新后自动跟随
- **积分明细**：展示各套餐（成长计划、奖励积分等）的剩余 / 总量 / 已用
- **签到状态**：今日是否签到、连续天数、本周签到天数
- **自动刷新**：1 / 5 / 15 / 30 分钟、1 小时，或仅手动刷新
- **开机自启**：菜单内一键开关（macOS 用 LaunchAgent，Windows 写注册表 Run 键）
- **桌面悬浮窗**（仅 Windows）：深色圆角挂件，位置可拖动并记住

## 诊断

```bash
# macOS
/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --check

# Windows
WorkBuddyStatus.exe --check
```

会打印账户信息、令牌有效期、积分明细与接口连通性，排查问题时很有用。

## 系统要求

- macOS 13 或更高，或 Windows 10 及以上（64 位）
- 已安装并登录 WorkBuddy 桌面端

## 卸载

```bash
# macOS
/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --disable-login
rm -rf /Applications/WorkBuddyStatus.app ~/.workbuddy-status
```

```powershell
# Windows
.\WorkBuddyStatus.exe --disable-autostart
Remove-Item -Recurse -Force "$env:USERPROFILE\.workbuddy-status"
```

## 说明

工具通过 WorkBuddy 桌面端的积分接口 `billing/meter/get-user-resource-summary` 读取数据，
复用本机已有的登录令牌，**不会**上传任何数据到第三方。令牌由 WorkBuddy 自己管理
（macOS 在 `~/Library/Application Support/CodeBuddyExtension/`，
Windows 在 `%LOCALAPPDATA%\CodeBuddyExtension\`），本工具只读不写。

三个平台共用同一份配置文件 `~/.workbuddy-status/config.json` 与同一套接口。
