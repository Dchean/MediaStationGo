# Windows 自动安装 ffmpeg/ffprobe 脚本
# 在项目首次运行或用户手动点击工具页按钮时执行。

param(
    [string]$InstallDir = (Join-Path $PSScriptRoot "..\tools\ffmpeg"),
    [string]$ConfigFile = (Join-Path $PSScriptRoot "..\config.yaml")
)

$ErrorActionPreference = "Stop"
$previousProgressPreference = $ProgressPreference
$ProgressPreference = "SilentlyContinue"
$tempRoot = $null

function Test-ToolInstalled {
    param([string]$BaseDir)

    $ffmpeg = Join-Path $BaseDir "bin\ffmpeg.exe"
    $ffprobe = Join-Path $BaseDir "bin\ffprobe.exe"
    return (Test-Path $ffmpeg) -and (Test-Path $ffprobe)
}

function Update-ConfigFile {
    param(
        [string]$Path,
        [string]$FFmpegPath,
        [string]$FFprobePath
    )

    if (!(Test-Path $Path)) {
        return
    }

    $content = Get-Content -Path $Path
    $content = $content | ForEach-Object {
        if ($_ -match '^\s*ffmpeg_path:') {
            "  ffmpeg_path: $FFmpegPath"
        } elseif ($_ -match '^\s*ffprobe_path:') {
            "  ffprobe_path: $FFprobePath"
        } else {
            $_
        }
    }

    Set-Content -Path $Path -Value $content -Encoding UTF8
}

function Cleanup-Temp {
    if ($tempRoot -and (Test-Path $tempRoot)) {
        Remove-Item -LiteralPath $tempRoot -Recurse -Force
    }
}

Write-Host "=== MediaStationGo ffmpeg 自动安装脚本 ===" -ForegroundColor Cyan
Write-Host ""

$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
$ConfigFile = [System.IO.Path]::GetFullPath($ConfigFile)
$ffmpegPath = Join-Path $InstallDir "bin\ffmpeg.exe"
$ffprobePath = Join-Path $InstallDir "bin\ffprobe.exe"
$arch = if ([Environment]::Is64BitOperatingSystem) { "win64" } else { "win32" }

Write-Host "检测到系统架构: $arch" -ForegroundColor Yellow
Write-Host "安装目录: $InstallDir" -ForegroundColor Yellow

trap {
    Write-Host "✗ 自动安装失败: $($_.Exception.Message)" -ForegroundColor Red
    if ($_.InvocationInfo) {
        Write-Host ("行 {0}: {1}" -f $_.InvocationInfo.ScriptLineNumber, $_.InvocationInfo.Line.Trim()) -ForegroundColor DarkRed
    }
    Write-Host ""
    Write-Host "请手动下载安装并放到以下目录:" -ForegroundColor Yellow
    Write-Host $InstallDir -ForegroundColor Yellow
    $ProgressPreference = $previousProgressPreference
    Cleanup-Temp
    exit 1
}

if (Test-ToolInstalled -BaseDir $InstallDir) {
    Write-Host "✓ ffmpeg 已安装" -ForegroundColor Green
    & $ffprobePath -version | Select-Object -First 1
    exit 0
}

$downloadUrls = @(
    "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip",
    "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-$arch-gpl.zip"
)

New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

$tempRoot = Join-Path $env:TEMP ("mediastationgo-ffmpeg-" + [guid]::NewGuid().ToString("N"))
$zipFile = Join-Path $tempRoot "ffmpeg.zip"
$extractDir = Join-Path $tempRoot "extract"
New-Item -ItemType Directory -Path $extractDir -Force | Out-Null

$downloaded = $false
foreach ($downloadUrl in $downloadUrls) {
    try {
        Write-Host "下载 ffmpeg..." -ForegroundColor Yellow
        Write-Host "URL: $downloadUrl"
        Invoke-WebRequest -Uri $downloadUrl -OutFile $zipFile
        $downloaded = $true
        break
    } catch {
        Write-Host "下载失败，尝试备用地址: $downloadUrl" -ForegroundColor DarkYellow
    }
}

if (-not $downloaded) {
    throw "所有下载地址均不可用"
}

Write-Host "解压文件..." -ForegroundColor Yellow
Expand-Archive -Path $zipFile -DestinationPath $extractDir -Force

$ffmpegBinaryPath = "" + (Get-ChildItem -Path $extractDir -Recurse -Filter "ffmpeg.exe" -File |
    Select-Object -First 1 -ExpandProperty FullName)
$ffprobeBinaryPath = "" + (Get-ChildItem -Path $extractDir -Recurse -Filter "ffprobe.exe" -File |
    Select-Object -First 1 -ExpandProperty FullName)

if ([string]::IsNullOrWhiteSpace($ffmpegBinaryPath) -or [string]::IsNullOrWhiteSpace($ffprobeBinaryPath)) {
    Write-Host "未找到预期二进制，解压结果样本:" -ForegroundColor DarkYellow
    Get-ChildItem -Path $extractDir -Recurse -File | Select-Object -First 20 FullName | ForEach-Object {
        Write-Host $_.FullName -ForegroundColor DarkYellow
    }
    throw "解压后未找到包含 bin\ffmpeg.exe 的目录"
}

$packageRoot = Split-Path -Parent (Split-Path -Parent $ffmpegBinaryPath)
Copy-Item -Path (Join-Path $packageRoot "*") -Destination $InstallDir -Recurse -Force

if (-not (Test-ToolInstalled -BaseDir $InstallDir)) {
    throw "ffmpeg 文件未正确安装到 $InstallDir\bin"
}

$env:PATH = "$InstallDir\bin;$env:PATH"
Update-ConfigFile -Path $ConfigFile -FFmpegPath $ffmpegPath -FFprobePath $ffprobePath

Write-Host "✓ 安装完成" -ForegroundColor Green
Write-Host "ffmpeg:  $ffmpegPath" -ForegroundColor Green
Write-Host "ffprobe: $ffprobePath" -ForegroundColor Green
& $ffprobePath -version | Select-Object -First 1
$ProgressPreference = $previousProgressPreference
Cleanup-Temp

Write-Host ""
Write-Host "提示: 如需永久加入 PATH，请以管理员执行:" -ForegroundColor Cyan
Write-Host "[Environment]::SetEnvironmentVariable('PATH', `$env:PATH + ';$InstallDir\bin', 'Machine')" -ForegroundColor Gray
