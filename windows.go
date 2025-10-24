//go:build windows

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/getlantern/systray"
)

//go:embed views/static/images/bart.ico
var iconData []byte

// Windows API constants and functions
var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procFreeConsole              = kernel32.NewProc("FreeConsole")
	procGetConsoleWindow         = kernel32.NewProc("GetConsoleWindow")
	procAllocConsole             = kernel32.NewProc("AllocConsole")
	procTerminateProcess         = kernel32.NewProc("TerminateProcess")
	procGetCurrentProcess        = kernel32.NewProc("GetCurrentProcess")
	user32                       = syscall.NewLazyDLL("user32.dll")
	procShowWindow               = user32.NewProc("ShowWindow")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

const (
	SW_HIDE = 0
	SW_SHOW = 5
)

// handleWindowsConsole hides or shows the console window based on debug mode
func handleWindowsConsole(debugMode bool) {
	if debugMode {
		// In debug mode, allocate a console if we don't have one
		allocConsole()
		showConsoleWindow()
	}
	// In normal mode, we don't need to do anything since we're built as a GUI app
}

// allocConsole allocates a new console for the process
func allocConsole() {
	procAllocConsole.Call()
}

// showConsoleWindow shows the console window
func showConsoleWindow() {
	console := getConsoleWindow()
	if console != 0 {
		showWindow(console, SW_SHOW)
	}
}

// getConsoleWindow gets the console window handle
func getConsoleWindow() uintptr {
	ret, _, _ := procGetConsoleWindow.Call()
	return ret
}

// showWindow shows or hides a window
func showWindow(hwnd uintptr, cmdShow int) {
	procShowWindow.Call(hwnd, uintptr(cmdShow))
}

// forceExit forcefully terminates the application
func forceExit() {
	// Immediate forceful exit - no graceful shutdown needed
	os.Exit(0)
}

// trayApp represents the system tray application
type trayApp struct {
	config *Config
}

// NewTrayApp creates a new tray application
func NewTrayApp(config *Config) *trayApp {
	return &trayApp{
		config: config,
	}
}

// Run starts the system tray application
func (t *trayApp) Run() {
	// Only run on Windows
	if runtime.GOOS != "windows" {
		return
	}

	systray.Run(t.onReady, t.onExit)
}

// onReady is called when the system tray is ready
func (t *trayApp) onReady() {
	// Set the icon
	systray.SetIcon(iconData)

	// Set tooltip
	systray.SetTitle("MariaDB Backup Tool")
	systray.SetTooltip("MariaDB Backup Tool - Click to open web interface")

	// Add menu items
	mOpen := systray.AddMenuItem("Open Web Interface", "Open the web interface in your browser")
	mQuit := systray.AddMenuItem("Quit", "Exit the application")

	// Handle menu clicks
	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				t.openWebInterface()
			case <-mQuit.ClickedCh:
				// Force exit the application immediately
				forceExit()
				return
			}
		}
	}()
}

// onExit is called when the system tray is exiting
func (t *trayApp) onExit() {
	// Clean up resources if needed
}

// openWebInterface opens the web interface in the default browser
func (t *trayApp) openWebInterface() {
	url := fmt.Sprintf("http://localhost:%d", t.config.Web.Port)

	// Try different methods to open the browser
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		LogError("Unsupported operating system: %s", runtime.GOOS)
		return
	}

	if err := cmd.Start(); err != nil {
		LogError("Failed to open browser: %v", err)
	}
}

// StartTrayIfWindows starts the system tray if running on Windows
func StartTrayIfWindows(config *Config) {
	if runtime.GOOS == "windows" {
		trayApp := NewTrayApp(config)
		go trayApp.Run()
	}
}
