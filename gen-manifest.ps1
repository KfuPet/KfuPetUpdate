#Requires -Version 5.1
<#
.SYNOPSIS
    KfuPet 发布清单生成脚本。

.DESCRIPTION
    生成 KfuPet-manifest.json：压缩包 sha256 + 随包发布的根级文件哈希 +
    本安装器（KfuPetInstall.exe）自身的哈希与版本。

    压缩包哈希供安装时校验安装包，「修复」按根级文件哈希逐文件校验；
    安装器那一段供安装/升级/修复把用户机上的常驻更新程序（KfuPetUpdate.exe）
    换成发布版这一份——更新程序没有别的更新通道，只能随 KfuPet 的发布版一起走。

    收录范围只有压缩包根目录下的文件（KfuPet.exe / KfuPet.dll 及两个 json）；
    Characters 等子目录属于角色模型内容，用户可自行增删改，不纳入清单。
    安装器（-Installer）不在压缩包内，是随 Release 单独上传的第二个附件。

    生成时会把「目录内容」与「压缩包内容」逐文件核对，两边哈希必须一致，
    否则报错退出——避免先打包、后又改动目录（或拿错包）导致清单与包不匹配。

    压缩包不存在时会自动按目录内容打一个（zip 根下直接放文件，不含顶层目录），
    省去手工打包这一步；已存在的压缩包不会被改动。

.EXAMPLE
    .\gen-manifest.ps1 -Dir ..\dit\KfuPet-windows-amd64 -Zip ..\dit\KfuPet-windows-amd64.zip -Version v0.0.10
    压缩包不存在时自动打包；清单输出到 zip 同目录的 KfuPet-manifest.json。

.EXAMPLE
    .\gen-manifest.ps1 -Dir ..\dit\KfuPet-windows-amd64 -Zip ..\dit\KfuPet-v0.0.10-windows-amd64.zip
    版本号从 zip 文件名解析（v0.0.10）。
#>
param(
    [Parameter(Mandatory = $true)][string]$Dir,
    [Parameter(Mandatory = $true)][string]$Zip,
    # 发布 tag（可带 v 前缀）；缺省从 zip 文件名里找形如 1.2.3 的版本号
    [string]$Version,
    # 清单输出路径；缺省为 zip 同目录下的 KfuPet-manifest.json
    [string]$Out,
    # 本安装器 exe 的路径；缺省取仓库里的 dist/KfuPetInstall.exe。
    # 它的哈希与版本写进清单的 installer 段，须与 zip 一起上传到同一个 Release。
    [string]$Installer
)

$ErrorActionPreference = 'Stop'

# 随包发布、由安装器独占管理的文件：缺任何一个都不能发版
$essential = @('KfuPet.exe', 'KfuPet.dll', 'KfuPet.deps.json', 'KfuPet.runtimeconfig.json')
# 资源管理器 / 缩略图缓存之类的杂物，不收录
$junk = @('desktop.ini', 'Thumbs.db')

# New-ReleaseZip 把目录内容打成一个 zip，文件直接位于包根（不套顶层目录）。
# 不用 Compress-Archive：它依赖的 .NET 版本在部分环境下会把条目名写成反斜杠，
# 而 installer 与本脚本都按 '/' 解析包内路径。这里显式指定条目名，结果确定。
function New-ReleaseZip {
    param(
        [Parameter(Mandatory = $true)][string]$SourceDir,
        [Parameter(Mandatory = $true)][string]$ZipPath
    )

    Add-Type -AssemblyName System.IO.Compression
    Add-Type -AssemblyName System.IO.Compression.FileSystem

    if ($parent = Split-Path -Parent $ZipPath) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }

    $base = $SourceDir.TrimEnd('\', '/')
    $archive = [IO.Compression.ZipFile]::Open($ZipPath, [IO.Compression.ZipArchiveMode]::Create)
    try {
        foreach ($f in Get-ChildItem -LiteralPath $base -Recurse -File) {
            # 杂物不打包：清单里也没有它们，否则会被判成"包根多出的文件"
            if ($junk -contains $f.Name) { continue }
            $rel = $f.FullName.Substring($base.Length + 1).Replace('\', '/')
            [IO.Compression.ZipFileExtensions]::CreateEntryFromFile(
                $archive, $f.FullName, $rel, [IO.Compression.CompressionLevel]::Optimal) | Out-Null
        }
    }
    finally { $archive.Dispose() }
}

if (-not (Test-Path -LiteralPath $Dir -PathType Container)) { throw "目录不存在：$Dir" }
$Dir = (Resolve-Path -LiteralPath $Dir).Path

# 安装器自身的信息：哈希用于判断用户机上的常驻更新程序是否该换成这一份，
# 版本只用于给用户看的文案（判定一律以哈希为准，不依赖版本号）。
# 排在自动打包之前：路径不合规时直接报错，不留下一个没用的包。
if (-not $Installer) { $Installer = Join-Path $PSScriptRoot 'dist/KfuPetInstall.exe' }
$installerEntry = $null
if (Test-Path -LiteralPath $Installer -PathType Leaf) {
    $exePath = (Resolve-Path -LiteralPath $Installer).Path
    if ($exePath.StartsWith($Dir + '\', [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "安装器不能放在 -Dir 之内：$exePath 会被当成随包发布的 KfuPet 文件"
    }
    $exe = Get-Item -LiteralPath $exePath
    # 版本取自 exe 资源（app.rc 的 VERSIONINFO）；取不到就留空，只影响展示文案
    $exeVer = ''
    if ($exe.VersionInfo.FileVersion) { $exeVer = $exe.VersionInfo.FileVersion }
    elseif ($exe.VersionInfo.ProductVersion) { $exeVer = $exe.VersionInfo.ProductVersion }
    $installerEntry = [pscustomobject]@{
        name    = $exe.Name
        sha256  = (Get-FileHash -LiteralPath $exe.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        size    = $exe.Length
        version = $exeVer.Trim().TrimStart('v', 'V')
    }
}
else {
    Write-Warning "未找到安装器 $Installer，清单不含 installer 段：本次发布不会更新用户机上的常驻更新程序。"
}

# 压缩包不存在就地打一个，省去手工打包。已存在的包不动：它可能已经发出去了，
# 悄悄覆盖会让清单里的 sha256 与用户手上的包对不上。
if (-not (Test-Path -LiteralPath $Zip -PathType Leaf)) {
    Write-Host "==> 压缩包不存在，按目录内容自动打包：$Zip" -ForegroundColor Cyan
    New-ReleaseZip -SourceDir $Dir -ZipPath $Zip
}
$Zip = (Resolve-Path -LiteralPath $Zip).Path

if (-not $Version) {
    $m = [regex]::Match([IO.Path]::GetFileName($Zip), '\d+(?:\.\d+)+')
    if (-not $m.Success) {
        throw '无法从压缩包文件名解析版本号，请用 -Version 指定（如 -Version v0.0.10）'
    }
    $Version = $m.Value
}
$Version = $Version.Trim().TrimStart('v', 'V')
if (-not $Version) { throw '版本号为空' }

# 1. 收录根级文件并计算哈希
$files = @(Get-ChildItem -LiteralPath $Dir -File |
    Where-Object { $junk -notcontains $_.Name } | Sort-Object Name)
foreach ($name in $essential) {
    if (-not ($files.Name -contains $name)) { throw "目录里缺少随包发布的关键文件：$name" }
}

$entries = @(foreach ($f in $files) {
    [pscustomobject]@{
        path   = $f.Name
        sha256 = (Get-FileHash -LiteralPath $f.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        size   = $f.Length
    }
})

$zipSha256 = (Get-FileHash -LiteralPath $Zip -Algorithm SHA256).Hash.ToLowerInvariant()

# 2. 与压缩包逐文件核对（zip 内可能带统一顶层目录，需剥掉后比对）
Add-Type -AssemblyName System.IO.Compression.FileSystem
$archive = [IO.Compression.ZipFile]::OpenRead($Zip)
try {
    $prefix = ''
    $roots = @{}
    $rootLevel = $false
    foreach ($e in $archive.Entries) {
        $i = $e.FullName.IndexOf('/')
        if ($i -le 0) { $rootLevel = $true; break } # 条目直接位于包根
        $seg = $e.FullName.Substring(0, $i)
        if ($seg -eq '.' -or $seg -eq '..') { $rootLevel = $true; break }
        $roots[$seg] = $true
    }
    # 仅当所有条目都在同一个顶层目录之下时才剥掉（与 installer 的规则一致）
    if (-not $rootLevel -and $roots.Count -eq 1) {
        $prefix = ($roots.Keys | Select-Object -First 1) + '/'
    }

    $inZip = @{}
    foreach ($e in $archive.Entries) {
        if ($e.FullName.EndsWith('/') -or $e.Name -eq '') { continue } # 目录条目
        $rel = $e.FullName
        if ($prefix -and $rel.StartsWith($prefix)) { $rel = $rel.Substring($prefix.Length) }
        $inZip[$rel] = $e
    }

    foreach ($entry in $entries) {
        if (-not $inZip.ContainsKey($entry.path)) {
            throw "压缩包里找不到 $($entry.path)：目录与压缩包不是同一份内容"
        }
        $stream = $inZip[$entry.path].Open()
        $sha = [Security.Cryptography.SHA256]::Create()
        try {
            $hash = [BitConverter]::ToString($sha.ComputeHash($stream)).Replace('-', '').ToLowerInvariant()
        }
        finally { $sha.Dispose(); $stream.Dispose() }
        if ($hash -ne $entry.sha256) {
            throw "$($entry.path) 在目录与压缩包中不一致（目录 $($entry.sha256)，包内 $hash）——是否先打包后又改了目录？"
        }
    }

    # 包根多出的文件同样算不一致（子目录内容如 Characters 不比对）
    foreach ($rel in $inZip.Keys) {
        if ($rel.Contains('/')) { continue }
        if (-not ($entries.path -contains $rel)) {
            throw "压缩包根目录多出目录里没有的文件：$rel"
        }
    }
}
finally { $archive.Dispose() }

# 3. 写清单（UTF-8 无 BOM：Go 侧 encoding/json 不认 BOM）
$manifest = [ordered]@{
    version   = $Version
    zipSha256 = $zipSha256
    files     = $entries
}
if ($installerEntry) { $manifest['installer'] = $installerEntry }
$json = [pscustomobject]$manifest | ConvertTo-Json -Depth 5

if (-not $Out) { $Out = Join-Path (Split-Path -Parent $Zip) 'KfuPet-manifest.json' }
[IO.File]::WriteAllText($Out, $json + "`n", (New-Object System.Text.UTF8Encoding($false)))

Write-Host "==> 已生成清单：$Out" -ForegroundColor Green
Write-Host ("    版本    ：{0}" -f $Version)
Write-Host ("    压缩包  ：{0}（sha256 {1}）" -f (Split-Path -Leaf $Zip), $zipSha256)
Write-Host ("    收录文件：{0} 个根级文件" -f $entries.Count)
foreach ($e in $entries) {
    Write-Host ("      {0,9}  {1}  {2}" -f $e.size, $e.sha256, $e.path)
}
if ($installerEntry) {
    Write-Host ("    安装器  ：{0}{1}（sha256 {2}）" -f $installerEntry.name,
        $(if ($installerEntry.version) { " v" + $installerEntry.version } else { '' }),
        $installerEntry.sha256)
}

$uploads = @((Split-Path -Leaf $Zip), 'KfuPet-manifest.json')
if ($installerEntry) { $uploads += $installerEntry.name }
Write-Host ("==> 上传提醒：{0} 需一起传到 GitHub 与 Gitee 的同一个 Release" -f ($uploads -join '、')) -ForegroundColor Yellow