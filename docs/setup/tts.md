# AI Voice (Text-to-Speech) einrichten

engelOS liest Stream-Events mit einer ElevenLabs-Stimme vor: neue Subs, Resubs,
Gift-Subs und Raids, dazu ein optionaler Channel-Point-Reward, mit dem Zuschauer
den Bot etwas sagen lassen. Die Synthese laeuft serverseitig, das Audio spielt
ein OBS-Browser-Source-Overlay ab. Du bringst deinen eigenen ElevenLabs-Key mit
(BYOK).

> Voraussetzung: `ENGELOS_SECRETS_KEY` muss auf dem Daemon gesetzt sein (32-Byte
> base64, erzeugen mit `openssl rand -base64 32`). Ohne ihn ist die
> Token-Verschluesselung aus und AI Voice bleibt deaktiviert.

---

## 1. ElevenLabs API-Key holen

1. Konto auf <https://elevenlabs.io> anlegen oder anmelden.
2. Im Profilmenue auf den API-Key-Bereich gehen und einen Key erzeugen.
3. Den Key kopieren. Jeder Account hat sein eigenes Zeichen-Kontingent; der
   Free-Tier reicht zum Ausprobieren, fuer Dauerbetrieb empfiehlt sich ein
   bezahlter Tier (hoehere Concurrency und mehr Zeichen).

---

## 2. Im Dashboard verbinden

1. Oeffne das Dashboard und waehle oben den Workspace (Kanal).
2. Gehe in der Seitenleiste auf **AI Voice**.
3. Trage den **ElevenLabs API key** ein. Er wird verschluesselt gespeichert und
   nie wieder im Klartext angezeigt; das Dashboard zeigt nur an, ob ein Key
   hinterlegt ist.
4. Klick **Load voices from my account** und waehle eine **Stimme** aus der
   Liste. Alternativ eine Voice-ID direkt eintragen.
5. **Modell** waehlen:
   - **Flash v2.5** (Standard) - schnellste Latenz, 32 Sprachen.
   - **Multilingual v2** - hoechste Qualitaet, etwas langsamer.
6. **Enable voice alerts** anhaken und **Save changes**.

---

## 3. Audio-Overlay in OBS einbinden

Der Bot synthesisiert das Audio, abgespielt wird es im Browser, damit OBS es als
Quelle aufnehmen kann (kein Virtual-Audio-Cable noetig).

1. In OBS eine neue **Browser-Quelle** anlegen.
2. Als URL die Overlay-Adresse deiner Installation eintragen:
   ```
   https://DEINE-DOMAIN/overlay/tts
   ```
   Fuer die Standard-Installation auf `bot.engels.wtf`:
   ```
   https://bot.engels.wtf/overlay/tts
   ```
3. Die Quelle braucht keine sichtbare Groesse; sie spielt nur Audio. Stelle
   sicher, dass OBS die Browser-Quelle nicht stummschaltet und **Control audio
   via OBS** aktiv ist, falls du die Lautstaerke mischen willst.

Mehrere Alerts ueberlappen nie: das Overlay spielt sie nacheinander aus einer
Warteschlange ab.

---

## 4. Trigger

Sobald aktiviert, werden diese Events automatisch vorgelesen:

| Event | Gesprochene Zeile |
|---|---|
| Neuer Sub | "{user} just subscribed!" |
| Resub | "{user} just resubscribed for {N} months!" |
| Gift-Sub | "{gifter} just gifted a subscription!" |
| Raid | "{raider} is raiding with {N} viewers!" |

### Channel-Point-Reward "Speak with AI voice"

Zusaetzlich kannst du einen Custom-Reward an die Stimme binden, sodass Zuschauer
den Bot etwas sagen lassen (klassisches "lies meine Nachricht vor"). Das setzt
voraus, dass Channel Points aktiv sind (Kanal ist Affiliate oder Partner und
Login-with-Twitch mit `channel:manage:redemptions` ist erfolgt).

1. Gehe im Dashboard auf **Redemptions**.
2. Lege eine Bindung an oder bearbeite eine bestehende.
3. Waehle als Aktion **Speak with AI voice**.
4. Im Vorlagenfeld die zu sprechende Vorlage eintragen. Platzhalter:
   `$user`, `$input`, `$reward`, `$cost`. Laesst du das Feld leer, wird die
   Eingabe des Einloesers (`$input`) vorgelesen.

---

## Hinweise

- **Key bleibt serverseitig:** Das Overlay erhaelt nur fertiges Audio, niemals
  den Key.
- **Kontingent:** Lange Streams koennen das ElevenLabs-Zeichenkontingent
  aufbrauchen. Bei Erreichen des Limits antwortet ElevenLabs mit einem Fehler,
  der Alert wird dann uebersprungen.
- **Reihenfolge:** Synthese laeuft pro Kanal sequenziell, damit Alerts nicht
  ueberlappen und das Concurrency-Limit des Keys nicht reisst.
- **Sprache/Aussprache:** Zahlen wie "$50" liest Flash v2.5 unter Umstaenden
  woertlich; fuer saubere Aussprache kann das Multilingual-Modell besser passen.
