# Bot-Accounts & Apps anlegen

Diese Anleitung führt durch das einmalige Anlegen der Bot-Identität für engelOS.
Bot-Handle: **`engelosbot`** (Twitch), Display-Name "engelOS".

> Reihenfolge spielt keine Rolle. Alle drei Schritte sind unabhängig.
> Sammle am Ende die fett markierten Werte — die brauchst du beim Start des
> Daemons (bzw. später im OAuth-Onboarding).

---

## 1. Twitch Bot-Account `engelosbot`

Ein eigener Twitch-Account für die Bot-Identität (getrennt von deinem
Streamer-Account `engelswtf`).

1. Ausloggen / privates Browserfenster öffnen.
2. Auf <https://www.twitch.tv/signup> registrieren:
   - **Username:** `engelosbot`
   - Eigene E-Mail (z. B. ein Alias `engelosbot@…`) + Passwort.
3. E-Mail bestätigen.
4. Optional: Profilbild/Display-Name "engelOS" setzen (Settings → Profile).

Mehr ist hier nicht nötig. Dieser Account ist die **Identität**, unter der der
Bot im Chat schreibt. Das eigentliche Schreib-Token kommt aus Schritt 2.

**Sammeln:** Login-Name `engelosbot` (= `ENGELOS_TWITCH_USERNAME`).

---

## 2. Twitch Developer App (für OAuth + Helix)

Wird gebraucht, sobald der Bot **schreiben/moderieren** soll und für den
"Login mit Twitch"-Flow. (Reines Mitlesen geht auch ohne — anonym.)

1. Auf <https://dev.twitch.tv/console> einloggen (mit dem `engelosbot`-Account
   **oder** deinem Hauptaccount — egal, die App gehört zu wem auch immer
   eingeloggt ist).
2. "Applications" → **Register Your Application**:
   - **Name:** `engelOS`
   - **OAuth Redirect URLs:** vorerst `http://localhost:8080/api/v1/auth/twitch/callback`
     (für lokale Entwicklung). Produktiv kommt später die echte Domain dazu —
     mehrere URLs sind erlaubt.
   - **Category:** `Chat Bot`
   - **Client Type:** `Confidential`
3. Erstellen → die App öffnen:
   - **Client ID** kopieren.
   - **New Secret** generieren → **Client Secret** kopieren (wird nur einmal
     angezeigt!).

**Sammeln:**
- **Client ID** → `ENGELOS_TWITCH_CLIENT_ID`
- **Client Secret** → (für OAuth-Backend; noch nicht als ENV verdrahtet)
- Redirect-URL (oben), exakt so wie im Code-Callback.

> Das eigentliche OAuth-Token (`ENGELOS_TWITCH_OAUTH`) wird später über den
> "Login mit Twitch"-Flow erzeugt. Für einen schnellen manuellen Test kannst du
> übergangsweise ein Chat-OAuth-Token über einen Token-Generator erzeugen — das
> ist aber nur ein Workaround bis das OAuth-Backend steht.

---

## 3. Discord Application + Bot

Discord hat **keinen Anonym-Modus** — ohne Token verbindet sich nichts. Der
Bot-Username muss NICHT eindeutig sein, "engelOS" ist also frei verwendbar.

1. Auf <https://discord.com/developers/applications> einloggen.
2. **New Application**:
   - **Name:** `engelOS`
3. Linke Sidebar → **Bot**:
   - **Reset Token** → **Token kopieren** (wird nur einmal angezeigt!).
   - **Privileged Gateway Intents:** **Message Content Intent** **aktivieren**
     (sonst sieht der Bot keine Nachrichteninhalte). Server Members Intent
     optional.
4. Linke Sidebar → **OAuth2**:
   - **Redirect** (für "Login mit Discord", später):
     `http://localhost:8080/api/v1/auth/discord/callback`
   - **Client ID** + **Client Secret** kopieren (für späteres OAuth).
5. **Bot in deinen Server einladen** (OAuth2 → URL Generator):
   - Scopes: `bot` + `applications.commands`
   - Bot Permissions (Minimum): `Read Messages/View Channels`, `Send Messages`,
     `Read Message History`. Mehr nach Bedarf (Manage Messages für Mod-Actions).
   - Generierte URL öffnen → Server wählen → autorisieren.

**Sammeln:**
- **Bot Token** → `ENGELOS_DISCORD_TOKEN`
- (für OAuth später) Discord **Client ID** + **Client Secret** + Redirect-URL.

---

## Was der Daemon heute schon nutzt (ENV-Vars)

Aktueller Stand der Verkabelung im Daemon (`cmd/engelos`):

| ENV-Var | Pflicht? | Wirkung |
|---|---|---|
| `ENGELOS_TWITCH_CHANNELS` | — | Komma-Liste der Channels zum Mitlesen (z. B. `engelswtf`). Leer = Twitch aus. |
| `ENGELOS_TWITCH_USERNAME` | optional | Bot-Login (`engelosbot`) für authentifizierten Modus. Leer = anonym (nur lesen). |
| `ENGELOS_TWITCH_OAUTH` | optional | Chat-/Helix-Token. Leer = anonym. |
| `ENGELOS_TWITCH_CLIENT_ID` | mit OAUTH | Client ID der Dev-App (Schritt 2). |
| `ENGELOS_DISCORD_TOKEN` | — | **Noch nicht verdrahtet** — kommt im nächsten Schritt. |

> **Discord ist im Daemon noch nicht verkabelt** — das ist der nächste
> Implementierungsschritt. Twitch-Mitlesen funktioniert bereits anonym, ganz
> ohne die obigen Accounts.

### Schnelltest (Twitch anonym, ohne Accounts)

```bash
ENGELOS_TWITCH_CHANNELS=engelswtf ./engelos
```

Der Bot verbindet sich anonym mit deinem Chat; jede Nachricht vergibt
Pity-Punkte und tickt Streaks — sichtbar über `/api/v1/stats` und die
Leaderboards.
