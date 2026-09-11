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

安装完成后询问是否立即启动 KfuPet：选「是」会拉起刚装好的程序（继承 updater 的管理员权限），选「否」则不启动；两者都会退出 updater。

### 目录结构
- `build.ps1`：一键构建脚本（按需重新生成 syso → 打包 exe，`-Run` 可打包后立即启动）；**须保持 UTF-8 with BOM 编码**，否则 PowerShell 5.1 按 GBK 解析会导致中文报错
- `main.go`：界面层（启动闪屏、主界面、安装向导各页面）
- `update.go`：版本查询逻辑（GitHub 源优先，自建服务器源为占位空壳，失败时回退），以及发布版产物（安装包）的解析与挑选
- `install.go`：安装状态检测（读取注册表安装记录并校验安装目录内程序是否仍存在）
- `installer.go`：安装流程（下载 → sha256 校验 → 解压 → 替换安装目录 → 创建快捷方式 → 写注册表）
- `shortcut_windows.go` / `shortcut_other.go`：快捷方式创建（Windows 实现与非 Windows 空实现）
- `registry_windows.go` / `registry_other.go`：安装记录的注册表读写（Windows 实现与非 Windows 空实现）
- `app.rc`：Windows 资源脚本（exe 图标 + 属性「详细信息」版本信息 + 应用程序清单）
- `app.manifest`：应用程序清单（声明 `requireAdministrator`）
- `app_windows_amd64.syso`：由 `app.rc` 编译出的 Windows 资源对象（已提交）
- `icon/`：图标素材（`Startlogo.png` 主界面 Logo、`appicon.png` 非 Windows 平台窗口图标、`app.ico` exe 多尺寸图标）
- `dist/`：打包输出目录（`go build -o dist/`，已在 `.gitignore` 忽略）
