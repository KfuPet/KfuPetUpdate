# KfuPetUpdate
这个是KfuPet配套的安装、更新、卸载程序

>本工具为独立部署程序，仅通过网络获取并分发配套 KfuPet 程序。KfuPet 本体为独立作品，使用 AGPL-3.0 许可证，[详见其独立仓库](https://github.com/KfuPet/KfuPet)。

## 开发指南

### 环境要求
- [Go](https://go.dev/) 1.27+
- Windows 系统，且装有 MSYS2 / MinGW（含 `gcc` 与 `windres`）：
  - Fyne 桌面程序依赖 CGO 编译，需要 `gcc`
  - exe 文件图标由 `windres` 从资源脚本生成，也需要它

### 生成 Windows 资源对象
`app_windows_amd64.syso`（由 `app.rc` + `icon/app.ico` 编译得到）是生成物，已在 `.gitignore` 中忽略。**clone 仓库后编译前需先生成一次**，否则编译出的 exe 不带图标：

```powershell
go generate ./...
# 等价于：windres app.rc -O coff -o app_windows_amd64.syso
```

### 运行与打包
```powershell
# 运行（启动闪屏 → 从 GitHub 查询 KfuPet 最新版本 → 主界面）
go run .

# 打包带图标的 exe
go generate ./...
go build -o KfuPetUpdate.exe .
```

### 目录结构
- `main.go`：界面层（启动闪屏/版本查询展示、主界面布局）
- `update.go`：版本查询逻辑（GitHub 源优先，自建服务器源为占位空壳，失败时回退）
- `app.rc`：Windows 图标资源脚本
- `icon/`：图标素材（`Startlogo.png` 主界面 Logo、`appicon.png` 非 Windows 平台窗口图标、`app.ico` exe 多尺寸图标）
