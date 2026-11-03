$ErrorActionPreference = 'Stop'
$scriptPath = Join-Path $PSScriptRoot 'restart-local.ps1'
foreach ($ps1File in @(Get-ChildItem -Path $PSScriptRoot -Filter '*.ps1')) {
    $bytes = [System.IO.File]::ReadAllBytes($ps1File.FullName)
    if ($bytes.Length -lt 3 -or $bytes[0] -ne 0xEF -or $bytes[1] -ne 0xBB -or $bytes[2] -ne 0xBF) {
        throw ("{0} must keep UTF-8 BOM for Windows PowerShell 5.1." -f $ps1File.Name)
    }
    $tokens = $null
    $parseErrors = $null
    $null = [System.Management.Automation.Language.Parser]::ParseFile($ps1File.FullName, [ref]$tokens, [ref]$parseErrors)
    if ($parseErrors.Count) { throw $parseErrors[0] }
}
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($scriptPath, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw $parseErrors[0] }
$rawText = [System.IO.File]::ReadAllText($scriptPath, [System.Text.Encoding]::UTF8)
if ($rawText.Contains('[DllImport("kernel32.dll")]')) {
    throw 'restart-local.ps1 $disableQuickEdit must escape inner quotes as \"kernel32.dll\" for Start-Process -Command.'
}
$function = $ast.Find({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Get-PortOccupancy' }, $true)
. ([scriptblock]::Create($function.Extent.Text))

# A missing listener in process discovery must not imply bind permission.
function Get-ListeningPids { return @() }
if ((Get-PortOccupancy 0).Status -ne 'free') { throw 'An OS-assigned port should be bindable.' }
$listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
$listener.Server.ExclusiveAddressUse = $true
try {
    $listener.Start()
    $port = $listener.LocalEndpoint.Port
    $result = Get-PortOccupancy $port
    if ($result.Status -ne 'unavailable' -or $result.Details.Count -eq 0) {
        throw 'A failed bind was incorrectly reported as free.'
    }
} finally {
    $listener.Stop()
}
if ((Get-PortOccupancy $port).Status -ne 'free') { throw 'Probe socket was not released.' }

function Get-ListeningPids { return @(12345) }
function Get-ProcessSummary { return 'test listener' }
function Test-ProcessFromRepo { return $false }
if ((Get-PortOccupancy $port).Status -ne 'foreign') { throw 'Foreign listener classification changed.' }
function Test-ProcessFromRepo { return $true }
if ((Get-PortOccupancy $port).Status -ne 'ours') { throw 'Workspace listener classification changed.' }
Write-Output 'PASS: bindable, unavailable, released, foreign and workspace ports.'
