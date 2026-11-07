# Bosskey

Ein kleines Programm für den Infobereich der Taskleiste: per Tastendruck auf einen virtuellen Desktop deiner Wahl. Gedacht als Boss-Taste: eine Taste, und du bist sofort auf Desktop 1. Mit mehreren Tasten springst du auf mehrere Desktops.

Nur für Windows 10 und 11 (x64 und ARM64).

## Die wichtigsten Zusicherungen

- **Kein Netzwerk.** Bosskey arbeitet nur lokal, es ruft nirgends an und lädt nichts nach.
- **Keine Adminrechte.** Alles läuft als normaler Nutzer, auch der Autostart.
- **Deine Einstellungsdatei gehört dir.** Bosskey liest sie, legt sie an, wenn sie fehlt, und hängt neue Einstellungen höchstens ans Ende an (vorher mit Sicherung als `.bak`). Deine Zeilen ändert es nie.
- **Es wird nichts gelöscht.** Bosskey drückt nur Strg+Win+Pfeil für dich, so wie du es auch selbst könntest.

## Benutzung

1. `Bosskey.exe` irgendwo hinlegen und doppelklicken. Unten rechts im Infobereich erscheint das Symbol (eventuell hinter dem kleinen Pfeil ^).
2. Ab jetzt springt die Taste **Pause** auf Desktop 1.
3. Andere Tasten einstellen: Rechtsklick aufs Symbol > **Einstellungen öffnen**, ändern, speichern. Nach zwei Sekunden gilt es, ohne Neustart.
4. Rechtsklick > **Mit Windows starten** setzt den Haken für den Autostart.

Das Menü am Symbol:

| Eintrag | Was er tut |
|---|---|
| Zu Desktop 1 ... | springt dorthin, rechts steht die Taste dafür |
| Einstellungen öffnen | öffnet `Bosskey.ini` im Editor |
| Einstellungen neu laden | liest sie sofort neu (passiert sonst von selbst nach dem Speichern) |
| Protokoll öffnen | öffnet das Log, falls etwas nicht klappt |
| Mit Windows starten | Autostart an oder aus (für deinen Benutzer, ohne Adminrechte) |
| Beenden | Symbol weg, Tasten wieder frei |

## Einstellungen

Die Datei `Bosskey.ini` sieht anfangs so aus (gekürzt):

```ini
meldungBeimStart=true
nochmalZurueck=false

[Tasten]
Pause = 1
```

| Einstellung | Standard | Bedeutung |
|---|---|---|
| `meldungBeimStart` | `true` | Beim Start kurz anzeigen, welche Tasten aktiv sind |
| `nochmalZurueck` | `false` | Nochmal drücken, obwohl du schon auf dem Ziel-Desktop bist, springt zurück dorthin, wo du vorher warst. Aus, damit ein doppelter Druck vor dem Chef nicht alles wieder aufdeckt |

Unter `[Tasten]` steht eine Zeile je Taste: `Taste = Desktop-Nummer`. Desktop 1 ist der erste, ganz links in der Übersicht (Win+Tab).

```ini
[Tasten]
Pause = 1
Strg+Alt+1 = 1
Strg+Alt+2 = 2
Strg+Alt+3 = 3
Strg+Alt+Z = zurück
```

- **Umschalttasten:** `Strg`, `Alt`, `Umschalt`, `Win`, `AltGr` (englisch geht auch: `Ctrl`, `Shift`).
- **Tasten mit Namen:** `Pause`, `Rollen`, `Druck`, `Einfg`, `Entf`, `Pos1`, `Ende`, `BildAuf`, `BildAb`, `Menue`, `Leertaste`, `Esc`, `Tab`, `Enter`, `Links`, `Hoch`, `Rechts`, `Runter`, `F1` bis `F24`, `Num0` bis `Num9`, `A` bis `Z`, `0` bis `9`, dazu Medientasten wie `Stumm`, `Leiser`, `Lauter`, `PlayPause`. Was keinen Namen hat, geht als Code von Windows, z. B. `0x13`.
- **`zurück`** statt einer Nummer springt dorthin, wo du vor dem letzten Sprung von Bosskey warst.
- **Gute Boss-Tasten** sind welche, die du sonst nie brauchst: `Pause`, `Rollen`, `Menue`. Eine einzelne Taste wie `F12` oder `A` ist danach in allen Programmen weg, auch in Spielen und beim Schreiben. Bosskey warnt bei Tasten, die du zum Schreiben brauchst.

Was nicht stimmt, meldet Bosskey als Benachrichtigung mit Zeilennummer und lässt die Zeile aus. Der Rest gilt.

## Wie der Wechsel funktioniert

Windows hat keine offizielle Schnittstelle, um direkt auf Desktop X zu springen. Die inoffizielle ändert sich mit fast jedem großen Windows-Update. Bosskey liest deshalb aus der Registry, auf welchem Desktop du bist, und drückt dann so oft Strg+Win+Pfeil, bis du am Ziel bist. Das hält jedes Update aus. Der Preis: du siehst kurz die Schiebe-Animation über die Desktops dazwischen. Wer sie nicht mag, schaltet unter Einstellungen > Barrierefreiheit > Visuelle Effekte die Animationseffekte aus.

## Wenn etwas nicht klappt

| Meldung | Was tun |
|---|---|
| ... ist schon von Windows oder einem anderen Programm belegt | Eine andere Taste nehmen. Viele Win-Kombinationen hat Windows selbst. |
| Desktop 4 gibt es nicht, du hast gerade 3 | Mit Win+Strg+D einen neuen Desktop anlegen oder die Nummer ändern. |
| Der Desktop-Wechsel hat nicht geklappt | Oft liegt ein Programm mit Adminrechten vorne (z. B. der Task-Manager). Einmal auf die Taskleiste klicken und nochmal drücken. |
| Bosskey läuft schon. | Das Symbol ist schon im Infobereich, eventuell hinter dem Pfeil ^. |

## Wo die Dateien liegen

| Was | Ort |
|---|---|
| Einstellungen | `%APPDATA%\Bosskey\Bosskey.ini` |
| Protokoll | `%LOCALAPPDATA%\Bosskey\logs\bosskey.log` |
| Autostart | Registry `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, Wert `Bosskey` |

Mit einer leeren Datei `portable.txt` neben der exe bleiben Einstellungen und Protokoll in diesem Ordner.

## Schalter für die Kommandozeile

Ohne Schalter startet Bosskey im Infobereich. Für Fehlersuche und CI:

| Schalter | Was er tut |
|---|---|
| `-version` | Version zeigen |
| `-pruefen` | Einstellungen prüfen und sagen, was gilt; ändert nichts |
| `-debug` | mehr Einzelheiten ins Protokoll |
| `-probelauf` | starten und nach drei Sekunden von selbst beenden (für die CI) |

Exit-Codes: `0` alles erledigt, `1` etwas ging schief (die Meldung sagt, was zu tun ist), `2` falscher Aufruf.

## Selbst bauen

Gebraucht wird Go in der Version aus `go.mod`. Keine weiteren Abhängigkeiten.

```
go test ./...
go vet ./...
GOOS=windows go vet ./...
go build -ldflags "-H windowsgui" -o Bosskey.exe .
```

Selbst gebaut gibt es das Standardsymbol von Windows. Das eigene Symbol bettet der Release-Bau mit go-winres ein.

## Lizenz

GPL-3.0-only, siehe [LICENSE](LICENSE).
