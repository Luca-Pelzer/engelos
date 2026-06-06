# YouTube Live Chat verbinden

Diese Anleitung verbindet engelOS mit dem YouTube Live Chat: der Bot liest und
schreibt Nachrichten im Live-Chat eines laufenden Streams. Die Einrichtung ist
einmalig pro Betreiber.

> Du brauchst einen Google-Account mit Zugriff auf den YouTube-Kanal, als dessen
> Identitaet der Bot lesen und schreiben soll. Sammle am Ende die fett
> markierten Werte, die kommen als ENV-Variablen in die Daemon-Konfiguration.

---

## 1. Google Cloud Projekt anlegen

1. Oeffne die <https://console.cloud.google.com/>.
2. Oben links neben "Google Cloud" auf die Projekt-Auswahl klicken, dann
   **Neues Projekt**.
3. Name: `engelos-youtube` (frei waehlbar), dann **Erstellen**.
4. Nach ein paar Sekunden das neue Projekt oben links auswaehlen.

---

## 2. YouTube Data API v3 aktivieren

1. Oeffne <https://console.cloud.google.com/apis/library/youtube.googleapis.com>.
2. Stelle sicher, dass oben das Projekt `engelos-youtube` ausgewaehlt ist.
3. **Aktivieren** klicken.

Ohne aktivierte API liefert jeder Live-Chat-Aufruf spaeter einen 403-Fehler.

---

## 3. OAuth Consent Screen konfigurieren

1. Oeffne <https://console.cloud.google.com/apis/credentials/consent>.
2. Nutzertyp **Extern** waehlen, dann **Erstellen**.
3. Pflichtfelder ausfuellen:
   - **App-Name:** `engelOS`
   - **Nutzer-Support-E-Mail:** deine E-Mail
   - **Entwickler-Kontakt:** deine E-Mail
4. Durch die restlichen Schritte mit **Speichern und fortfahren** klicken.
   Scopes muessen hier nicht gesetzt werden, der Daemon fordert sie zur Laufzeit
   an (siehe unten).
5. Bei **Testnutzer** auf **Add Users** und die **eigene Google-/YouTube-Adresse**
   eintragen, mit der du den Bot verbinden willst. Solange die App im
   Test-Status ist, darf sich nur ein eingetragener Testnutzer anmelden, sonst
   blockt Google den Login.

> Solange die App "Im Test" steht, laeuft alles. Eine Google-Verifizierung ist
> nur noetig, wenn du fremde Nutzer ausserhalb der Testliste zulassen willst.

---

## 4. OAuth-Client-ID erstellen

1. Oeffne <https://console.cloud.google.com/apis/credentials>.
2. Oben **+ Anmeldedaten erstellen**, dann **OAuth-Client-ID**.
3. **Anwendungstyp:** `Webanwendung`.
4. **Name:** `engelos-bot`.
5. Unter **Autorisierte Weiterleitungs-URIs** auf **URI hinzufuegen** und exakt
   die Callback-URL deiner Installation eintragen:
   ```
   https://DEINE-DOMAIN/api/v1/auth/youtube/callback
   ```
   Fuer die Standard-Installation auf `bot.engels.wtf`:
   ```
   https://bot.engels.wtf/api/v1/auth/youtube/callback
   ```
   Lokal in der Entwicklung:
   ```
   http://localhost:8080/api/v1/auth/youtube/callback
   ```
6. **Erstellen**. Im Popup erscheinen **Client-ID** und **Client-Secret**, beide
   kopieren (oder JSON herunterladen). Das Secret wird spaeter nicht mehr im
   Klartext angezeigt.

**Sammeln:**
- **Client-ID** -> `ENGELOS_YOUTUBE_CLIENT_ID`
- **Client-Secret** -> `ENGELOS_YOUTUBE_CLIENT_SECRET`
- Redirect-URL (oben), exakt wie eingetragen -> `ENGELOS_YOUTUBE_REDIRECT_URL`

---

## 5. ENV-Variablen setzen

Trage die drei Werte in die Daemon-Konfiguration ein (Standard-Deploy:
`/etc/engelos/engelos.env`):

```
ENGELOS_YOUTUBE_CLIENT_ID=...
ENGELOS_YOUTUBE_CLIENT_SECRET=...
ENGELOS_YOUTUBE_REDIRECT_URL=https://bot.engels.wtf/api/v1/auth/youtube/callback
```

Voraussetzung wie bei jedem OAuth-Provider: `ENGELOS_SECRETS_KEY` muss gesetzt
sein (32-Byte base64, erzeugen mit `openssl rand -base64 32`), sonst wird der
verschluesselte Token-Speicher nicht aktiviert.

Danach den Daemon neu starten:

```bash
systemctl restart engelos
```

Fehlt einer der drei YouTube-Werte, bleiben die OAuth-Routen lautlos
ungemountet (genau wie bei Twitch und Spotify). Im Log erscheint dann
`youtube oauth disabled`. Sind alle drei gesetzt, steht
`GET /api/v1/auth/youtube/login` bereit und der "Connect"-Button auf der
Connections-Seite funktioniert.

---

## 6. Verbinden (OAuth-Flow)

1. Oeffne das Dashboard (z. B. <https://bot.engels.wtf>) und melde dich als
   Owner an.
2. Gehe auf **Connections**.
3. Bei **YouTube** auf **Connect** klicken.
4. Google fragt nach Login plus Zustimmung zu den Berechtigungen. Mit dem in
   Schritt 3 eingetragenen Testnutzer anmelden und bestaetigen.
5. Nach erfolgreicher Zustimmung leitet Google zurueck. Die Karte zeigt jetzt
   **Connected** mit dem verbundenen Google-Account.

Der Bot fordert dabei diese Scopes an:
- `youtube.force-ssl` (Live-Chat lesen, senden und moderieren)
- `userinfo.profile` (welcher Google-Account verbunden wurde)

Der Flow nutzt `access_type=offline` plus `prompt=consent`, damit Google ein
Refresh-Token ausgibt. Das Token wird mit `ENGELOS_SECRETS_KEY` verschluesselt
in der DB abgelegt und automatisch erneuert.

---

## 7. Live-Chat-Quelle festlegen

Der Adapter muss wissen, welchen Live-Chat er pollen soll. Zwei Wege:

| ENV-Var | Wirkung |
|---|---|
| `ENGELOS_YOUTUBE_VIDEO_ID` | Video-ID des laufenden Live-Streams. Der Adapter loest daraus beim Verbinden automatisch die Live-Chat-ID auf. Bequemster Weg. |
| `ENGELOS_YOUTUBE_LIVE_CHAT_ID` | Direkte Live-Chat-ID, falls bereits bekannt. Ueberspringt die Aufloesung. |

Die Video-ID ist der Teil nach `v=` in der Stream-URL, z. B. bei
`https://www.youtube.com/watch?v=abcd1234XYZ` ist sie `abcd1234XYZ`.

Setze einen der beiden Werte und starte neu:

```bash
systemctl restart engelos
```

Im Log sollte der YouTube-Adapter dann verbinden und mit dem Pollen beginnen.
Ohne gesetzten Wert bleibt YouTube verbunden, aber idle (kein Chat-Polling),
und das Log weist auf die fehlende Chat-Quelle hin.

---

## Was der Daemon nutzt (ENV-Vars)

| ENV-Var | Pflicht? | Wirkung |
|---|---|---|
| `ENGELOS_YOUTUBE_CLIENT_ID` | fuer OAuth | Client-ID der Google-OAuth-App (Schritt 4). |
| `ENGELOS_YOUTUBE_CLIENT_SECRET` | fuer OAuth | Client-Secret der Google-OAuth-App. |
| `ENGELOS_YOUTUBE_REDIRECT_URL` | fuer OAuth | Exakte Callback-URL, muss zur App-Registrierung passen. |
| `ENGELOS_SECRETS_KEY` | fuer OAuth | 32-Byte base64-Schluessel fuer die Token-Verschluesselung at-rest. |
| `ENGELOS_YOUTUBE_VIDEO_ID` | fuer Polling | Video-ID des Live-Streams, woraus die Chat-ID aufgeloest wird. |
| `ENGELOS_YOUTUBE_LIVE_CHAT_ID` | alternativ | Direkte Live-Chat-ID statt Video-ID. |
| `ENGELOS_YOUTUBE_API_KEY` | optional | API-Key fuer nicht-authentifizierte Lese-Aufrufe (ohne OAuth). |

---

## Hinweise zur API-Quota

YouTube vergibt pro Projekt ein tägliches Quota (Standard 10.000 Einheiten). Der
Adapter deckelt seinen Verbrauch standardmaessig auf 10.000 Einheiten pro Tag
und respektiert das von der API vorgeschlagene Polling-Intervall (mit einem
Mindestabstand von 1 Sekunde). Bei langen Streams kann das Quota knapp werden.
Mehr Quota beantragst du im Google Cloud Projekt unter "APIs & Dienste",
"Kontingente".
