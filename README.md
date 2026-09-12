# KfuPetUpdate
这个是KfuPet配套的安装、更新、卸载程序

>本工具为独立部署程序，仅通过网络获取并分发配套 KfuPet 程序。KfuPet 本体为独立作品，使用 AGPL-3.0 许可证，[详见其独立仓库](https://github.com/KfuPet/KfuPet)。

## 开发指南

### 环境要求
- [Go](https://go.dev/) 1.27+
- Windows 系统，且装有 MSYS2 / MinGW（含 `gcc`）：
  - Fyne 桌面程序依赖 CGO 编译，需要 `gcc`
  - exe 图标与清单资源已随仓库提交；仅在修改图标、版本信息或清单后重新生成时才需要 `windres`

### Windows 资源对象（图标、版本信息、清单）
`app_windows_amd64.syso` 由 `app.rc`（图标 + 版本信息 + `app.manifest`）经 `windres` 编译得到，**已随仓库提交**，clone 后可直接编译出带图标、版本信息和管理员权限要求的 exe，无需额外工具链。

`app.manifest` 声明 `requireAdministrator`：安装要写入 `%ProgramFiles%` 并创建快捷方式，因此 **exe 每次启动都会弹 UAC**。

**仅当修改了 `icon/app.ico`、`app.rc` 或 `app.manifest` 时**，才需重新生成并提交（需 MSYS2 的 `windres`）：

```powershell
go generate ./...
# 等价于：windres -c 65001 app.rc -O coff -o app_windows_amd64.syso
```

> `-c 65001` 指定源文件为 UTF-8；缺省时 `windres` 会按本地代码页（GBK）解析，
> 导致 `app.rc` 中 VERSIONINFO 的中文（如「文件说明」）在 exe 属性里显示为乱码。

### 运行与打包
```powershell
# 一键构建（推荐）：资源有更新才重新生成 syso → 打包到 dist/KfuPetUpdate.exe
.\build.ps1

# 打包后立即启动（会弹 UAC）
.\build.ps1 -Run
```

手动执行等价命令：

```powershell
# 运行（启动即请求管理员权限 → 闪屏 → 从 GitHub 查询 KfuPet 最新版本 → 主界面）
# 开发时 go run 的控制台附在当前终端上，属正常现象
go run .

# 打包（输出到 dist/）
# -H=windowsgui 必须带上：否则 exe 是控制台子系统，双击运行会多出一个黑色命令行窗口
New-Item -ItemType Directory -Force -Path dist | Out-Null
go build -ldflags -H=windowsgui -o dist/KfuPetUpdate.exe .
```

### 安装流程
主界面点「安装」进入向导：**选择安装位置**（默认 `%ProgramFiles%\KfuPet`，可浏览自定义）→ **安装选项**（桌面/开始菜单快捷方式）→ 安装。安装目录整体替换（先落暂存目录再改名），下载带 sha256 校验，快捷方式经 PowerShell 调 `WScript.Shell` 创建。

下载安装包走 GitHub Releases 直链；连接被重置、握手超时这类瞬时失败很常见，因此最多尝试 **3 次**、
每次间隔 2 秒（安装被取消或整体超时则立即停止，不再重试）。重试时进度条会从 0 重走一遍。

安装完成后询问是否立即启动 KfuPet：选「是」会拉起刚装好的程序（继承 updater 的管理员权限），选「否」则不启动；两者都会退出 updater。

安装时会把 updater 自身复制一份到安装目录（`KfuPetUpdate.exe`）常驻：标准卸载入口与 KfuPet 的「检查更新」都指向这个固定位置，用户删掉当初下载的 updater 也不影响后续卸载与升级。

### 卸载流程
主界面点「卸载」→ 确认框（内含「保留个人数据」勾选，默认勾上）→ 删除安装目录 → 删除桌面/开始菜单快捷方式 → 按选择删除 `%APPDATA%\KfuPet` 等个人数据 → 最后删除注册表记录。

顺序是刻意的：**先把文件删干净，最后才删注册表**。中间任何一步失败都保留注册表，用户重试才有据可依；反过来会留下"显示未安装、文件却还在"的状态，用户以为卸干净了，比直接报错更糟。KfuPet 正在运行时会被先拦下。

安装时还会写入 `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\KfuPet`，使本程序出现在 Windows「设置 → 应用和功能」中。`UninstallString` 指向安装目录内的常驻副本并带 `--action=uninstall`，点卸载会直接弹出卸载确认框。

### 命令行参数
不带参数时打开图形界面（主界面按安装状态提供安装/升级/卸载）。

| 参数 | 说明 |
| --- | --- |
| `--action=update` | 升级入口（**仅预留，尚未实现**，当前只会提示未实现） |
| `--action=uninstall` | 卸载：直接弹出卸载确认框 |
| `--action=uninstall --yes` | 静默卸载（不询问） |
| `--purge-data` | 与卸载搭配：一并删除个人数据 |
| `--notify` | 与静默卸载搭配：完成后弹系统提示框（界面已退出时用） |
| `--dir=<路径>` | 指定安装目录；缺省读注册表记录 |
| `--wait-pid=<pid>` | 动手前等该进程退出，可重复传入 |

桌宠发起更新的约定（待升级实现后生效）：以管理员方式（`ShellExecute`，会弹一次 UAC）拉起
`%ProgramFiles%\KfuPet\KfuPetUpdate.exe --action=update --wait-pid=<桌宠 PID>`，随后桌宠自行退出。

`--action=uninstall --yes` 若发现自身正运行在安装目录内，会先把自己复制到 `%TEMP%` 重启一个副本来执行
（运行中的 exe 无法删除/替换自己），原进程退出后再动手；副本退出时会自删该临时目录，
崩溃留下的由下次启动兜底清理。

同一时刻只允许一个 updater 实例运行（命名互斥体 `Local\KfuPetUpdate-Singleton`）：两个实例同时
安装/卸载会争抢同一个安装目录，而且常驻副本正在运行时，别的实例既替换不了也删不掉它。
所以启动时若已有实例在跑，会提示「另一个 KfuPet 更新程序正在运行」并退出。

### 目录结构
- `build.ps1`：一键构建脚本（按需重新生成 syso → 打包 exe，`-Run` 可打包后立即启动）；**须保持 UTF-8 with BOM 编码**，否则 PowerShell 5.1 按 GBK 解析会导致中文报错
- `main.go`：界面层（启动闪屏、主界面、安装向导各页面）
- `update.go`：版本查询逻辑（GitHub 源优先，自建服务器源为占位空壳，失败时回退），以及发布版产物（安装包）的解析与挑选
- `install.go`：安装状态检测（读取注册表安装记录并校验安装目录内程序是否仍存在）
- `installer.go`：安装流程（下载 → sha256 校验 → 解压 → 替换安装目录 → 创建快捷方式 → 写注册表）
- `uninstall.go`：卸载流程（删安装目录 → 删快捷方式 → 按选择删个人数据 → 删注册表）与标准卸载入口的内容组装
- `cli.go`：命令行参数解析与静默卸载（`--action=update` 目前仅预留入口）
- `selfcopy.go`：自身复制、临时副本接力与临时目录清理
- `process_windows.go` / `process_other.go`：启动临时副本、等待目标进程退出、临时副本目录的自删安排
- `lock_windows.go` / `lock_other.go`：单实例互斥锁（Windows 用命名互斥体，非 Windows 空实现）
- `notify_windows.go` / `notify_other.go`：静默模式下的错误提示（Windows 用系统弹窗）
- `shortcut_windows.go` / `shortcut_other.go`：快捷方式的创建与删除（Windows 实现与非 Windows 空实现）
- `registry_windows.go` / `registry_other.go`：安装记录与标准卸载入口的注册表读写（Windows 实现与非 Windows 空实现）
- `app.rc`：Windows 资源脚本（exe 图标 + 属性「详细信息」版本信息 + 应用程序清单）
- `app.manifest`：应用程序清单（声明 `requireAdministrator`）
- `app_windows_amd64.syso`：由 `app.rc` 编译出的 Windows 资源对象（已提交）
- `icon/`：图标素材（`Startlogo.png` 主界面 Logo、`appicon.png` 非 Windows 平台窗口图标、`app.ico` exe 多尺寸图标）
- `dist/`：打包输出目录（`go build -o dist/`，已在 `.gitignore` 忽略）
