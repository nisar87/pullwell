package main

// The engine's notifications go through the grouper (notify_group.go): finished downloads and problems are
// collected for a few seconds and shown as one notification each.

import (
	"strings"

	"pullwell/dm"
	"sync"
	"time"
)

var (
	grouper     = newNoticeGrouper()
	grouperOnce sync.Once
)

// noticeKind recognises the engine's notification by its title (the titles are written in dm/dl_manager.go).
func noticeKind(title, text string) string {
	switch {
	case title == "Download finished":
		return "finished"
	case title == "Download needs attention":
		return "attention"
	case title == "Checksum does not match":
		return "checksum"
	case title == "Downloads finished" && strings.Contains(strings.ToLower(text), "shut"):
		return "shutdown"
	case title == "Downloads finished":
		return "allDone"
	}
	return "attention"
}

// notify is the engine's Host.Notify.
func notify(title, text string) {
	grouperOnce.Do(func() {
		go func() {
			for range time.Tick(time.Second) {
				for _, t := range grouper.Due(time.Now()) {
					showToast(t)
				}
			}
		}()
	})
	n := Notice{Kind: noticeKind(title, text), Title: title, Text: text}
	if n.Kind == "finished" {
		n.ItemID = dm.FinishedNamed(strings.TrimSpace(text)) // for the Open and Open folder buttons
	}
	if n.Kind == "finished" && showCompleteOn() {
		// "Show the finished download": the window comes up with its progress window, which offers Open and Open folder
		pendingMu.Lock()
		pendingViewV = "complete"
		pendingMu.Unlock()
		go showMainWindow()
	}
	for _, t := range grouper.Add(n, time.Now()) {
		showToast(t)
	}
}

// showToast shows one notification as a tray balloon (real toasts with buttons come with the installer).
func showToast(t toastSpec) {
	if toastsUsable() && showToastXML(toastXML(t)) == nil {
		return
	}
	showBalloon(t.Title, strings.Join(t.Lines, "\n"))
	setBalloonView(toastView(t.Tag))
}

// toastView is the part of the list a click on the notification shows: the finished downloads, or the ones that
// need attention.
func toastView(tag string) string {
	switch tag {
	case "finished", "done", "allDone", "shutdown":
		return "finished"
	case "attention", "checksum", "problem":
		return "errors"
	}
	return ""
}
