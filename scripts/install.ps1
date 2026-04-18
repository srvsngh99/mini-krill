# Mini Krill installer for Windows
# Usage: irm https://raw.githubusercontent.com/srvsngh99/mini-krill/master/scripts/install.ps1 | iex

$ErrorActionPreference = "Stop"

$Repo       = "srvsngh99/mini-krill"
$Binary     = "minikrill.exe"
$InstallDir = "$env:LOCALAPPDATA\MiniKrill\bin"

# Detect architecture
function Get-Arch {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"   { return "amd64" }
        "Arm64" { return "arm64" }
        default { throw "Unsupported architecture: $arch" }
    }
}

# Get latest release tag from GitHub
function Get-LatestVersion {
    $url = "https://api.github.com/repos/$Repo/releases/latest"
    $release = Invoke-RestMethod -Uri $url -UseBasicParsing
    return $release.tag_name
}

# Main
Write-Host ""
Write-Host "  M I N I   K R I L L  -  Windows Installer" -ForegroundColor Cyan
Write-Host ""

$arch    = Get-Arch
$version = Get-LatestVersion

Write-Host "[info] Architecture: $arch"
Write-Host "[info] Latest version: $version"

$versionClean = $version.TrimStart("v")
$archiveName  = "minikrill_${versionClean}_windows_${arch}.zip"
$downloadUrl  = "https://github.com/$Repo/releases/download/$version/$archiveName"

# Create install directory
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# Download
$tmpZip = Join-Path $env:TEMP $archiveName
Write-Host "[info] Downloading $downloadUrl ..."
Invoke-WebRequest -Uri $downloadUrl -OutFile $tmpZip -UseBasicParsing

# Extract
Write-Host "[info] Extracting to $InstallDir ..."
Expand-Archive -Path $tmpZip -DestinationPath $InstallDir -Force
Remove-Item $tmpZip -Force

# Add to user PATH if not already present
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    Write-Host "[info] Added $InstallDir to user PATH."
    Write-Host "       Restart your terminal for PATH changes to take effect."
}

Write-Host ""
Write-Host "[info] Mini Krill $version installed to $InstallDir\$Binary" -ForegroundColor Green
Write-Host ""
Write-Host "  Run 'minikrill init' to get started."
Write-Host ""
