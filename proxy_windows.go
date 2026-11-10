package main

import "syscall"

// readSystemProxy reads the current user's proxy settings (the same as Settings → Network → Proxy).
func readSystemProxy() systemProxy {
	const key = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	var sp systemProxy
	on, _ := regDWORD(syscall.HKEY_CURRENT_USER, key, "ProxyEnable")
	sp.Enabled = on == 1
	sp.Server, _ = regString(syscall.HKEY_CURRENT_USER, key, "ProxyServer")
	sp.Bypass, _ = regString(syscall.HKEY_CURRENT_USER, key, "ProxyOverride")
	sp.Script, _ = regString(syscall.HKEY_CURRENT_USER, key, "AutoConfigURL")
	return sp
}
