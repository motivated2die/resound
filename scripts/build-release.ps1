# Builds a release Resound.exe with matching version numbers everywhere.
# The release workflow (.github/workflows/release.yml) runs this same script,
# so a local build and a GitHub build of the same commit are made the same way.
#
#   .\scripts\build-release.ps1 -Version 0.7.0
#   .\scripts\build-release.ps1 -Version 0.7.0-rc.1 -Out dist
#
# Version is semver without the "v": major.minor.patch, optionally followed by
# a pre-release suffix (-rc.1, -beta.2). The exe's Windows file version uses
# only the numbers. The output folder defaults to a folder outside the repo,
# so the exe is not "portable" and installs itself when started.
#
# Output: <Out>\Resound.exe and <Out>\SHA256SUMS.txt. See docs\RELEASING.md.
param(
    [Parameter(Mandatory = $true)][ValidatePattern('^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$')][string]$Version,
    [string]$Out = (Join-Path $env:USERPROFILE "Desktop\Resound-$Version")
)
$ErrorActionPreference = "Stop"
$Out = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Out)
$repo = Split-Path -Parent $PSScriptRoot
Push-Location $repo
try {
    if (-not (Test-Path "internal\server\web\index.html")) {
        throw "internal\server\web\index.html is missing: the phone remote would not be embedded."
    }

    $commit = git rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE) { $commit = "unknown" }
    if (git status --porcelain 2>$null) {
        Write-Warning "The working tree has uncommitted changes: this build is not exactly commit $commit."
    }

    Write-Host "== vet and test"
    go vet ./...
    if ($LASTEXITCODE) { throw "go vet failed" }
    go test ./...
    if ($LASTEXITCODE) { throw "go test failed" }

    $winVersion = ($Version -replace '-.*$') + ".0"
    if (-not (Test-Path "assets\icon.ico")) { throw "assets\icon.ico is missing (go run ./scripts/icons)." }
    Write-Host "== resources (icon, manifest, version $winVersion)"
    go-winres make --in winres\winres.json --out cmd\resound\rsrc --arch amd64 `
        --product-version $winVersion --file-version $winVersion
    if ($LASTEXITCODE) { throw "go-winres failed (installed? go install github.com/tc-hib/go-winres@v0.3.3)" }

    New-Item -ItemType Directory -Force $Out | Out-Null
    $exe = Join-Path $Out "Resound.exe"
    Write-Host "== build $exe (v$Version, commit $commit)"
    go build -trimpath `
        -ldflags "-H=windowsgui -s -w -X github.com/motivated2die/resound/internal/app.Version=$Version" `
        -o $exe ./cmd/resound
    if ($LASTEXITCODE) { throw "go build failed" }

    # sha256sum format, so `sha256sum -c SHA256SUMS.txt` works too.
    $hash = (Get-FileHash $exe -Algorithm SHA256).Hash.ToLowerInvariant()
    Set-Content -Path (Join-Path $Out "SHA256SUMS.txt") -Value "$hash  Resound.exe" -Encoding ascii
    Write-Host "== done: $exe"
    Write-Host "   SHA-256 $hash"
} finally {
    Pop-Location
}
