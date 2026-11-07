// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Die Funktionen der Windows-API, die Bosskey braucht. Alles über das
// Paket syscall der Standardbibliothek, ohne fremde Module: das Programm
// baut damit überall ohne Download.
package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")

	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procRegisterHotKey         = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey       = user32.NewProc("UnregisterHotKey")
	procSetTimer               = user32.NewProc("SetTimer")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenuW            = user32.NewProc("AppendMenuW")
	procTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procFindWindowW            = user32.NewProc("FindWindowW")
	procLoadImageW             = user32.NewProc("LoadImageW")
	procLoadIconW              = user32.NewProc("LoadIconW")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procMessageBoxW            = user32.NewProc("MessageBoxW")
	procSendInput              = user32.NewProc("SendInput")
	procMapVirtualKeyW         = user32.NewProc("MapVirtualKeyW")
	procGetAsyncKeyState       = user32.NewProc("GetAsyncKeyState")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW    = shell32.NewProc("ShellExecuteW")

	procGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW         = kernel32.NewProc("CreateMutexW")
	procAttachConsole        = kernel32.NewProc("AttachConsole")
	procProcessIdToSessionId = kernel32.NewProc("ProcessIdToSessionId")
	procRegSetValueExW       = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW      = advapi32.NewProc("RegDeleteValueW")
	procRtlGetVersion        = ntdll.NewProc("RtlGetVersion")
	procSetConsoleOutputCP   = kernel32.NewProc("SetConsoleOutputCP")
)

// Nachrichten und Schalter aus winuser.h und shellapi.h.
const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmNull        = 0x0000
	wmTimer       = 0x0113
	wmHotkey      = 0x0312
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	wmApp         = 0x8000

	nimAdd    = 0x0
	nimModify = 0x1
	nimDelete = 0x2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10

	niifInfo    = 0x1
	niifWarning = 0x2

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmBottomAlign = 0x0020

	imageIcon       = 1
	lrDefaultColor  = 0x0
	smCxSmIcon      = 49
	smCySmIcon      = 50
	idiApplication  = 32512
	swShowNormal    = 1
	mbIconWarning   = 0x30
	mbIconError     = 0x10
	mbSetForeground = 0x10000

	inputKeyboard       = 1
	keyEventExtendedKey = 0x0001
	keyEventKeyUp       = 0x0002

	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12 // Alt
	vkLWin     = 0x5B
	vkRWin     = 0x5C
	vkLControl = 0xA2
	vkLeft     = 0x25
	vkRight    = 0x27

	errorAlreadyExists           = 183
	errorHotkeyAlreadyRegistered = 1409
)

// utf16Ptr ist ein Zeiger auf Text für die API. Text mit NUL darin gibt es
// bei Bosskey nicht (eigene Texte, Pfade); dann bleibt es leer statt abzustürzen.
func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("") // ignore: leerer Text hat kein NUL
	}
	return p
}

// copyUTF16 schreibt s in ein festes Feld (Tooltip, Sprechblase) und kürzt
// es, damit am Ende immer die abschließende NUL Platz hat.
func copyUTF16(dst []uint16, s string) {
	u := syscall.StringToUTF16(s) // endet mit NUL
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

// attachParentConsole hängt Bosskey an die Konsole, aus der es gestartet
// wurde (cmd, PowerShell), damit -version und -pruefen dort antworten.
// Bosskey ist als Fensterprogramm gebaut (-H windowsgui) und hat sonst keine.
// Ist die Ausgabe umgeleitet (Datei, Pipe, CI), passt schon alles.
func attachParentConsole() {
	if h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != syscall.InvalidHandle {
		return
	}
	const attachParentProcess = ^uintptr(0) // (DWORD)-1
	if r, _, _ := procAttachConsole.Call(attachParentProcess); r == 0 {
		return // aus dem Explorer gestartet: es gibt keine Konsole
	}
	const cpUTF8 = 65001
	procSetConsoleOutputCP.Call(cpUTF8) // Umlaute; scheitert es, sehen sie nur falsch aus
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
}

// showMessageBox zeigt Text in einem Meldungsfenster, für Fehler beim Start
// im Tray-Betrieb (es gibt keine Konsole, die sie zeigen könnte).
func showMessageBox(text string) {
	body, title := utf16Ptr(text), utf16Ptr(appName)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(body)), uintptr(unsafe.Pointer(title)), mbIconWarning|mbSetForeground)
}
