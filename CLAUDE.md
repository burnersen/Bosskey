# Anleitung für KI in diesem Projekt

Gilt für jede KI, die hier arbeitet (`AGENTS.md` zeigt hierher). Diese Datei ist öffentlich: nur Dinge hineinschreiben, die jeder lesen darf. Private Notizen gehören nach `_notizen/` (wird nicht eingecheckt).

## Grundlage

- Dieses Projekt folgt dem **dev-starterkit** (`burnersen/dev-starterkit`, privat). Kit-Stand beim Anlegen: **1.2**, Vorlage `go-cli`. Vor größerer Arbeit das Kit holen und die passenden Leitlinien lesen (`03` bis `09`). Ist das Kit neuer als der Stand oben, kurz sagen, was sich laut `AENDERUNGEN-*.md` geändert hat.
- Antworte auf Deutsch, locker, ohne Gedankenstriche. Erst sagen, wie du den Auftrag verstanden hast, bei allem Größeren auf Okay warten.
- Befehle, die der Nutzer selbst ausführt: als ganzer Block zum Kopieren, mit Sicherung vorher und dem Befehl zum Zurückholen (`01-arbeitsweise.md`).

## Steckbrief

| | |
|---|---|
| Zweck | Tray-Programm: per Hotkey auf einen virtuellen Desktop springen (Boss-Taste) |
| Sichtbarkeit | privat |
| Sprache Kommentare, Commits, Meldungen | Deutsch |
| Plattformen | Windows 10 und 11, x64 und ARM64 (Linux nur für Tests und vet) |
| Lizenz | GPL-3.0-only |

## Befehle

```
gofmt -l .                      # muss leer bleiben
go vet ./...
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=arm64 go vet ./...
go test -race -count=1 ./...
GOOS=windows go build -ldflags "-H windowsgui" -o Bosskey.exe .
```

Ohne Go-Proxy in der Cloud-Sitzung: Bosskey hat keine Abhängigkeiten, eine Kopie mit `go 1.24` in `go.mod` und `GOTOOLCHAIN=local` reicht für vet, Tests und den Windows-Bau.

Release: vorher `.github/release-notes/1.2.3.md` schreiben, dann Tag `v1.2.3` pushen oder (aus der Cloud-Sitzung) Actions > Release > Run workflow (`07-build-und-release.md`).

## Aufbau

Siehe Kopfkommentar in `main.go`. `os.Exit` steht nur in `main()`, Fehler beim Start zeigt `finish()` (im Tray-Betrieb gesammelt als Meldungsfenster), alles danach kommt als Benachrichtigung am Symbol. Alle Pfade kommen aus `paths.go`, alle Einstellungen aus der Tabelle in `config.go`. Die Tasten stehen seit 0.2.0 in `tasten.json` (schreibt nur das Programm, Kit K2), eingerichtet im Fenster aus `setup_windows.go`; der alte Abschnitt `[Tasten]` der INI wird nur einmal übernommen (`bindings.go`, `migrateBindings`).

- Windows-API nur über `syscall` der Standardbibliothek, keine fremden Module. Strukturen stehen ohne Build-Tag in `win32types.go`, `TestWin32StructSizes` prüft ihre Größen gegen die Windows-Header.
- Desktop-Wechsel: Registry lesen (`VirtualDesktopIDs`, `CurrentVirtualDesktop`), dann Strg+Win+Pfeil Schritt für Schritt mit Prüfung nach jedem Schritt (`desktop_windows.go`). Die inoffizielle COM-Schnittstelle bewusst nicht, sie bricht bei Windows-Updates.
- Tasten aufnehmen: Tastatur-Hook (WH_KEYBOARD_LL) nur während der Aufnahme. Die Logik, was verschluckt wird und wann der Hook wegkann, steht testbar in `capture.go`. Notbremsen: 15 Sekunden Zeitlimit, und der Tray nimmt einen hängengebliebenen Hook nach 3 Sekunden weg.
- Animationen: `animation.go` (Zustand, "vorher" merken, testbar) und `animation_windows.go` (SystemParametersInfo, ohne Adminrechte). Nie ein "vorher" überschreiben, das schon gemerkt ist.
- Rechnen ohne Windows (`hotkey.go`, `desktop.go`, `config.go`, `bindings.go`, `capture.go`, `animation.go`) ist unter Linux getestet. Das Echte (Tasten, Animation, Adminprogramme im Vordergrund) lässt sich nur auf einem echten Windows prüfen; die CI startet den Tray dort zweimal mit `-probelauf` (erster Start mit Fenster, zweiter ohne).
- `logging.go`, `files.go` und `paths.go` kommen unverändert aus dem Kit und sind deshalb englisch kommentiert.

## Vor jedem Commit

Die Checklisten aus `03` bis `09` des Kits durchgehen, mindestens: gofmt leer, vet und Tests grün, kein neues `_ =` ohne Kommentar, keine Geheimnisse und privaten Pfade, README und Release-Notes nachgezogen, wenn sich für Nutzer etwas ändert. Danach `bash <kit>/werkzeuge/projekt-pruefen.sh .`

## Das Kit lernt mit

Ergibt sich etwas, das auch anderen Projekten hilft (Fehler, der wiederkommen kann, gute Lösung, Bibliothek mit gutem oder schlechtem Ergebnis, Build-Trick, bessere Regel), am Ende des Arbeitsschritts in einem Satz fragen: "Das wäre was fürs Kit: <worum es geht>. Übernehmen?" Ablauf in `21-kit-lernt-mit.md` des Kits.
