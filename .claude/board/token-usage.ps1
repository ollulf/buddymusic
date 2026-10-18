# Sums today's Claude Code token usage for this project from the session logs, for the
# Tasks board's token meter. Prints one JSON object:
#   { date, updatedAt, total, manager, agents: { <agentId>: usage } }
# where each usage is { input, cacheWrite, cacheRead, output, total }.
# Top-level session logs count as the manager; subagents/agent-<id>.jsonl as that agent.
param(
    [string]$ProjectDir = (Join-Path $env:USERPROFILE ".claude\projects\F--Fork-buddymusic"),
    [string]$OutFile
)

$today = (Get-Date).Date
$keys = @("input", "cacheWrite", "cacheRead", "output")
$idRx = [regex]'"message":\{"model":"[^"]*","id":"([^"]+)"'
$tsRx = [regex]'"timestamp":"([^"]+)"'
$numRx = @(
    [regex]'"usage":\{"input_tokens":(\d+)',
    [regex]'"cache_creation_input_tokens":(\d+)',
    [regex]'"cache_read_input_tokens":(\d+)',
    [regex]'"output_tokens":(\d+)'
)

function New-Usage { [ordered]@{ input = 0L; cacheWrite = 0L; cacheRead = 0L; output = 0L; total = 0L } }

# One message is logged once per content block while it streams, each time with the usage so
# far, so every message keeps the largest value seen per field.
function Read-Messages($path, $messages) {
    $reader = [System.IO.StreamReader]::new($path)
    try {
        while ($null -ne ($line = $reader.ReadLine())) {
            if ($line.IndexOf('"usage":{') -lt 0) { continue }
            $id = $idRx.Match($line)
            if (-not $id.Success) { continue }
            $ts = $tsRx.Match($line)
            if ($ts.Success -and ([datetime]$ts.Groups[1].Value).ToLocalTime().Date -ne $today) { continue }
            $key = $id.Groups[1].Value
            if (-not $messages.ContainsKey($key)) { $messages[$key] = [long[]]::new(4) }
            $u = $messages[$key]
            for ($i = 0; $i -lt 4; $i++) {
                $m = $numRx[$i].Match($line)
                if ($m.Success -and [long]$m.Groups[1].Value -gt $u[$i]) { $u[$i] = [long]$m.Groups[1].Value }
            }
        }
    } finally { $reader.Close() }
}

function Sum-Messages($messages) {
    $usage = New-Usage
    foreach ($u in $messages.Values) { for ($i = 0; $i -lt 4; $i++) { $usage[$keys[$i]] += $u[$i] } }
    $usage.total = $usage.input + $usage.cacheWrite + $usage.cacheRead + $usage.output
    $usage
}

$managerMessages = @{}
Get-ChildItem -Path $ProjectDir -Filter *.jsonl -File |
    Where-Object { $_.LastWriteTime -ge $today } |
    ForEach-Object { Read-Messages $_.FullName $managerMessages }
$manager = Sum-Messages $managerMessages

$agents = [ordered]@{}
Get-ChildItem -Path $ProjectDir -Recurse -Filter "agent-*.jsonl" -File |
    Where-Object { $_.LastWriteTime -ge $today -and $_.Directory.Name -eq "subagents" } |
    ForEach-Object {
        $messages = @{}
        Read-Messages $_.FullName $messages
        $agents[$_.BaseName.Substring(6)] = Sum-Messages $messages
    }

$total = New-Usage
foreach ($u in @($manager) + @($agents.Values)) {
    foreach ($k in $keys + "total") { $total[$k] += $u[$k] }
}

# Plan limits only reach the status line (.claude/board/statusline.ps1), which keeps the
# latest session state here. Times become epoch ms like everything else on the board.
$limits = [ordered]@{}
$statusFile = Join-Path $env:LOCALAPPDATA "cuttingboard-board\statusline.json"
if (Test-Path $statusFile) {
    try {
        $status = Get-Content -Raw $statusFile | ConvertFrom-Json
        if ($status.rate_limits) {
            foreach ($p in $status.rate_limits.PSObject.Properties) {
                if ($null -eq $p.Value.used_percentage) { continue }
                $limits[$p.Name] = [ordered]@{
                    used     = [double]$p.Value.used_percentage
                    resetsAt = if ($p.Value.resets_at) { [long]$p.Value.resets_at * 1000 } else { $null }
                }
            }
        }
        $limitsAt = [DateTimeOffset]::new((Get-Item $statusFile).LastWriteTimeUtc).ToUnixTimeMilliseconds()
    } catch {}
}

$result = [ordered]@{
    date      = $today.ToString("yyyy-MM-dd")
    updatedAt = [DateTimeOffset]::Now.ToUnixTimeMilliseconds()
    total     = $total
    manager   = $manager
    agents    = $agents
    limits    = $limits
    limitsAt  = $limitsAt
}
$json = $result | ConvertTo-Json -Depth 5 -Compress
if ($OutFile) { [System.IO.File]::WriteAllText($OutFile, $json) } else { $json }
