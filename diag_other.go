//go:build !windows

package main

import "runtime"

func osVersion() string { return runtime.GOOS }
