// Bosskey (https://github.com/burnersen/Bosskey)
// Copyright (C) 2026 burnersen
// SPDX-License-Identifier: GPL-3.0-only

// Rechnen mit virtuellen Desktops: wo bin ich, wohin soll es gehen. Ohne
// Windows, damit die Tests unter Linux laufen. Das Lesen aus der Registry
// und das Drücken der Tasten steht in desktop_windows.go.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// guidSize: Windows führt jeden Desktop unter einer GUID (16 Byte) und
// legt die Liste als REG_BINARY hintereinander ab.
const guidSize = 16

// maxDesktop ist nur eine Grenze gegen Tippfehler ("Pause = 11" statt 1).
// Windows selbst nennt keine Obergrenze.
const maxDesktop = 99

var errCurrentUnknown = errors.New("aktueller Desktop steht nicht in der Liste")

// desktopPosition sucht den aktuellen Desktop in der Liste aller Desktops
// und liefert seine Position (ab 0) und die Anzahl. Eine leere Liste heißt:
// es gab nie mehr als einen Desktop, dann schreibt Windows keine.
func desktopPosition(ids, current []byte) (pos, count int, err error) {
	if len(ids)%guidSize != 0 {
		return 0, 0, fmt.Errorf("Desktop-Liste hat %d Byte, kein Vielfaches von %d", len(ids), guidSize)
	}
	count = len(ids) / guidSize
	if count <= 1 {
		return 0, 1, nil
	}
	if len(current) != guidSize {
		return 0, count, errCurrentUnknown
	}
	for i := range count {
		if bytes.Equal(ids[i*guidSize:(i+1)*guidSize], current) {
			return i, count, nil
		}
	}
	return 0, count, errCurrentUnknown
}

// target ist, wohin eine Taste springt: ein Desktop (ab 1, wie Windows
// sie in der Übersicht zählt) oder zurück zum vorigen.
type target struct {
	Desktop int  // 1 bis maxDesktop, 0 bei Back
	Back    bool // zurück zu dem Desktop vor dem letzten Sprung
}

func parseTarget(v string) (target, error) {
	switch normalizeKeyText(v) {
	case "zurueck", "back", "vorher", "voriger":
		return target{Back: true}, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return target{}, errors.New("erwartet wird eine Desktop-Nummer (1, 2, 3 ...) oder \"zurück\"")
	}
	if n < 1 || n > maxDesktop {
		return target{}, fmt.Errorf("Desktop-Nummer muss zwischen 1 und %d liegen", maxDesktop)
	}
	return target{Desktop: n}, nil
}

func (t target) String() string {
	if t.Back {
		return "zurück zum vorigen Desktop"
	}
	return "Desktop " + strconv.Itoa(t.Desktop)
}

// noPrevious: noch kein Sprung gemacht, es gibt kein "vorher".
const noPrevious = -1

// decideDestination sagt, auf welchen Desktop (ab 0) es geht.
//
//	current   wo wir gerade sind (ab 0)
//	previous  wo wir vor dem letzten Sprung von Bosskey waren, sonst noPrevious
//	toggle    Einstellung nochmalZurueck: wer schon am Ziel ist und
//	          nochmal drückt, kommt zurück
//
// Gleich current heißt: nichts zu tun.
func decideDestination(t target, current, previous int, toggle bool) (int, error) {
	if t.Back {
		if previous == noPrevious {
			return current, userError(nil, "Noch kein Desktop zum Zurückspringen",
				"Zurück geht erst, nachdem Bosskey einmal gesprungen ist.")
		}
		return previous, nil
	}
	dest := t.Desktop - 1
	if dest == current && toggle && previous != noPrevious && previous != current {
		return previous, nil
	}
	return dest, nil
}
