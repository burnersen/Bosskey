// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// "Mit Windows starten": ein Eintrag unter HKCU\...\Run, nur für den
// angemeldeten Nutzer, ohne Adminrechte. Derselbe Ort, den Windows unter
// Einstellungen > Apps > Autostart anzeigt und abschalten lässt.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// autostartCommand ist, was in den Run-Eintrag kommt: der Pfad dieser exe
// in Anführungszeichen (Pfade mit Leerzeichen).
func autostartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `"`, nil
}

// autostartEnabled: es gibt einen Eintrag und er zeigt auf diese exe.
// Zeigt er auf eine andere (alte Kopie), gilt er als aus; einschalten
// schreibt dann den richtigen Pfad.
func autostartEnabled() bool {
	want, err := autostartCommand()
	if err != nil {
		return false
	}
	have, err := regString(runKey, appName)
	return err == nil && strings.EqualFold(have, want)
}

func setAutostart(on bool) error {
	var key syscall.Handle
	const keySetValue = 0x0002
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, utf16Ptr(runKey), 0, keySetValue, &key); err != nil {
		return fmt.Errorf("Run-Schlüssel öffnen: %w", err)
	}
	defer syscall.RegCloseKey(key) // ein Fehler beim Schließen ändert am geschriebenen Wert nichts

	name := utf16Ptr(appName)
	if !on {
		r, _, _ := procRegDeleteValueW.Call(uintptr(key), uintptr(unsafe.Pointer(name)))
		if r != 0 && syscall.Errno(r) != syscall.ERROR_FILE_NOT_FOUND {
			return fmt.Errorf("Autostart-Eintrag löschen: %w", syscall.Errno(r))
		}
		return nil
	}
	cmd, err := autostartCommand()
	if err != nil {
		return fmt.Errorf("Pfad der exe: %w", err)
	}
	data := syscall.StringToUTF16(cmd)
	r, _, _ := procRegSetValueExW.Call(uintptr(key), uintptr(unsafe.Pointer(name)), 0, syscall.REG_SZ,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r != 0 {
		return fmt.Errorf("Autostart-Eintrag schreiben: %w", syscall.Errno(r))
	}
	return nil
}

// regString liest einen REG_SZ-Wert unter HKEY_CURRENT_USER.
func regString(path, name string) (string, error) {
	b, err := regBinary(path, name)
	if err != nil {
		return "", err
	}
	if len(b) < 2 {
		return "", errors.New("leerer Wert")
	}
	u := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
	return syscall.UTF16ToString(u), nil
}
