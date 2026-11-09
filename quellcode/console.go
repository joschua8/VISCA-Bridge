package main

import "os"

// consoleAvailable meldet, ob es eine Konsole gibt. In der Version ohne
// Fenster (-H windowsgui) ist Stdout ungültig, dann wird nur in die Datei geloggt.
func consoleAvailable() bool {
	if os.Stdout == nil {
		return false
	}
	_, err := os.Stdout.Stat()
	return err == nil
}
