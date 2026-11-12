# Inspect or operate on only the isolated UI windows selected by class and title.
param(
  [Parameter(Mandatory=$true)][string]$ClassPrefix,
  [string]$TitleContains = '',
  [ValidateSet('inspect','minimize','close')][string]$Action = 'inspect'
)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -TypeDefinition @'
using System;
using System.Collections.Generic;
using System.Runtime.InteropServices;
using System.Text;
public static class NativeWindowCheck {
  public delegate bool EnumProc(IntPtr hwnd, IntPtr data);
  [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc proc, IntPtr data);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr hwnd, StringBuilder text, int count);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr hwnd, StringBuilder text, int count);
  [DllImport("user32.dll")] public static extern bool IsIconic(IntPtr hwnd);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr hwnd);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr hwnd, int command);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr hwnd, uint message, IntPtr wp, IntPtr lp);
  public sealed class Window {
    public long Handle;
    public string Title;
    public bool Iconic;
    public bool Visible;
  }
  public static List<Window> Find(string prefix, string contains) {
    var found = new List<Window>();
    EnumWindows((hwnd, data) => {
      var cls = new StringBuilder(256);
      var title = new StringBuilder(4096);
      GetClassName(hwnd, cls, cls.Capacity);
      GetWindowText(hwnd, title, title.Capacity);
      if (cls.ToString().StartsWith(prefix, StringComparison.Ordinal) && title.ToString().Contains(contains))
        found.Add(new Window { Handle=hwnd.ToInt64(), Title=title.ToString(), Iconic=IsIconic(hwnd), Visible=IsWindowVisible(hwnd) });
      return true;
    }, IntPtr.Zero);
    return found;
  }
}
'@
$windows = [NativeWindowCheck]::Find($ClassPrefix, $TitleContains)
foreach ($window in $windows) {
  if ($Action -eq 'minimize') { [NativeWindowCheck]::ShowWindow([IntPtr]$window.Handle, 6) | Out-Null }
  if ($Action -eq 'close') { [NativeWindowCheck]::PostMessage([IntPtr]$window.Handle, 0x10, [IntPtr]::Zero, [IntPtr]::Zero) | Out-Null }
}
ConvertTo-Json -InputObject @($windows) -Compress
