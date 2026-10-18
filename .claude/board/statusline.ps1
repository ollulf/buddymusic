# Claude Code status line for this project. Claude Code pipes the session state as JSON on
# stdin; this keeps the latest copy for the Tasks board's token meter (plan limits and reset
# times are only available here) and prints a short line for the terminal.
$raw = [Console]::In.ReadToEnd()
$dir = Join-Path $env:LOCALAPPDATA "cuttingboard-board"
try {
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    [System.IO.File]::WriteAllText((Join-Path $dir "statusline.json"), $raw)
} catch {}

try { $s = $raw | ConvertFrom-Json } catch { $s = $null }
$parts = @()
if ($s -and $s.model -and $s.model.display_name) { $parts += $s.model.display_name }
if ($s -and $s.rate_limits) {
    foreach ($p in $s.rate_limits.PSObject.Properties) {
        $v = $p.Value
        if ($v -and $null -ne $v.used_percentage) { $parts += ("{0} {1:0}%" -f $p.Name, $v.used_percentage) }
    }
}
if ($s -and $s.context_window -and $null -ne $s.context_window.used_percentage) {
    $parts += ("ctx {0:0}%" -f $s.context_window.used_percentage)
}
if (-not $parts) { $parts = @("buddymusic") }
$parts -join " | "
