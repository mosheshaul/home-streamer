//go:build !windows

package main

import (
	"log"
	"os/exec"
)

// On other systems the server runs in the terminal, as before.
func setupLogging(dir string)                                    {}
func singleInstance() bool                                       { return true }
func fatal(format string, args ...any)                           { log.Fatalf(format, args...) }
func hideWindow(cmd *exec.Cmd)                                   {}
func runApp(name string, port int, cfgPath string, serve func()) { serve() }
