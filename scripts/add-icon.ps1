<#
.SYNOPSIS
Downloads SVG icons into internal\ui\icons, where they are embedded into the exe.

.DESCRIPTION
Icons are fetched once at a pinned library version and committed; the app
never loads anything at runtime. The panel rasterizes them anti-aliased at
the exact pixel size for the monitor's DPI (see internal\ui\icon.go).

File names: Tabler outline icons keep their name ("refresh"), Tabler filled
icons get "-filled", Font Awesome icons get the set as prefix
("fa-solid-house"). Use the file name without ".svg" in code:
ui.Icon("refresh"), or add a constant to internal\ui\icon.go.

Browse: https://tabler.io/icons  ·  https://fontawesome.com/search?ic=free

.EXAMPLE
.\scripts\add-icon.ps1 refresh, player-play
.\scripts\add-icon.ps1 -Set tabler-filled star
.\scripts\add-icon.ps1 -Set fa-solid volume-high
#>
param(
    [Parameter(Mandatory, Position = 0)][string[]]$Name,
    [ValidateSet('tabler', 'tabler-filled', 'fa-solid', 'fa-regular', 'fa-brands')]
    [string]$Set = 'tabler'
)

$ErrorActionPreference = 'Stop'
$TablerVersion = '3.49.0'      # MIT
$FontAwesomeVersion = '6.7.2'  # Free icons: CC BY 4.0

$dir = Join-Path $PSScriptRoot '..\internal\ui\icons'
foreach ($n in $Name) {
    switch ($Set) {
        'tabler'        { $url = "https://unpkg.com/@tabler/icons@$TablerVersion/icons/outline/$n.svg"; $file = "$n.svg" }
        'tabler-filled' { $url = "https://unpkg.com/@tabler/icons@$TablerVersion/icons/filled/$n.svg"; $file = "$n-filled.svg" }
        default {
            $style = $Set.Substring(3) # solid, regular, brands
            $url = "https://unpkg.com/@fortawesome/fontawesome-free@$FontAwesomeVersion/svgs/$style/$n.svg"
            $file = "$Set-$n.svg"
        }
    }
    $out = Join-Path $dir $file
    Invoke-WebRequest -Uri $url -OutFile $out -UseBasicParsing
    Write-Host "added internal\ui\icons\$file  ->  ui.Icon(`"$($file -replace '\.svg$')`")"
}
Write-Host 'Rebuild to embed them. go test ./internal/ui checks that every icon renders.'
