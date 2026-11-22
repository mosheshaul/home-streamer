//go:build windows

package main

// On Windows, HomeStreamer runs without a window: it shows an icon next to
// the clock with a small menu, writes its messages to HomeStreamer.log, and
// starts by itself when Windows starts (Settings in the menu can turn that off).

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed tray.ico
var trayIcon []byte

var logPath string

const (
	runKey         = `Software\Microsoft\Windows\CurrentVersion\Run`
	runName        = "HomeStreamer"
	appKey         = `Software\HomeStreamer`
	createNoWindow = 0x08000000
)

func setupLogging(dir string) {
	logPath = filepath.Join(dir, "HomeStreamer.log")
	// Keep the log small: start over (keeping the previous one) when it's big.
	if st, err := os.Stat(logPath); err == nil && st.Size() > 5<<20 {
		os.Rename(logPath, logPath+".old")
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.SetOutput(io.Discard)
		return
	}
	log.SetOutput(f)
	log.Printf("------------------------------------------------------------")
}

func message(text string, icon uint32) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("Home Streamer")
	windows.MessageBox(0, t, c, windows.MB_OK|icon|windows.MB_SETFOREGROUND)
}

func fatal(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Print(msg)
	message("Home Streamer can't start:\n\n"+msg, windows.MB_ICONERROR)
	os.Exit(1)
}

// singleInstance reports whether this is the only copy running.
func singleInstance() bool {
	name, _ := windows.UTF16PtrFromString(`Local\HomeStreamerServer`)
	_, err := windows.CreateMutex(nil, false, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		// Started twice while Windows starts (say, by an old Startup-folder
		// shortcut as well): stay quiet.
		if uptimeMs() < 10*60*1000 {
			return false
		}
		message("Home Streamer is already running.\n\nLook for its icon next to the clock (it may be under the ^ arrow).", windows.MB_ICONINFORMATION)
		return false
	}
	return true
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func exePath() string {
	exe, _ := os.Executable()
	return exe
}

func autostartOn() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runName)
	return err == nil && v != ""
}

func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runName); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	return k.SetStringValue(runName, `"`+exePath()+`"`)
}

// firstRun reports whether HomeStreamer has never run before on this PC, and remembers that it has.
func firstRun() bool {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, appKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	if _, _, err := k.GetIntegerValue("Started"); err == nil {
		return false
	}
	k.SetDWordValue("Started", 1)
	return true
}

func openURL(target string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	cmd.Start()
}

func runApp(name string, port int, cfgPath string, serve func()) {
	go serve()
	first := firstRun()
	if first {
		// Start with Windows unless the user turns it off in the menu.
		if err := setAutostart(true); err != nil {
			log.Printf("Couldn't set HomeStreamer to start with Windows: %v", err)
		}
	} else if autostartOn() {
		setAutostart(true) // keep the path right if the program was moved
	}
	playerURL := fmt.Sprintf("http://localhost:%d/", port)

	systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("Home Streamer is sharing your videos (" + name + ")")
		title := systray.AddMenuItem("Home Streamer "+version, "")
		title.Disable()
		systray.AddSeparator()
		mOpen := systray.AddMenuItem("Open the player", "Watch in the browser on this PC")
		mConfig := systray.AddMenuItem("Choose video folders…", "Edit config.json, then restart Home Streamer")
		mAddr := systray.AddMenuItem("Show addresses", "The addresses to type in the apps")
		mLog := systray.AddMenuItem("Show log", "Messages from Home Streamer, including its addresses")
		systray.AddSeparator()
		mAuto := systray.AddMenuItemCheckbox("Start with Windows", "Run Home Streamer automatically when you sign in to Windows", autostartOn())
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Stop sharing videos")
		if first {
			go message("Home Streamer is running.\n\n"+
				"It has no window: its icon is next to the clock (Windows may put it under the ^ arrow; drag it next to the clock to keep it visible). "+
				"Right-click the icon for its menu.\n\n"+
				"It will start by itself every time Windows starts. You can turn that off in the menu.", windows.MB_ICONINFORMATION)
		}
		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					openURL(playerURL)
				case <-mConfig.ClickedCh:
					cmd := exec.Command("notepad.exe", cfgPath)
					cmd.Start()
				case <-mAddr.ClickedCh:
					go message(addresses(port), windows.MB_ICONINFORMATION)
				case <-mLog.ClickedCh:
					cmd := exec.Command("notepad.exe", logPath)
					cmd.Start()
				case <-mAuto.ClickedCh:
					on := !mAuto.Checked()
					if err := setAutostart(on); err != nil {
						go message("Couldn't change this: "+err.Error(), windows.MB_ICONERROR)
						continue
					}
					if on {
						mAuto.Check()
					} else {
						mAuto.Uncheck()
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()
	}, func() {
		log.Printf("Quit from the tray menu.")
		os.Exit(0)
	})
}

var getTickCount64 = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64")

// uptimeMs is how long Windows has been running, in milliseconds.
func uptimeMs() uint64 {
	r, _, _ := getTickCount64.Call()
	return uint64(r)
}

func addresses(port int) string {
	out := ""
	for _, ip := range lanIPs() {
		if isTailscale(ip) {
			out += fmt.Sprintf("Away from home (Tailscale): %s:%d\n", ip, port)
		} else {
			out += fmt.Sprintf("At home: %s:%d\n", ip, port)
		}
	}
	if out == "" {
		return "This PC isn't connected to a network right now."
	}
	return out + "\nIn a browser, add http:// in front, like http://" + lanIPs()[0] + fmt.Sprintf(":%d/", port)
}
