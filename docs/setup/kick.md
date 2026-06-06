# Kick Live Chat verbinden

Diese Anleitung verbindet engelOS mit dem Kick Live Chat: der Bot liest
Nachrichten ueber eingehende Webhooks und sendet/moderiert ueber die Kick-REST-API.
Die Einrichtung ist einmalig pro Betreiber.

> Kick hat keinen WebSocket fuer Chat. Events kommen als HTTP-POST an einen
> oeffentlichen Endpoint deiner Installation, und Senden/Moderieren laeuft ueber
> die REST-API. Sammle am Ende die fett markierten Werte, die kommen als
> ENV-Variablen in die Daemon-Konfiguration.

---

## 1. Kick Developer App anlegen

1. Melde dich auf <https://kick.com> mit dem Account an, als der der Bot lesen
   und schreiben soll.
2. Gehe zu **Settings**, dann **Developer** (oder direkt
   <https://kick.com/settings/developer>).
3. **Create App** / **Neue App**. Vergib einen Namen, z. B. `engelOS`.
4. Trage als **Redirect URL** exakt die Callback-URL deiner Installation ein:
   ```
   https://DEINE-DOMAIN/api/v1/auth/kick/callback
   ```
   Fuer die Standard-Installation auf `bot.engels.wtf`:
   ```
   https://bot.engels.wtf/api/v1/auth/kick/callback
   ```
   Lokal in der Entwicklung:
   ```
   http://localhost:8080/api/v1/auth/kick/callback
   ```
5. Nach dem Erstellen erscheinen **Client-ID** und **Client-Secret**, beide
   kopieren. Das Secret wird spaeter nicht mehr im Klartext angezeigt.

**Sammeln:**
- **Client-ID** -> `ENGELOS_KICK_CLIENT_ID`
- **Client-Secret** -> `ENGELOS_KICK_CLIENT_SECRET`
- Redirect-URL (oben), exakt wie eingetragen -> `ENGELOS_KICK_REDIRECT_URL`

---

## 2. Webhooks aktivieren

Kick liefert Chat-, Subscription- und Moderations-Events ueber Webhooks. Dazu
muss dein Endpoint oeffentlich erreichbar sein.

1. Im selben Developer-Bereich **Webhooks aktivieren**.
2. Trage als oeffentliche Webhook-URL deiner Installation ein:
   ```
   https://DEINE-DOMAIN/webhooks/kick
   ```
   Fuer `bot.engels.wtf`:
   ```
   https://bot.engels.wtf/webhooks/kick
   ```
   Diese Route liegt bewusst ausserhalb der angemeldeten API: Kick POSTet hier
   unauthentifiziert, und engelOS prueft die Echtheit ueber die RSA-Signatur
   jedes Requests.

**Sammeln:**
- Webhook-URL (oben) -> `ENGELOS_KICK_WEBHOOK_URL`

> Wichtig: Faellt dein Endpoint laenger als 24 Stunden aus oder antwortet er
> dauerhaft mit Fehlern, meldet Kick die App automatisch von den Events ab.
> engelOS antwortet daher immer sofort und verarbeitet Events asynchron.

---

## 3. Verbinden (OAuth-Flow mit PKCE)

Kick nutzt OAuth 2.1 mit verpflichtendem PKCE. engelOS erzeugt den
code_verifier automatisch, du musst nur durchklicken.

1. Setze zuerst die drei ENV-Variablen aus Schritt 1 (siehe Schritt 5) und
   starte den Daemon neu, damit die OAuth-Routen gemountet werden.
2. Oeffne das Dashboard (z. B. <https://bot.engels.wtf>) und melde dich als
   Owner an.
3. Gehe auf **Connections**.
4. Bei **Kick** auf **Connect** klicken.
5. Kick fragt nach Login plus Zustimmung zu den Berechtigungen. Mit dem
   Bot-Account anmelden und bestaetigen.
6. Nach erfolgreicher Zustimmung leitet Kick zurueck. Die Karte zeigt jetzt
   **Connected** mit dem verbundenen Kick-Account.

Der Bot fordert dabei diese Scopes an:
- `chat:write` (Chat senden)
- `events:subscribe` (Webhook-Event-Abos registrieren)
- `moderation:ban` (Bans und Timeouts)
- `moderation:chat_message:manage` (Nachrichten loeschen)

Das Token wird mit `ENGELOS_SECRETS_KEY` verschluesselt in der DB abgelegt und
automatisch erneuert.

---

## 4. Broadcaster-Channel festlegen

Der Adapter muss wissen, fuer welchen Kanal er Events abonniert und in welchen
Kanal er sendet. Das geschieht ueber die numerische Broadcaster-User-ID.

| ENV-Var | Wirkung |
|---|---|
| `ENGELOS_KICK_BROADCASTER_USER_ID` | Numerische Kick-User-ID des Kanals. Pflicht, sonst startet der Kick-Adapter nicht. |
| `ENGELOS_KICK_CHANNEL` | Optionales engelOS-Label fuer den Kanal (taucht in Events auf). |

Die User-ID findest du ueber die Kick-API mit deinem verbundenen Token oder im
Developer-Bereich. Setze den Wert und starte neu:

```bash
systemctl restart engelos
```

Im Log sollte der Kick-Adapter dann verbinden und nach einer kurzen
Wartezeit die Webhook-Abos registrieren.

---

## 5. ENV-Variablen setzen

Trage die Werte in die Daemon-Konfiguration ein (Standard-Deploy:
`/etc/engelos/engelos.env`):

```
ENGELOS_KICK_CLIENT_ID=...
ENGELOS_KICK_CLIENT_SECRET=...
ENGELOS_KICK_REDIRECT_URL=https://bot.engels.wtf/api/v1/auth/kick/callback
ENGELOS_KICK_WEBHOOK_URL=https://bot.engels.wtf/webhooks/kick
ENGELOS_KICK_BROADCASTER_USER_ID=12345
```

Voraussetzung wie bei jedem OAuth-Provider: `ENGELOS_SECRETS_KEY` muss gesetzt
sein (32-Byte base64, erzeugen mit `openssl rand -base64 32`), sonst wird der
verschluesselte Token-Speicher nicht aktiviert.

Danach den Daemon neu starten:

```bash
systemctl restart engelos
```

Fehlt einer der drei OAuth-Werte (Client-ID, Secret, Redirect-URL), bleiben die
OAuth-Routen lautlos ungemountet (wie bei Twitch, Spotify und YouTube). Im Log
erscheint dann `kick oauth disabled`. Sind alle drei gesetzt, steht
`GET /api/v1/auth/kick/login` bereit und der "Connect"-Button auf der
Connections-Seite funktioniert.

---

## Was der Daemon nutzt (ENV-Vars)

| ENV-Var | Pflicht? | Wirkung |
|---|---|---|
| `ENGELOS_KICK_CLIENT_ID` | fuer OAuth | Client-ID der Kick-Developer-App (Schritt 1). |
| `ENGELOS_KICK_CLIENT_SECRET` | fuer OAuth | Client-Secret der Kick-Developer-App. |
| `ENGELOS_KICK_REDIRECT_URL` | fuer OAuth | Exakte Callback-URL, muss zur App-Registrierung passen. |
| `ENGELOS_SECRETS_KEY` | fuer OAuth | 32-Byte base64-Schluessel fuer die Token-Verschluesselung at-rest. |
| `ENGELOS_KICK_BROADCASTER_USER_ID` | fuer Adapter | Numerische Kick-User-ID des Kanals; ohne sie startet der Adapter nicht. |
| `ENGELOS_KICK_WEBHOOK_URL` | fuer Adapter | Oeffentliche Webhook-URL, die bei Kick als Event-Ziel registriert wird. |
| `ENGELOS_KICK_CHANNEL` | optional | engelOS-Channel-Label, das in Events auftaucht. |
| `ENGELOS_KICK_APP_TOKEN` | optional | App-Access-Token (client_credentials) fuer die Event-Abos, falls nicht ueber den OAuth-Flow bezogen. |
| `ENGELOS_KICK_USER_TOKEN` | optional | Statischer User-Token als Fallback, falls keine Verbindung ueber den OAuth-Flow besteht. |

---

## Sicherheit und Grenzen

- **Signaturpruefung:** Jeder eingehende Webhook wird gegen Kicks RSA-Public-Key
  geprueft (RSA-SHA256 ueber `message_id.timestamp.body`). Ungueltige Signaturen
  werden mit 403 abgewiesen.
- **Replay-Schutz:** Requests aelter als 5 Minuten werden abgelehnt, und doppelte
  Message-IDs werden ueber einen Cache dedupliziert.
- **Nachrichtenlaenge:** Kick begrenzt Chat-Nachrichten auf 500 Unicode-Zeichen;
  der Adapter validiert das vor dem Senden.
- **Timeout-Dauer:** 1 bis 10080 Minuten (7 Tage). Ohne Dauer wird permanent
  gebannt.
- **Rate-Limits:** Beim Senden kann Kick mit 429 antworten; der Adapter meldet
  das als eigenen Fehler zurueck.
