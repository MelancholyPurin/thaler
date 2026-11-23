<!-- translated from docs/config.md at d1a4d9b623f7 -->
# config.toml

[English](config.md) · [Español](config.es.md) · Deutsch

Jeder Schlüssel, den Thaler aus seiner Konfigurationsdatei liest: was er bewirkt, was er annimmt und was ohne ihn gilt. [`config.example.toml`](../config.example.toml) ist dasselbe als Datei zum Anfangen.

Thaler läuft auch ohne Konfigurationsdatei; alles unten hat einen Standardwert. Die Datei ist `~/.config/thaler/config.toml` (oder `$XDG_CONFIG_HOME/thaler/config.toml`) oder die mit `--config` angegebene, die dann existieren muss. In der Oberfläche öffnet `C` sie in deinem Editor und legt sie, falls es keine gibt, aus dem Beispiel an, mit allen Zeilen ausgeschaltet; `ctrl+l` und `ctrl+t` schreiben `language` und `theme` für dich, und die Regelansicht und der Regelvorschlag (`tab` in der Kategorieauswahl) ändern die `[[rule]]`-Tabellen an Ort und Stelle und lassen deine Kommentare und alles andere, wie du es geschrieben hast.

**Die Datei wird streng gelesen.** Ein Schlüssel, den Thaler nicht kennt, ist ein Fehler, ein Tippfehler kann also nicht stillschweigend eine Regel oder Einstellung abschalten; Schlüssel werden kleingeschrieben, `Language` oder `[[Rule]]` ist also auch ein Tippfehler. Ein Wert außerhalb des Bereichs ist ein Fehler, der den Schlüssel nennt. Eine Datei mit einem Fehler wird nicht verwendet: Befehle von `thaler cli`, die sie brauchen, brechen mit dem Fehler ab, und die Oberfläche sagt, was falsch ist, und lehnt Importe ab, bis es behoben ist. Sie wird bei jedem Import neu gelesen, eine Änderung gilt also ab dem nächsten.

Die Schlüssel unten sind vollständig geschrieben: `profile.columns.date` ist `date` unter `[profile.columns]` eines `[[profile]]`.

## Einstellungen

| Schlüssel | Nimmt | Ohne ihn | Was er bewirkt |
| --- | --- | --- | --- |
| `language` | `"en"`, `"es"` oder `"de"` | die Umgebung entscheidet: `LC_ALL`, `LC_MESSAGES`, dann `LANG` (`de_DE.UTF-8`); eine Sprache, die Thaler nicht spricht, ist Englisch | Die Sprache der Oberfläche. Eine noch nicht übersetzte Meldung erscheint auf Englisch. `thaler cli` ist Englisch, egal was hier steht. `"pseudo"` ist eine ausgedachte Sprache, länger und mit Akzenten, um Text zu finden, der nicht passt. |
| `theme` | `"auto"`, `"dark"` oder `"light"` | `"auto"` | Die Farben der Oberfläche. `"auto"` folgt dem Hintergrund des Terminals; `"dark"` und `"light"` legen sie fest für ein Terminal, das nicht sagt, welchen Hintergrund es hat (manche Multiplexer, eine Sitzung über ssh). |
| `import_dir` | ein Ordner; `~` ist dein Home-Ordner | `~/Downloads` | Der Ordner, aus dem die Oberfläche Bankexporte listet, wenn du `i` drückst. Du kannst immer eine andere Datei oder einen anderen Ordner tippen oder hineinziehen. |
| `watch_import_dir` | `true` oder `false` | `true` | Ob die Oberfläche, solange sie läuft, alle paar Sekunden in `import_dir` schaut, nur auf Namen und Zeiten der CSV-Dateien, und in der unteren Leiste meldet, wenn ein neuer Export angekommen ist. |
| `backup_reminder_days` | eine ganze Zahl, 0 oder mehr | `7` | Die Oberfläche erinnert dich an ein Backup, wenn das neueste so viele Tage alt ist oder es keines gibt und du Transaktionen hast. `0` schaltet die Erinnerung aus. |
| `keep_backups` | eine ganze Zahl, 0 oder mehr | `10` | Wie viele der neuesten Backups in jedem Backup-Ordner behalten werden: die Backups, die du machst, die Datenbanken, die Wiederherstellungen ersetzt haben, und die Kopien vor Upgrades. Sind es mehr, bietet die Oberfläche an, die ältesten zu entfernen (`ctrl+p`), und `thaler cli prune` listet sie; nichts wird ohne ein Ja entfernt. `0` behält alles und schaltet das Angebot ab. |

## Funktionen

| Schlüssel | Nimmt | Ohne ihn | Was er bewirkt |
| --- | --- | --- | --- |
| `features.<name>` | `true` oder `false` | aus | Schaltet eine Story ein, die vor ihrer Fertigstellung eingebaut wurde, um sie früh auszuprobieren. Ein Name, den es nicht gibt, wird abgelehnt, und der Fehler zählt die vorhandenen auf. In dieser Version ist nichts hinter einem Schalter. |

```toml
[features]
some_feature = true
```

## Regeln: `[[rule]]`

Jede `[[rule]]` gibt importierten Transaktionen eine Kategorie. Sie werden von oben geprüft, und die erste passende gewinnt; eine Transaktion, zu der keine Regel passt, bleibt in `Expenses:Uncategorized` oder `Income:Uncategorized`, damit du entscheidest. Eine Regel wird mit Empfänger, Verwendungszweck und Buchungstext einer Transaktion verglichen, durch Leerzeichen verbunden, ohne Rücksicht auf Groß- und Kleinschreibung.

| Schlüssel | Nimmt | Ohne ihn | Was er bewirkt |
| --- | --- | --- | --- |
| `rule.contains` | Text | — | Passt zu einer Transaktion, deren Text dies enthält, ohne Rücksicht auf Groß- und Kleinschreibung und auf Leerzeichen drumherum. |
| `rule.regex` | ein regulärer Ausdruck ([Go-Syntax](https://pkg.go.dev/regexp/syntax)) | — | Passt zu einer Transaktion, in deren Text dieser Ausdruck etwas findet, ohne Rücksicht auf Groß- und Kleinschreibung. |
| `rule.category` | ein Kontoname wie `Expenses:Groceries` | erforderlich | Die Kategorie, auf die eine passende Transaktion gebucht wird. Eine, die es noch nicht gibt, wird beim Import angelegt. `Equity:Transfers` kennzeichnet Geld, das zwischen deinen eigenen Konten bewegt wurde; Thaler paart die beiden Hälften einer solchen Umbuchung selbst (gleicher Betrag, innerhalb von drei Tagen), und eine Regel kennzeichnet nur eine, deren andere Hälfte noch nicht importiert ist. |

Eine Regel hat genau eines von `contains` und `regex` sowie eine `category`; eine Regel mit keinem, mit beiden, mit einem kaputten Ausdruck oder mit einer Kategorie, die kein gültiger Name ist, ist ein Fehler, der die Nummer der Regel nennt.

```toml
[[rule]]
contains = "REWE"
category = "Expenses:Groceries"

[[rule]]
regex = "b(ä|ae)ckerei|edeka|lidl|aldi"
category = "Expenses:Groceries"
```

## Bankprofile: `[[profile]]`

Ein Profil sagt, wie der Export einer Bank zu lesen ist. Thaler bringt vier mit: `sparkasse` (die Exporte CSV-CAMT und CSV-MT940 aus dem Online-Banking der Sparkasse), `ofx`, `qif` und `camt` (CAMT.053-Auszüge). Ein eigenes Profil brauchst du nur für das CSV einer anderen Bank oder für eine QIF-Datei, deren Daten ein eingebautes Profil verkehrt herum liest.

`thaler cli import --profile NAME` wählt ein Profil; ohne die Option, und in der Oberfläche immer, wird es aus der Datei ermittelt (`auto`): eine OFX-, QIF- oder CAMT.053-Datei an ihrem Anfang, eine CSV-Datei an ihrer Kopfzeile. Unter den CSV-Profilen, deren Datums- und Betragsspalten die Kopfzeile hat, gewinnt das, das die meisten seiner Spalten findet, und unter gleich guten kommen deine zuerst, in der geschriebenen Reihenfolge. Ein eigenes Profil mit dem Namen eines eingebauten ersetzt dieses.

| Schlüssel | Nimmt | Ohne ihn | Was er bewirkt |
| --- | --- | --- | --- |
| `profile.name` | ein Name | erforderlich | Wie `--profile` es nennt, ohne Rücksicht auf Groß- und Kleinschreibung, und womit ein Import sagt, dass er die Datei gelesen hat. |
| `profile.format` | `"csv"`, `"ofx"`, `"qif"` oder `"camt"` | `"csv"` | Die Art der Datei. Eine OFX- oder CAMT.053-Datei braucht sonst nichts; eine QIF-Datei nimmt `date_formats` und `decimal_separator`; die Spalten, das Trennzeichen, die Kodierung und die Ausschlussregel gelten nur für CSV. |
| `profile.delimiter` | ein Zeichen oder `"auto"` | `"auto"` | Was die Spalten trennt. `"auto"` probiert `;`, `,`, Tabulator und `\|` und behält das, das die Kopfzeile in die Datums- und Betragsspalte teilt. |
| `profile.encoding` | `"auto"`, `"utf-8"`, `"utf-16"`, `"utf-16le"`, `"utf-16be"` oder `"windows-1252"` (auch `"latin1"`, `"iso-8859-1"`) | `"auto"` | Die Zeichenkodierung. `"auto"` liest UTF-16 oder UTF-8, wenn die Bytes es sagen, und sonst Windows-1252, das deutsche Banken noch verwenden. |
| `profile.decimal_separator` | `","` oder `"."` | für jeden Betrag ermittelt | Das Dezimaltrennzeichen der Beträge. Setze es, wenn Beträge wie `1.234` sich auf beide Arten lesen ließen. |
| `profile.date_formats` | eine Liste von Formaten aus `DD`, `MM`, `YY` und `YYYY`, etwa `"DD.MM.YYYY"` | `["YYYY-MM-DD", "DD.MM.YYYY"]` | Wie Daten geschrieben sind. Jedes Datum wird mit dem ersten passenden Format gelesen; eine QIF-Datei verwendet das erste Format, das jedes ihrer Daten liest. Ein Go-Layout wie `"2006-01-02"` funktioniert auch. |
| `profile.skip_column` | ein Spaltenname | keine Zeile wird übersprungen | Mit `skip_contains`: Eine Zeile, deren Wert in dieser Spalte `skip_contains` enthält, ohne Rücksicht auf Groß- und Kleinschreibung, wird weggelassen, etwa eine vorgemerkte Buchung. |
| `profile.skip_contains` | Text | keine Zeile wird übersprungen | Siehe `skip_column`. |

### Spalten: `[profile.columns]`

Jeder Schlüssel listet die Spaltennamen, die dieses Feld enthalten können; verwendet wird der erste, den die Datei hat. Spaltennamen werden ohne Rücksicht auf Groß- und Kleinschreibung verglichen, und `ä`, `ö`, `ü` und `ß` zählen als `ae`, `oe`, `ue` und `ss`. Ein CSV-Profil braucht `date` und `amount`; die anderen bleiben leer, wenn sie fehlen.

| Schlüssel | Nimmt | Ohne ihn | Was er bewirkt |
| --- | --- | --- | --- |
| `profile.columns.date` | eine Liste von Spaltennamen | für CSV erforderlich | Der Buchungstag. |
| `profile.columns.amount` | eine Liste von Spaltennamen | für CSV erforderlich | Der Betrag, negativ für hinausgehendes Geld. |
| `profile.columns.payee` | eine Liste von Spaltennamen | der Buchungstext | Wer bezahlt wurde oder wer bezahlt hat. |
| `profile.columns.purpose` | eine Liste von Spaltennamen | leer | Verwendungszweck oder Referenz. |
| `profile.columns.iban` | eine Liste von Spaltennamen | leer | Das Konto der Gegenseite. |
| `profile.columns.booking_text` | eine Liste von Spaltennamen | leer | Die Art der Buchung (Kartenzahlung, Überweisung, Entgelt). |
| `profile.columns.currency` | eine Liste von Spaltennamen | die Währung des Kontos | Die Währung jeder Zeile. |

Empfänger, Verwendungszweck und Buchungstext sind das, womit die Regeln vergleichen. Zusammen mit Datum, Betrag und IBAN bilden sie auch den Schlüssel, der eine schon importierte Zeile erkennt; zu ändern, welche Spalten ein Profil liest, kann einen Export also noch einmal importieren lassen.

Ein Profil hat ein Problem, wenn sein Format, Trennzeichen, seine Kodierung oder sein Dezimaltrennzeichen keiner der Werte oben ist oder einem CSV-Profil die Datums- oder Betragsspalte fehlt; der Fehler nennt das Profil.

```toml
[[profile]]
name = "mybank"
delimiter = ","
decimal_separator = "."
date_formats = ["YYYY-MM-DD"]
skip_column = "Status"
skip_contains = "pending"

[profile.columns]
date = ["Date", "Booking date"]
payee = ["Description"]
amount = ["Amount"]

[[profile]]
name = "mybank-qif"
format = "qif"
date_formats = ["DD/MM/YY"]
decimal_separator = ","
```
