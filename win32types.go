// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Strukturen der Windows-API, so wie sie unter 64 Bit (x64 und ARM64) im
// Speicher liegen. Sie stehen ohne Build-Tag hier, damit ein Test unter
// Linux ihre Größe gegen die Werte aus den Windows-Headern prüft: eine
// falsche Größe merkt Windows sonst nur still (Symbol fehlt, Tasten kommen
// nicht an).
package main

// notifyIconData ist NOTIFYICONDATAW (shellapi.h), 976 Byte unter 64 Bit.
type notifyIconData struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32 // Union mit uTimeout
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         guid
	hBalloonIcon     uintptr
}

type guid struct {
	data1 uint32
	data2 uint16
	data3 uint16
	data4 [8]byte
}

// keyboardInput ist INPUT (winuser.h) mit dem Teil KEYBDINPUT, 40 Byte
// unter 64 Bit. Die Union ist so groß wie MOUSEINPUT, daher der Rest am Ende.
type keyboardInput struct {
	inputType uint32
	_         uint32
	wVk       uint16
	wScan     uint16
	dwFlags   uint32
	time      uint32
	_         uint32
	extraInfo uintptr
	_         [8]byte
}

// wndClassEx ist WNDCLASSEXW, 80 Byte unter 64 Bit.
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// winMsg ist MSG, 48 Byte unter 64 Bit.
type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	ptX      int32
	ptY      int32
	lPrivate uint32
}

// point ist POINT.
type point struct{ x, y int32 }

// osVersionInfo ist OSVERSIONINFOW für RtlGetVersion, 276 Byte.
type osVersionInfo struct {
	size     uint32
	major    uint32
	minor    uint32
	build    uint32
	platform uint32
	csd      [128]uint16
}

// kbdLLHookStruct ist KBDLLHOOKSTRUCT, 24 Byte unter 64 Bit. So meldet der
// Tastatur-Hook jeden Tastendruck.
type kbdLLHookStruct struct {
	vkCode    uint32
	scanCode  uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

// rect ist RECT.
type rect struct{ left, top, right, bottom int32 }
