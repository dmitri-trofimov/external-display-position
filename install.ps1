[CmdletBinding()]
param(
    [Alias('y')]
    [switch]$Yes,
    [switch]$InstallGo,
    [switch]$EnableStartup,
    [switch]$DisableStartup,
    [switch]$SkipShortcut
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

function Find-GoExe {
    $cmd = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }

    $candidates = @(
        (Join-Path $env:ProgramFiles 'Go\bin\go.exe'),
        (Join-Path $env:LOCALAPPDATA 'Programs\Go\bin\go.exe')
    )
    foreach ($c in $candidates) {
        if ($c -and (Test-Path $c)) { return $c }
    }
    return $null
}

function Refresh-ProcessPath {
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $env:Path = "$machinePath;$userPath"
}

function Ensure-GoInstalled {
    $goExe = Find-GoExe
    if ($goExe) {
        $ver = & $goExe version
        Write-Host "[OK] Found $ver" -ForegroundColor Green
        return $goExe
    }

    Write-Host "[!] Go is not installed." -ForegroundColor Yellow
    $winget = Get-Command winget.exe -ErrorAction SilentlyContinue

    $shouldInstall = $Yes -or $InstallGo
    if (-not $shouldInstall) {
        if ($winget) {
            $answer = Read-Host "Install Go automatically using winget (GoLang.Go)? [Y/n]"
            $shouldInstall = ($answer -eq '' -or $answer -match '^(y|yes)$')
        }
    }

    if (-not $shouldInstall) {
        Write-Host ""
        Write-Host "Please install Go from https://go.dev/dl/ (or run: winget install GoLang.Go)" -ForegroundColor Cyan
        Write-Host "Then re-run .\install.ps1 (or run .\install.ps1 -y to auto-install Go)." -ForegroundColor Cyan
        exit 1
    }

    if (-not $winget) {
        Write-Error "winget.exe was not found. Please install Go manually from https://go.dev/dl/ and re-run .\install.ps1."
    }

    Write-Host "[*] Installing Go via winget..." -ForegroundColor Cyan
    & $winget.Source install --id GoLang.Go --exact --accept-package-agreements --accept-source-agreements --silent
    if ($LASTEXITCODE -ne 0) {
        Write-Error "winget install GoLang.Go failed with exit code $LASTEXITCODE. Install Go manually from https://go.dev/dl/."
    }

    Refresh-ProcessPath
    $goExe = Find-GoExe
    if (-not $goExe) {
        Write-Error "Go installation completed, but go.exe was not found in PATH yet. Open a new terminal and re-run .\install.ps1."
    }

    $ver = & $goExe version
    Write-Host "[OK] Installed $ver" -ForegroundColor Green
    return $goExe
}

function Build-App([string]$goExe) {
    $running = Get-Process -Name 'edpw' -ErrorAction SilentlyContinue
    if ($running) {
        Write-Host "[*] Stopping running edpw.exe before rebuilding..." -ForegroundColor Cyan
        $running | Stop-Process -Force
        Start-Sleep -Milliseconds 200
    }

    Write-Host "[*] Building edp.exe and edpw.exe..." -ForegroundColor Cyan
    & $goExe build -ldflags="-s -w" -o (Join-Path $PSScriptRoot 'edp.exe') ./cmd/edp
    if ($LASTEXITCODE -ne 0) { Write-Error "Failed to build edp.exe" }

    & $goExe build -ldflags="-s -w -H=windowsgui" -o (Join-Path $PSScriptRoot 'edpw.exe') ./cmd/edpw
    if ($LASTEXITCODE -ne 0) { Write-Error "Failed to build edpw.exe" }

    Write-Host "[OK] Built edp.exe and edpw.exe" -ForegroundColor Green
}

function Configure-Startup {
    if ($EnableStartup -and $DisableStartup) {
        Write-Error "Cannot pass both -EnableStartup and -DisableStartup."
    }

    $doStartup = $false
    if ($EnableStartup) {
        $doStartup = $true
    } elseif ($DisableStartup) {
        $doStartup = $false
    } elseif ($Yes) {
        $doStartup = $true
    } else {
        $answer = Read-Host "Run silent display watcher on Windows startup and start it now? [Y/n]"
        $doStartup = ($answer -eq '' -or $answer -match '^(y|yes)$')
    }

    $edpExe = Join-Path $PSScriptRoot 'edp.exe'
    $edpwExe = Join-Path $PSScriptRoot 'edpw.exe'

    if ($doStartup) {
        & $edpExe autostart enable
        if ($LASTEXITCODE -ne 0) { Write-Error "Failed to enable autostart" }

        Start-Process -FilePath $edpwExe -WorkingDirectory $PSScriptRoot
        Write-Host "[OK] Windows startup enabled and edpw.exe started." -ForegroundColor Green
    } else {
        Write-Host "[--] Skipped Windows startup (you can enable it later with: .\edp.exe autostart enable)" -ForegroundColor DarkGray
    }
}

function Create-DesktopShortcut {
    if ($SkipShortcut) { return }

    $desktop = [Environment]::GetFolderPath('Desktop')
    if (-not $desktop -or -not (Test-Path $desktop)) {
        Write-Warning "Could not locate Desktop folder; skipping shortcut creation."
        return
    }

    $shortcutPath = Join-Path $desktop 'Save Display Layout.lnk'
    $edpExe = Join-Path $PSScriptRoot 'edp.exe'
    $iconPath = Join-Path $PSScriptRoot 'assets\app.ico'

    $shell = New-Object -ComObject WScript.Shell
    $shortcut = $shell.CreateShortcut($shortcutPath)
    $shortcut.TargetPath = $edpExe
    $shortcut.Arguments = 'save --pause'
    $shortcut.WorkingDirectory = $PSScriptRoot
    $shortcut.Description = 'Save current external display position for the active laptop port'
    if (Test-Path $iconPath) {
        $shortcut.IconLocation = "$iconPath, 0"
    }
    $shortcut.Save()

    Write-Host "[OK] Created Desktop shortcut: $shortcutPath" -ForegroundColor Green
}

$goExe = Ensure-GoInstalled
Build-App $goExe
Configure-Startup
Create-DesktopShortcut

Write-Host ""
Write-Host "Installation complete!" -ForegroundColor Green
Write-Host "Next: arrange your displays in Windows Settings, then double-click 'Save Display Layout' on your Desktop for each port." -ForegroundColor Cyan
