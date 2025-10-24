//go:build !windows

package main

// handleWindowsConsole is a no-op on non-Windows platforms
func handleWindowsConsole(debugMode bool) {
	// Console handling is only needed on Windows
	// On other platforms, this function does nothing
}

// StartTrayIfWindows is a no-op on non-Windows platforms
func StartTrayIfWindows(config *Config) {
	// System tray is only supported on Windows
	// On other platforms, this function does nothing
	LogDebug("System tray not supported on this platform")
}
