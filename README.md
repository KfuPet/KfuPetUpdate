# KfuPetUpdate
这个是KfuPet配套的安装、更新、卸载程序

>本工具为独立部署程序，仅通过网络获取并分发配套 KfuPet 程序。KfuPet 本体为独立作品，使用 AGPL-3.0 许可证，[详见其独立仓库](https://github.com/KfuPet/KfuPet)。

## 开发指南

### 环境要求
- [Go](https://go.dev/) 1.27+
- Windows 系统，且装有 MSYS2 / MinGW（含 `gcc`）：
  - Fyne 桌面程序依赖 CGO 编译，需要 `gcc`
  - exe 图标资源已随仓库提交；仅在修改图标后重新生成时才需要 `windres`

### Windows 资源对象（图标）
`app_windows_amd64.syso` 由 `app.rc` + `icon/app.ico` 经 `windres` 编译得到，**已随仓库提交**，clone 后可直接编译出带图标的 exe，无需额外工具链。

**仅当修改了 `icon/app.ico` 或 `app.rc` 时**，才需重新生成并提交（需 MSYS2 的 `windres`）：

```powershell
go generate ./...
# 等价于：windres app.rc -O coff -o app_windows_amd64.syso
```

### 运行与打包
```powershell
# 运行（启动闪屏 → 从 GitHub 查询 KfuPet 最新版本 → 主界面）
go run .

# 打包带图标的 exe（输出到 dist/）
New-Item -ItemType Directory -Force -Path dist | Out-Null
go build -o dist/KfuPetUpdate.exe .
```

### 目录结构
- `main.go`：界面层（启动闪屏/版本查询展示、主界面布局）
- `update.go`：版本查询逻辑（GitHub 源优先，自建服务器源为占位空壳，失败时回退）
- `app.rc`：Windows 图标资源脚本
- `app_windows_amd64.syso`：由 `app.rc` 编译出的 Windows 图标资源对象（已提交）
- `icon/`：图标素材（`Startlogo.png` 主界面 Logo、`appicon.png` 非 Windows 平台窗口图标、`app.ico` exe 多尺寸图标）
- `dist/`：打包输出目录（`go build -o dist/`，已在 `.gitignore` 忽略）
