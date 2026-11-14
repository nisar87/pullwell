package main

import "strings"

// AppName is the only place the product name is written in Go code. Everything else is derived from it, so a
// rename changes this one line (and the docs). appname_test.go fails if the name appears anywhere else.
const AppName = "Pullwell"

// appVersion must equal the VERSION file (checked by appname_test.go). appBuild is the commit, set by the build
// (-ldflags "-X main.appBuild=<commit>"); "dev" for a local build.
var (
	appVersion = "0.13.1"
	appBuild   = "dev"
)

var (
	appID      = AppName + ".App"         // Windows app id (AppUserModelID) for the taskbar and toasts
	appScheme  = strings.ToLower(AppName) // link scheme for toast buttons: pullwell://
	appHeader  = "X-" + AppName           // required on every POST to the local API
	appDataDir = AppName                  // %LOCALAPPDATA%\<appDataDir>\data when installed
	appRepo    = "nisar87/" + appScheme   // GitHub repository, for update links
)
