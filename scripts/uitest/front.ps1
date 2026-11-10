# Before a window check on a Windows runner: closes what earlier checks left open (Explorer folder windows from
# "Open folder", the "Select an app" dialog from opening a file nobody handles), which would otherwise take the
# focus in the middle of the next check, and brings the program's window to the front.
# Usage: front.ps1 <process name without .exe>
param([string]$Name)
Get-Process -Name OpenWith -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
try { (New-Object -ComObject Shell.Application).Windows() | ForEach-Object { try { $_.Quit() } catch {} } } catch {}
Start-Sleep -Milliseconds 700
$p = Get-Process -Name $Name -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
if ($p) {
  Add-Type -Namespace W -Name U -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetForegroundWindow(System.IntPtr h); [DllImport("user32.dll")] public static extern bool ShowWindow(System.IntPtr h, int c);'
  [W.U]::ShowWindow($p.MainWindowHandle, 9) | Out-Null
  (New-Object -ComObject WScript.Shell).AppActivate($p.Id) | Out-Null
  [W.U]::SetForegroundWindow($p.MainWindowHandle) | Out-Null
  "front: $Name ($($p.Id))"
} else { "front: no window for $Name" }
