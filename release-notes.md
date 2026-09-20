# WorkBuddy 积分小工具 v1.3.0

在 macOS 菜单栏 / Windows 系统托盘 / Linux 桌面实时显示 WorkBuddy 积分余额，点开即可看积分明细。

**这一版的重点：签到从「只能看状态」变成了「点一下就签」，Linux 版也重写成了桌面小组件。**

## 本版更新

### 新增：每日签到（三平台）

以前只能看到「今日是否已签到」，现在可以直接点按钮签到，顺手领积分。

- **一键签到**：窗口里一个按钮，点完立刻显示领到多少积分、连续第几天
- **托盘菜单**：不打开窗口也能签（macOS 菜单栏 / Windows 托盘右键 / Linux 托盘右键）
- **幂等安全**：重复点不会报错，今天签过了按钮就会变绿并置灰
- **连续天数**：显示连续签到天数与本周已签天数

> 踩到的坑记录一下：接口在「今日已签」时返回的是 **HTTP 400**，业务码 `10001` 藏在响应体里。
> 如果只看 HTTP 状态码就会把「今天已经签过了」误判成失败并弹错误框。已按业务码判定。
> 另有一个 `/checkin-status` 端点会返回矛盾的 `active:false`，不可信，已弃用。

### Linux 版重写：改用桌面小组件

v1.2 的说明里写的「Python 3 + XFCE genmon」**是不成立的，genmon 根本跑不起来**，
这一版把它换成了真正的桌面程序。

- **GTK3 桌面窗口 + 托盘图标**，与桌面环境无关（XFCE / GNOME / KDE / MATE 都能用）
- 渐变进度条、积分包分条显示、签到卡片，中文界面
- **单实例**：重复点桌面图标会唤醒已开的窗口，而不是开出第二个
- 托盘常驻模式下**不弹窗**，点图标才展开
- 一键安装脚本自动搞定桌面快捷方式、应用菜单项、开机自启

为什么要换掉 genmon：`xfce4-genmon-plugin 4.1.1` 搭配 `xfce4-panel 4.18.4` 时，
面板上永远显示字面量占位符 `XXX`，配置的命令**一次都没被执行过**。
用 `dbus-monitor` 抓 xfconf 调用可以确认——面板上其它 12 个插件都在读自己的配置，
而 genmon 一条 xfconf 请求都没发。详细排查过程写在 `linux/README.md` 里。

### 修复：Linux 安装脚本会清空面板插件

`install.sh` 原来用 `xfconf-query ... -t int -s $ID -a` 来「追加」面板插件，
但 `xfconf-query` **没有追加数组的选项**——`-a` 是「属性不存在时创建」，
用在数组上会把整个数组**覆盖**成单个元素。

结果是重新运行一次安装脚本，就会把用户面板上原有的插件列表全部清空。
现在改成「先读整份、再整份写回」，并且面板插件注册改为 **opt-in**
（需要显式加 `--with-panel`，默认不碰面板）。

## 下载

| 平台 | 文件 | 说明 |
| --- | --- | --- |
| macOS | `WorkBuddyStatus-1.3.0-macos-universal.zip` | Universal 二进制，Intel 与 Apple 芯片通用 |
| Windows | `WorkBuddyStatus-1.3.0-windows-amd64.zip` | 单文件 exe，64 位，无需运行库 |
| Linux | — | 见仓库 `linux/` 目录，Python 3 + GTK3，自带安装脚本 |

---

## macOS

```bash
unzip WorkBuddyStatus-1.3.0-macos-universal.zip
xattr -dr com.apple.quarantine WorkBuddyStatus.app
cp -R WorkBuddyStatus.app /Applications/ && open /Applications/WorkBuddyStatus.app
```

装好后菜单栏会出现 ⚡ 图标，后面跟着当前剩余积分。

**首次打开被系统拦截**：右键 App → 打开。本项目只做 ad-hoc 签名，没有 Apple 开发者证书，
Gatekeeper 会提示「无法验证开发者」，属正常现象。

也可以用仓库里的安装脚本一键完成（自动清理隔离属性并处理登录自启）：

```bash
./install.sh WorkBuddyStatus-1.3.0-macos-universal.zip   # 安装并启动
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

## Linux

依赖：`python3-gi`、`gir1.2-gtk-3.0`、`python3-pil`（画图标，可选）。

```bash
cd linux
./install-credits.sh          # 装桌面小组件（含桌面图标、菜单项、开机自启）
./install.sh                  # 装命令行与后台刷新服务
```

装好后双击桌面的「WorkBuddy 积分」图标即可打开；托盘图标常驻，不弹窗。

命令行用法：

```bash
workbuddy-credits            # 打开窗口（已有实例时唤醒它）
workbuddy-credits --tray     # 托盘常驻
workbuddy-status fetch       # 抓一次并写缓存
workbuddy-status checkin     # 签到（输出 JSON）
workbuddy-status daemon      # 后台定时刷新
```

**凭据来源与 macOS / Windows 不同**：Linux 上没有 `CodeBuddyExtension/Data/Public/auth/*.info`，
也没有可用的 `state.vscdb`（里面的令牌是加密 Buffer）。能用的明文 JWT 在
`~/.config/CodeBuddy CN/automations/automations.db` 里（`automation_runs.runs_json`）。
该库里混着不少**被截断的残片**，所以程序会把候选令牌按「优选指纹 → 签发时间 → 长度」
排序后**逐个真去请求接口**，第一个成功的就是要用的那个。

## 功能

- **托盘 / 菜单栏常驻**：以 `⚡ 1,814` 形式显示剩余积分，支持数值 / 百分比 / 仅图标三种模式
- **每日签到**：一键签到，显示领取积分与连续天数（macOS / Windows / Linux 均支持）
- **自动读取登录态**：复用 WorkBuddy 桌面端已登录的令牌，无需手动填 token，
  令牌在 WorkBuddy 里刷新后自动跟随
- **积分明细**：展示各套餐（成长计划、奖励积分等）的剩余 / 总量 / 已用
- **自动刷新**：1 / 5 / 15 / 30 分钟、1 小时，或仅手动刷新
- **开机自启**：菜单内一键开关（macOS 用 LaunchAgent，Windows 写注册表 Run 键，
  Linux 用 autostart `.desktop`）
- **桌面悬浮窗**（仅 Windows）：深色圆角挂件，位置可拖动并记住

## 诊断

```bash
# macOS
/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --check

# Windows
WorkBuddyStatus.exe --check

# Linux
workbuddy-status check
```

会打印账户信息、令牌有效期、积分明细与接口连通性，排查问题时很有用。

## 系统要求

- macOS 13 或更高、Windows 10 及以上（64 位）、或带 GTK3 的 Linux 桌面
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

```bash
# Linux
cd linux && ./install-credits.sh --uninstall && ./install.sh --uninstall
```

## 说明

工具通过 WorkBuddy 桌面端的积分接口 `billing/meter/*` 读取数据，
复用本机已有的登录令牌，**不会**上传任何数据到第三方。令牌由 WorkBuddy 自己管理
（macOS 在 `~/Library/Application Support/CodeBuddyExtension/`，
Windows 在 `%LOCALAPPDATA%\CodeBuddyExtension\`），本工具只读不写。

三个平台共用同一份配置文件 `~/.workbuddy-status/config.json` 与同一套接口。
