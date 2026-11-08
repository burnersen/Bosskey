// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Die beiden Animationsschalter von Windows lesen und setzen, über
// SystemParametersInfo. Gilt nur für den angemeldeten Benutzer, ohne
// Adminrechte, und sofort (SPIF_SENDCHANGE sagt es allen Programmen).
package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	spiGetAnimation           = 0x0048
	spiSetAnimation           = 0x0049
	spiGetClientAreaAnimation = 0x1042
	spiSetClientAreaAnimation = 0x1043

	spifUpdateIniFile = 0x01 // dauerhaft, auch nach Neustart
	spifSendChange    = 0x02 // alle Fenster erfahren es gleich
)

// animationInfo ist ANIMATIONINFO (winuser.h), 8 Byte.
type animationInfo struct {
	size       uint32
	minAnimate int32
}

func getAnimations() (animations, error) {
	var a animations
	var client int32
	if r, _, err := procSystemParametersInfoW.Call(spiGetClientAreaAnimation, 0, uintptr(unsafe.Pointer(&client)), 0); r == 0 {
		return a, fmt.Errorf("Animationseffekte lesen: %w", err)
	}
	ai := animationInfo{size: uint32(unsafe.Sizeof(animationInfo{}))}
	if r, _, err := procSystemParametersInfoW.Call(spiGetAnimation, uintptr(ai.size), uintptr(unsafe.Pointer(&ai)), 0); r == 0 {
		return a, fmt.Errorf("Fensteranimation lesen: %w", err)
	}
	a.ClientArea = client != 0
	a.MinMax = ai.minAnimate != 0
	return a, nil
}

func setAnimations(a animations) error {
	const flags = spifUpdateIniFile | spifSendChange
	// Bei SPI_SETCLIENTAREAANIMATION steht der Wert selbst im Zeiger-Argument.
	if r, _, err := procSystemParametersInfoW.Call(spiSetClientAreaAnimation, 0, boolArg(a.ClientArea), flags); r == 0 {
		return fmt.Errorf("Animationseffekte setzen: %w", errnoOr(err))
	}
	ai := animationInfo{size: uint32(unsafe.Sizeof(animationInfo{}))}
	if a.MinMax {
		ai.minAnimate = 1
	}
	if r, _, err := procSystemParametersInfoW.Call(spiSetAnimation, uintptr(ai.size), uintptr(unsafe.Pointer(&ai)), flags); r == 0 {
		return fmt.Errorf("Fensteranimation setzen: %w", errnoOr(err))
	}
	return nil
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// errnoOr: Call gibt immer einen Fehler zurück, auch "erfolgreich". Nur
// ein echter Code zählt.
func errnoOr(err error) error {
	if e, ok := err.(syscall.Errno); ok && e != 0 {
		return e
	}
	return fmt.Errorf("unbekannter Grund")
}
