// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Die Strukturen der Windows-API müssen genau so groß sein wie in den
// Headern von Windows. Gilt für 64 Bit; 32 Bit baut Bosskey nicht.
package main

import (
	"testing"
	"unsafe"
)

func TestWin32StructSizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("nur für 64 Bit")
	}
	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"NOTIFYICONDATAW", unsafe.Sizeof(notifyIconData{}), 976},
		{"INPUT", unsafe.Sizeof(keyboardInput{}), 40},
		{"WNDCLASSEXW", unsafe.Sizeof(wndClassEx{}), 80},
		{"MSG", unsafe.Sizeof(winMsg{}), 48},
		{"OSVERSIONINFOW", unsafe.Sizeof(osVersionInfo{}), 276},
		{"KBDLLHOOKSTRUCT", unsafe.Sizeof(kbdLLHookStruct{}), 24},
		{"RECT", unsafe.Sizeof(rect{}), 16},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %d Byte, Windows erwartet %d", c.name, c.got, c.want)
		}
	}
	// Felder, deren Lage Windows prüft: Sprechblasentext und die Tastenfelder.
	var n notifyIconData
	if off := unsafe.Offsetof(n.szInfo); off != 304 {
		t.Errorf("szInfo bei %d, erwartet 304", off)
	}
	if off := unsafe.Offsetof(n.hBalloonIcon); off != 968 {
		t.Errorf("hBalloonIcon bei %d, erwartet 968", off)
	}
	var k keyboardInput
	if off := unsafe.Offsetof(k.wVk); off != 8 {
		t.Errorf("wVk bei %d, erwartet 8", off)
	}
}
