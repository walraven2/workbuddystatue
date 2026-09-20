# WorkBuddy 积分状态栏小工具 v1.2.0

在 macOS 菜单栏实时显示 WorkBuddy 积分余额，点开即可看积分明细与签到状态。

## 下载

| 文件 | 说明 |
| --- | --- |
| `WorkBuddyStatus-1.2.0-macos-universal.zip` | Universal 二进制，Intel 与 Apple 芯片通用 |

## 安装

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

## 功能

- **菜单栏常驻**：以 `⚡ 1507` 形式显示剩余积分，支持数值 / 百分比 / 仅图标三种模式
- **自动读取登录态**：复用 WorkBuddy 桌面端已登录的令牌，无需手动填 token，
  令牌在 WorkBuddy 里刷新后自动跟随
- **积分明细**：展示各套餐（成长计划、奖励积分等）的剩余 / 总量 / 已用
- **签到状态**：今日是否签到、连续天数、本周签到天数
- **自动刷新**：1 / 5 / 15 / 30 分钟，或仅手动刷新（⌘R）
- **登录时启动**：用户级 LaunchAgent 实现，菜单内一键开关

## 诊断

```bash
/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --check
```

会打印账户信息、令牌有效期、积分明细与接口连通性，排查问题时很有用。

## 系统要求

- macOS 13 或更高
- 已安装并登录 WorkBuddy 桌面端

## 卸载

```bash
/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --disable-login
rm -rf /Applications/WorkBuddyStatus.app ~/.workbuddy-status
```

## 说明

工具通过 WorkBuddy 桌面端的积分接口 `billing/meter/get-user-resource-summary` 读取数据，
复用本机已有的登录令牌，**不会**上传任何数据到第三方。令牌以明文保存在
`~/Library/Application Support/CodeBuddyExtension/Data/Public/auth/`（WorkBuddy 自己管理的目录），
本工具只读不写。
