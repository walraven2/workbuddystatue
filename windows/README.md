# WorkBuddy 积分小工具（Windows）

在任务栏托盘常驻显示 WorkBuddy 积分，并在桌面右上角放一个可拖动的悬浮窗。

```
┌──────────┐                                    ┌─────────────┐
│  ⚡ (托盘) │   ← 鼠标悬停看明细、右键出菜单       │ ⚡ 1,494     │ ← 可拖动
└──────────┘                                    └─────────────┘
```

## 特性

- **托盘常驻**：系统托盘图标 + 悬停提示（余额、签到状态、更新时间）
- **桌面悬浮窗**：深色圆角小挂件显示 `⚡ 1,494`，位置可拖动并自动记住
- **自动读取登录态**：直接复用 WorkBuddy 桌面端已登录的令牌，**无需手动填 token**
- **积分明细**：右键菜单展示各套餐（成长计划、奖励积分等）的剩余 / 总量
- **签到状态**：今日是否签到、连续天数、本周签到天数
- **自动刷新**：可选 1 / 5 / 15 / 30 分钟、1 小时，或仅手动刷新
- **开机自启**：菜单内一键开关，写入 `HKCU\...\CurrentVersion\Run`
- **零依赖**：纯 Go 标准库，不依赖 .NET / VC++ 运行库，单个 exe 约 7.8 MB

## 环境要求

- Windows 10 及以上，64 位
- 已安装并登录 WorkBuddy 桌面端（否则读不到令牌）

## 使用

### 直接下载

到 [Releases](https://github.com/walraven2/workbuddystatue/releases) 下载
`WorkBuddyStatus-<版本>-windows-amd64.zip`，解压得到 `WorkBuddyStatus.exe`，双击即可。

程序是 GUI 子系统，双击运行没有控制台窗口，直接出图标和悬浮窗。

### 建议先跑一次诊断

第一次在本机运行时，建议先确认凭据和网络都通：

```powershell
.\WorkBuddyStatus.exe --check
```

因为程序是 GUI 子系统，在 PowerShell / CMD 里运行时会主动附加到父进程控制台，
所以诊断结果能直接打印在终端里；如果在资源管理器里双击，结果会以弹窗显示。

诊断会依次检查：配置文件、凭据目录（逐个列出并标注是否命中）、令牌有效性、
接口连通性与耗时、积分明细、签到状态，并写入
`%USERPROFILE%\.workbuddy-status\last-check.txt`。

## 命令行参数

| 参数 | 说明 |
| --- | --- |
| （无参数） | 启动托盘图标与悬浮窗 |
| `--check` / `--diagnose` | 无界面诊断，打印凭据、令牌与接口连通性 |
| `--enable-autostart` | 开启开机自启 |
| `--disable-autostart` | 关闭开机自启 |
| `--help` | 显示帮助 |

## 右键菜单

```
👤 账户名
💰 剩余 1,494 / 16,242
✅ 今日已签到 · 连续 3 天 · 本周 5 天
🕘 更新于 09:31:05
─────────────────────
📦 积分套餐          ▸  各套餐的剩余 / 总量
⏱ 自动刷新          ▸  仅手动 / 1 分钟 / 5 分钟 / 15 分钟 / 30 分钟 / 1 小时
🎚 显示方式          ▸  积分数值 / 剩余百分比 / 仅图标
🖥 桌面悬浮窗             开关
🚀 开机自启               开关
─────────────────────
🔄 立即刷新
🪟 打开 WorkBuddy
📋 复制余额明细
📁 打开配置目录
─────────────────────
退出
```

菜单里的信息行（账户、余额、签到、更新时间）是灰色不可点的标签行，
用来一眼看到状态；所有可操作项都在下方。

**悬浮窗操作**：左键拖动移动位置（松手即保存），右键弹出同一个菜单。

## 配置

配置文件：`%USERPROFILE%\.workbuddy-status\config.json`（菜单里可一键打开目录）

首次运行会自动生成带注释的模板：

```json
{
  "//": "accessToken / userId 留空时会自动从 WorkBuddy 桌面端读取，通常无需填写。",
  "endpoint": "https://copilot.tencent.com",
  "accessToken": "",
  "userId": "",
  "refreshInterval": 300,
  "displayMode": "value",
  "badgePrefix": "⚡ ",
  "showBadge": true
}
```

| 键 | 说明 |
| --- | --- |
| `refreshInterval` | 自动刷新间隔（秒），`0` 表示仅手动刷新 |
| `displayMode` | `value` 积分数值 / `percent` 剩余百分比 / `icon` 仅图标 |
| `badgePrefix` | 悬浮窗文字前缀，默认 `⚡ ` |
| `showBadge` | 是否显示桌面悬浮窗；`badgeX` / `badgeY` 会记录拖动后的位置 |

> **如果前缀显示成方块**：个别系统的中文字体缺少 `⚡`（U+26A1）字形。
> 程序启动时会用 `GetGlyphIndicesW` 探测一次，缺字形就自动换成 `积分 `，
> 也可以手动把 `badgePrefix` 改成 `积分 `。
>
> 配置文件与 macOS 版共用格式，同一份 `config.json` 两边都能读；
> 本工具只改自己认识的键，其它键一律原样保留。

也可以用环境变量覆盖：`WORKBUDDY_ENDPOINT`、`WORKBUDDY_ACCESS_TOKEN`、`WORKBUDDY_USER_ID`。

运行日志：`%USERPROFILE%\.workbuddy-status\last-error.log`（超过 512 KB 自动裁剪）

## 凭据来源

按以下顺序查找：

1. `config.json` 里的 `accessToken`，或环境变量 `WORKBUDDY_ACCESS_TOKEN`
2. `%LOCALAPPDATA%\CodeBuddyExtension\Data\Public\auth\*.info`（WorkBuddy 桌面端登录信息）
3. `%APPDATA%\CodeBuddyExtension\...`、`~/.local/share/CodeBuddyExtension/...`（兼容其它布局）
4. `~/.workbuddy/auth/`、`~/.codebuddy/auth/`（兼容旧布局）

第 2 条是从 WorkBuddy 桌面端源码里的 `FilePathServiceImpl.getBasePath()` 推出来的：
Windows 分支用的是 **`AppData\Local`**（不是 `Roaming`），凭据文件是
`auth\workbuddy-desktop.info`。同一目录下若有多个 `.info`，取修改时间最新的那个。

取到 JWT 后本地解析 `exp` 判断是否过期，过期会在菜单里提示重新登录。

## 从源码编译

在 macOS / Linux 上就能交叉编译，**不需要 Windows，也不需要 cgo**——
程序只用 Go 标准库，所有 Win32 调用都通过 `syscall.NewLazyDLL` 完成。

```bash
cd windows

./build-windows.sh              # 生成 dist/WorkBuddyStatus.exe 与 zip
./build-windows.sh --no-zip     # 只要 exe
./build-windows.sh --arm64      # 编译 ARM64 版本
```

脚本做的事：生成资源文件 → `go vet` → 交叉编译 → 打包并打印 SHA-256。

也可以直接手工编译：

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build \
    -trimpath -ldflags "-s -w -H windowsgui" -o dist/WorkBuddyStatus.exe .
```

> `rsrc_windows_amd64.syso` 是生成产物，**不在版本库里**。直接手工编译前要先跑一次
> `go run ./tools/mkrsrc ...`（或直接执行 `build-windows.sh`），否则 exe 没有图标、
> 清单和版本信息。

### 关于 `.syso`：为什么需要它

Go 链接器默认不会往 exe 里放图标和清单。但只要有任何目标文件带有名为 `.rsrc` 的节，
`cmd/link` 就会把它原样搬进 PE 并填好资源数据目录
（见 `cmd/link/internal/ld/pe.go` 的 `addpersrc()`）。

`tools/mkrsrc` 就是为这个手写的、约 380 行的 COFF 目标文件生成器，它把一个
`.rsrc` 节（`RT_ICON` × 8 + `RT_GROUP_ICON` + `RT_VERSION` + `RT_MANIFEST`）
连同重定位表打包成 `rsrc_windows_amd64.syso`。

其中 `RT_MANIFEST` 尤其重要：**没有清单，Windows 会把程序当成 DPI 不感知的旧程序**，
在 125% / 150% 缩放的屏幕上整个窗口被系统拉伸，文字会糊。清单里声明了
`dpiAware=true`，悬浮窗的尺寸则由 `computeMetrics()` 按实际 DPI 换算。

## 源码结构

```
windows/
├── main.go              入口：单实例互斥体、命令行、消息循环启动
├── app.go               界面层：托盘、悬浮窗绘制与拖动、菜单、刷新调度
├── win32.go             Win32 绑定：LazyDLL 过程句柄与通用封装
├── autostart.go         开机自启（HKCU 的 Run 键）
├── diagnostics.go       --check 诊断报告
├── internal/core/       平台无关的核心（可在 macOS 上跑单元测试）
│   ├── model.go         数据模型与接口响应解析
│   ├── api.go           积分接口客户端
│   ├── credential.go    凭据探测与 JWT 解析
│   ├── config.go        配置读写与日志
│   ├── format.go        数值格式化与文案
│   ├── errors.go        带中文提示的错误类型
│   ├── ico.go           ICO 解析
│   ├── win32const.go    Win32 常量与 NOTIFYICONDATAW
│   └── win32struct.go   Win32 结构体（含布局单元测试）
├── tools/
│   ├── makeico/         程序化生成 Resources/AppIcon.ico
│   ├── ico2png/         把 ico 的某帧导成 png，用于肉眼检查
│   └── mkrsrc/          生成 rsrc_windows_amd64.syso
├── Resources/
│   ├── AppIcon.ico      8 个尺寸（16～256），全部 BMP 帧
│   └── app.manifest     清单：DPI 感知、兼容性、asInvoker
└── build-windows.sh     一条命令走完生成资源 → 检查 → 编译 → 打包
```

### 为什么把逻辑放进 `internal/core`

macOS 上无法运行 Windows 二进制，而 Win32 界面层正是那种「编译得过、运行时崩」
的代码。所以凡是能剥离的都被剥离到 `internal/core`：数据解析、凭据、格式化、
ICO 解析，以及**直接按字节传给系统 API 的结构体**。

结构体的字段偏移写错了编译器不会报错，只会在 Windows 上崩或者静默失效，
因此 `win32struct_test.go` 用 `unsafe.Sizeof` / `Offsetof` 逐个校验
`MSG` / `WNDCLASSEXW` / `PAINTSTRUCT` / `POINT` / `RECT`，
`core_test.go` 校验 `NOTIFYICONDATAW` 的 V2 布局（x64 下 952 字节，
交叉验证：加上 GUID 与 `hBalloonIcon` 得到的 V3 是 976，与公开资料一致）。

## 已知限制

- **Win32 界面层没有可执行测试**。Mac 上没有 Wine，只有平台无关的部分跑过单元测试。
  第一次运行如果异常，先看 `--check` 的输出和 `last-error.log`。
- 悬浮窗固定为「系统 DPI 感知」，不处理跨显示器拖动时的 DPI 变化：
  在缩放比例不同的两块屏之间拖动时，挂件尺寸不会重新换算。
- 没有做主题跟随（深色 / 浅色）；悬浮窗固定使用深色 HUD 配色，
  这样在任何壁纸下都清晰。

## 卸载

```powershell
.\WorkBuddyStatus.exe --disable-autostart    # 关掉开机自启
Remove-Item -Recurse -Force "$env:USERPROFILE\.workbuddy-status"
```

然后删除 exe 即可。
