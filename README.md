# WorkBuddy 积分小工具

把 WorkBuddy 的积分余额直接显示在系统托盘 / 菜单栏 / 桌面上，鼠标一点即可看到明细、一键签到，并支持自动刷新与开机自启。

三个平台各有一份实现，共用同一套接口与配置格式：

| 平台 | 显示载体 | 实现 | 说明 |
| --- | --- | --- | --- |
| **macOS** | 菜单栏 | Swift + AppKit | 本文档；产物约 300 KB |
| **Windows** | 系统托盘 + 桌面悬浮窗 | Go（纯标准库） | [windows/README.md](windows/README.md)；单文件 exe |
| **Linux** | 桌面小组件 + 托盘 | Python 3 + GTK3 | [linux/](linux/)；`linux/install-credits.sh` |

---

## macOS 版

![状态栏](预览-状态栏.png)

![菜单](预览-菜单.png)

### 特性

- **菜单栏常驻**：以 `⚡ 1507` 形式显示剩余积分，支持「数值 / 百分比 / 仅图标」三种显示模式
- **自动读取登录态**：直接复用 WorkBuddy 桌面端已登录的令牌，**无需手动填 token**，令牌刷新后自动跟随
- **积分明细**：下拉菜单展示各套餐（成长计划、奖励积分等）的剩余 / 总量 / 已用
- **每日签到**：一键签到并显示领取积分、连续天数、本周签到天数；重复点安全（幂等）
- **自动刷新**：可选 1 / 5 / 15 / 30 分钟，或仅手动刷新（⌘R）
- **登录时启动**：菜单内一键开关，写入用户级 LaunchAgent
- **零依赖**：纯 Swift + AppKit，编译产物约 300 KB

## 环境要求

- macOS 13 及以上（开发验证环境：macOS 13.7.8 / Intel x86_64）
- Xcode 命令行工具（`xcode-select --install`），提供 `swiftc`

## 下载安装（无需编译）

到 [Releases](https://github.com/walraven2/workbuddystatue/releases) 下载
`WorkBuddyStatus-<版本>-macos-universal.zip`（同时支持 Intel 与 Apple 芯片），然后：

```bash
unzip WorkBuddyStatus-1.3.0-macos-universal.zip
./WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --check   # 可选：先诊断
xattr -dr com.apple.quarantine WorkBuddyStatus.app             # 去掉隔离属性
cp -R WorkBuddyStatus.app /Applications/ && open /Applications/WorkBuddyStatus.app
```

或者解压后直接用仓库里的安装脚本：

```bash
./install.sh WorkBuddyStatus-1.3.0-macos-universal.zip   # 安装并启动
./install.sh --uninstall                                 # 卸载（含登录自启）
```

**首次打开被 Gatekeeper 拦截时**：右键 App → 打开，或执行上面的 `xattr -dr`。
原因是本地构建只做了 ad-hoc 签名，没有 Apple 开发者证书。

## 编译与安装

```bash
cd WorkBuddyStatus

./build.sh            # 仅编译当前架构，产物在 build/WorkBuddyStatus.app
./build.sh run        # 编译并运行
./build.sh install    # 编译并安装到 /Applications 后启动
./build.sh dist       # 产出 Universal 发行包 dist/WorkBuddyStatus-<版本>-macos-universal.zip
./build.sh clean      # 清理 build/ 与 dist/
```

发行包会对 x86_64 与 arm64 各编译一次再用 `lipo` 合并，最后用
`ditto -c -k --keepParent` 打包，保留符号链接与扩展属性。

首次打开若被 Gatekeeper 拦截：右键 App → 打开；或执行
`xattr -dr com.apple.quarantine /Applications/WorkBuddyStatus.app`。

## 命令行参数

| 参数 | 说明 |
| --- | --- |
| `--check` / `--diagnose` | 无界面诊断：打印账户、令牌有效期、积分明细，用于排查 |
| `--enable-login` | 开启登录自启 |
| `--disable-login` | 关闭登录自启 |
| `--auto-open-menu` | 启动后自动展开菜单（截图 / 调试用） |

诊断示例：

```bash
/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --check
```

## 配置

配置文件：`~/.workbuddy-status/config.json`（菜单中可一键打开所在目录）

```json
{
  "endpoint": "https://copilot.tencent.com",
  "accessToken": "",
  "userId": "",
  "refreshInterval": 0
}
```

- `accessToken` / `userId` 留空时，自动从 WorkBuddy 桌面端读取，**通常无需填写**
- 也可用环境变量覆盖：`WORKBUDDY_ENDPOINT`、`WORKBUDDY_ACCESS_TOKEN`、`WORKBUDDY_USER_ID`

运行日志：`~/.workbuddy-status/last-error.log`

## 凭据来源

按以下顺序查找：

1. `~/.workbuddy-status/config.json` 中的 `accessToken`
2. `~/Library/Application Support/CodeBuddyExtension/Data/Public/auth/*.info`（WorkBuddy 桌面端登录信息）
3. `~/.workbuddy/auth/`、`~/.codebuddy/auth/`（兼容旧布局）

取到 JWT 后本地解析 `exp` 判断是否过期，过期会在菜单中提示重新登录。

## 数据接口

```
POST https://copilot.tencent.com/billing/meter/get-user-resource-summary
Authorization: Bearer <accessToken>
X-User-Id: <userId>
```

签到相关：

```
POST /billing/meter/checkin-activity-status   # 查状态
POST /billing/meter/daily-checkin             # 执行签到
```

> 「今日已签」时签到接口返回 **HTTP 400** + 响应体 `{"code":10001}`，
> 需按业务码判定，不能把 4xx 当成失败。

## 卸载

```bash
/Applications/WorkBuddyStatus.app/Contents/MacOS/WorkBuddyStatus --disable-login
rm -rf /Applications/WorkBuddyStatus.app ~/.workbuddy-status
```

## 源码结构

| 文件 | 职责 |
| --- | --- |
| `Sources/main.swift` | 应用主体：状态栏项、菜单构建、刷新调度 |
| `Sources/CreditsAPI.swift` | 积分接口客户端与数据模型 |
| `Sources/Credential.swift` | 配置读写、凭据探测与 JWT 解析 |
| `Sources/LaunchAtLogin.swift` | 登录自启（LaunchAgent） |
| `Sources/Diagnostics.swift` | `--check` 诊断输出 |
| `Resources/AppIcon.icns` | 应用图标 |
| `build.sh` | 编译 / 打包 / 签名 / 产出发行包 |
| `install.sh` | 安装 / 卸载脚本 |
| `make-release.sh` | 创建 / 更新 GitHub Release 并上传发行包（需 token） |
| `release-notes.md` | Release 说明正文，由 `make-release.sh` 读取 |
| `windows/` | Windows 版（Go），详见 [windows/README.md](windows/README.md) |
| `linux/` | Linux 版（Python + GTK3 桌面小组件），详见 [linux/README.md](linux/README.md) |

## 共用约定

三个平台读同一份配置文件 `~/.workbuddy-status/config.json`，接口与缓存格式也一致。
**但凭据位置各平台不同**——WorkBuddy 桌面端在各系统存放令牌的方式不一样：

| 平台 | 凭据位置 | 备注 |
| --- | --- | --- |
| macOS | `~/Library/Application Support/CodeBuddyExtension/Data/Public/auth/*.info` | 明文 JSON |
| Windows | `%LOCALAPPDATA%\CodeBuddyExtension\Data\Public\auth\*.info` | 明文 JSON |
| Linux | `~/.config/CodeBuddy CN/automations/automations.db`（表 `automation_runs.runs_json`） | **没有 `auth/*.info`**；`state.vscdb` 里的令牌是加密 Buffer 取不出来，只能从 SQLite 抓明文 JWT |

此外三个平台都会回退到旧布局 `~/.workbuddy/auth/`、`~/.codebuddy/auth/`。

Linux 的 `automations.db` 里混着不少**被截断的 JWT 残片**，所以 Linux 版会把候选令牌
按「优选指纹 → 签发时间 → 长度」排序后**逐个真去请求接口**，第一个成功的就是要用的那个，
并把它的指纹存下来供下次优先使用。

接口同为：

```
POST https://copilot.tencent.com/billing/meter/get-user-resource-summary
Authorization: Bearer <accessToken>
X-User-Id: <userId>
```

Windows 版写 `config.json` 时采用「读-改-写」，只覆盖自己认识的键，
因此 macOS 版写入的其它字段（包括 `"//"` 注释键）不会被抹掉。
