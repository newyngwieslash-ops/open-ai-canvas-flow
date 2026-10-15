$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot 'restart-local.ps1'), [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw $parseErrors[0] }
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
