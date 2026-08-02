package tray

import _ "embed"

// AppIcon is the generated multi-resolution Windows icon used by the
// executable, shortcuts, installer, and desktop windows.
//
//go:embed app.ico
var AppIcon []byte
