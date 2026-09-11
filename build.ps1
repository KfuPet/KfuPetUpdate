#Requires -Version 5.1
<#
.SYNOPSIS
    KfuPetUpdate 一键构建脚本。

.DESCRIPTION
    按需重新生成 Windows 资源对象（app.rc / app.manifest / icon/app.ico 有更新时），
    然后编译出带图标、版本信息与管理员清单的 dist/KfuPetUpdate.exe。

.EXAMPLE
    .\build.ps1
    只打包到 dist/KfuPetUpdate.exe。

.EXAMPLE
    .\build.ps1 -Run
    打包完成后立即启动 exe（会弹 UAC）。
#>
param(
    [switch]$Run
)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

$syso = 'app_windows_amd64.syso'
$srcs = @('app.rc', 'app.manifest', 'icon/app.ico')
$exe = Join-Path $PWD 'dist/KfuPetUpdate.exe'

# 1. 仅当资源源文件比 syso 新时，才重新生成（需要 windres）
$needGen = -not (Test-Path -LiteralPath $syso)
if (-not $needGen) {
    $sysoTime = (Get-Item -LiteralPath $syso).LastWriteTimeUtc
    foreach ($s in $srcs) {
        if ((Test-Path -LiteralPath $s) -and (Get-Item -LiteralPath $s).LastWriteTimeUtc -gt $sysoTime) {
            $needGen = $true
            break
        }
    }
}

if ($needGen) {
    if (-not (Get-Command windres -ErrorAction SilentlyContinue)) {
        # windres 不一定在 PATH 中，尝试常见的 MSYS2 安装位置
        $candidates = @()
        if ($env:MSYS2_ROOT) { $candidates += (Join-Path $env:MSYS2_ROOT 'mingw64\bin') }
        $candidates += @('C:\msys64\mingw64\bin', 'C:\msys64\ucrt64\bin')
        foreach ($p in $candidates) {
            if ((Test-Path -LiteralPath (Join-Path $p 'windres.exe'))) {
                $env:PATH = "$p;$env:PATH"
                break
            }
        }
    }
    if (-not (Get-Command windres -ErrorAction SilentlyContinue)) {
        throw '资源源文件有更新，但未找到 windres。请安装 MSYS2 并把其 bin 目录加入 PATH（详见 README）。'
    }

    Write-Host '==> 资源有更新，重新生成 app_windows_amd64.syso' -ForegroundColor Cyan
    go generate ./...
    if ($LASTEXITCODE -ne 0) { throw "go generate 失败（退出码 $LASTEXITCODE）" }
}
else {
    Write-Host '==> 资源对象已是最新，跳过生成' -ForegroundColor DarkGray
}

# 2. 编译（-H=windowsgui 必须带上，否则双击运行会多出黑色控制台窗口）
Write-Host '==> 编译 dist/KfuPetUpdate.exe' -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path dist | Out-Null
go build -ldflags '-H=windowsgui' -o dist/KfuPetUpdate.exe .
if ($LASTEXITCODE -ne 0) { throw "go build 失败（退出码 $LASTEXITCODE）" }
Write-Host "==> 完成：$exe" -ForegroundColor Green

if ($Run) {
    Write-Host '==> 启动 exe（会请求管理员权限）' -ForegroundColor Cyan
    Start-Process -FilePath $exe
}
