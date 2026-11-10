package main

import (
	"fmt"
	"os"
)

// The program itself does not exist yet: this file only makes the repository build while the parts are added in
// small steps (see docs/window-tray-notifications-design.md, section 9).
func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(AppName, appVersion)
		return
	}
	fmt.Printf("%s %s is still being built and cannot download anything yet.\n", AppName, appVersion)
}
