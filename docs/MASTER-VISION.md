# engelOS - Master Vision Plan v2

> **Codename:** engelOS
> **Tagline:** *The streaming bot that remembers you. Open source. Run it anywhere.*
> **Mission:** Der Industry-Standard für Streaming-Bots - ein Operating-Layer für Streaming-Communities, OSS-Core + Cloud-Premium, läuft auf jedem OS
> **Owner:** Luca (engelswtf)
> **License:** AGPL-3.0 (Core Daemon) + Apache-2.0 (SDK/Plugin-API/Client-Libraries) + Proprietary (Cloud-Features)
> **Erstellt:** 2026-05-27 - komplett neu geschrieben am 2026-05-28 nach Stack-Recherche
> **Vorgänger:** v1 (siehe Git-History)

---

## ⚡ EXECUTIVE SUMMARY

**Was wir bauen:** Einen All-in-One Streaming-Bot der das **Netdata-Modell** auf die Streaming-Welt überträgt: OSS-Core den jeder selbst auf Linux/macOS/Windows installieren kann (Self-Hosting wie Netdata-Agent), plus eine Cloud-Version mit Premium-Features die wir managed hosten und monetarisieren.

**Was es einzigartig macht:**
1. **OSS-Core** - kein anderer großer Streaming-Bot ist Open Source mit AGPL-Lizenz. Trust, Transparenz, keine Vendor-Lock-in-Sorge
2. **Cross-OS** - läuft auf allen drei Major-OS als Single-Binary (Cloud auf Linux Server, Self-Hoster auf Pi/NAS/Desktop ihrer Wahl)
3. **Vier UI-Modi** - Headless-Daemon, TUI für SSH-Power-User, Native-GUI für Desktop-Streamer, Web-UI für Browser-Access
4. **Memory-Layer statt Command-Router** - der Bot *kennt* Viewer/Streamer über Zeit und Plattformen
5. **First-Mover-Features** - AI Co-Host, Cross-Streamer-Loyalty, Pity-System, Stream-Wrapped, Moment-Alerts

**Markt-Realität:**
- 14M+ potenzielle Bot-User weltweit
- $300-500M TAM, +15%/Jahr Wachstum
- Top-Bots (Nightbot 2012, MEE6 2015, StreamElements 2017) sind frozen in time
- **Kein OSS-Streaming-Bot existiert** mit moderner Architektur + Cloud-Companion (Lücke!)
- Indie-Dev kann gewinnen - Markt zu "boring" für VCs, zu spezialisiert für Big-Tech

**Stack (committed):**
- **Core:** Go 1.24+ (Single-Binary, CGO_ENABLED=0, Cross-Compile von Linux-CI)
- **Native GUI:** Wails v2 (Go-Backend + Webview-Frontend, ein Build pro OS)
- **TUI:** Bubble Tea (Charmbracelet)
- **Frontend:** Svelte / SvelteKit (shared zwischen Wails-GUI und Cloud-Web)
- **DB:** SQLite (lokal, `modernc.org/sqlite` pure-Go) + PostgreSQL (Cloud, `jackc/pgx`)
- **AI-Sidecars:** Python (FastAPI) - Cloud-only oder via BYOK self-hostbar

**Pfad:**
- Phase 0 (Jetzt-Juni 2026): EngelGuard fixen, engelOS-Skelett aufsetzen, OSS von Tag 1
- Phase 1 (Juni-Dez 2026): Core + 4 Killer-Features + Solo/Friends-Dogfooding
- Phase 2 (Jan-Juni 2027): Open Beta, erste 100 User, Cloud-Version live
- Phase 3 (Juli 2027-Juni 2028): 5.000 User, Freemium aktivieren, profitabel
- Phase 4 (Juli 2028-Juni 2030): Network Effects, 50.000 User, "Industry-Standard"
- Phase 5 (2030+): Strategic Decision (Lifestyle / VC / Exit)

---

## 🎯 VISION + POSITIONING

### Tagline-Optionen
- ✅ **Primary:** *"The streaming bot that remembers you. Open source. Run it anywhere."*
- Alternativ: *"Built for streamers who care about their community."*
- Alternativ: *"Every streaming feature you need. One bot. Free and open."*

### Was engelOS NICHT ist
- ❌ Nicht "noch ein Command-Router" (Nightbot, Moobot)
- ❌ Nicht "Overlay-Studio mit Bot dran" (StreamElements, Streamlabs)
- ❌ Nicht "Twitch-only" (Moobot, Wizebot)
- ❌ Nicht "Stream-Deck-Software" (Streamer.bot, SAMMI)
- ❌ Nicht "Cloud-only-SaaS mit Lock-in" (alle großen Anbieter)

### Was engelOS IST
- ✅ **Cross-Platform-Software** die auf Linux/macOS/Windows läuft (deine Wahl)
- ✅ **OSS-Core** unter AGPL-3.0 - du kannst es selbst hosten, forken, auditieren
- ✅ **Cloud-Premium-Option** für die die nicht selbst hosten wollen
- ✅ **Community-Layer** über Twitch/YouTube/Kick/Discord hinwegläuft
- ✅ **Memory-System** das Viewer und ihre Geschichte über Plattformen + Zeit kennt
- ✅ **AI-First** ohne lokale GPU-Anforderung (Cloud-only AI oder BYOK)
- ✅ **Network-Effect-Platform** - wert wird größer mit mehr Streamern
- ✅ **Multi-UI** - Daemon, TUI, Native-GUI, Web - je nachdem wie der User es will

### Das Netdata-Modell - unsere strategische Inspiration

| Netdata | engelOS |
|---|---|
| OSS-Agent installiert in 60s, läuft überall | OSS-Core installiert in 60s, läuft überall |
| Sofort beeindruckende UI auch ohne Cloud | Sofort funktionierender Bot auch ohne Cloud |
| Cloud-Version: Multi-Node-View, Alerts | Cloud-Version: AI-Features, Cross-Stream-Analytics |
| 70k GitHub-Stars treiben organische Adoption | Ziel: gleicher Mechanismus für Streaming-Bot-Welt |
| "Industry-Standard" durch ubiquität, nicht Marketing | Gleiche Strategie |

---

## 🏗️ ARCHITEKTUR - Die 8 unverhandelbaren Prinzipien

### 1. **OSS-Core + Cloud-Premium (Open Core Modell)**

```
┌──────────────────────────────────────────────────────┐
│  engelOS Core (AGPL-3.0, GitHub Public, Day 1)      │
│  ─────────────────────────────────────────────       │
│  - Vollständig funktionierender Bot                  │
│  - Alle Platform-Adapter (Twitch/YT/Kick/Discord)    │
│  - AutoMod, Commands, Loyalty, Streaks, Pity         │
│  - Integrations-Framework (Spotify, Game-APIs)       │
│  - Web-UI (lokal embedded) + TUI + Native-GUI        │
│  - Event-Sourcing, Multi-User-Auth, RBAC             │
│  - SQLite + PostgreSQL                               │
└──────────────────────────────────────────────────────┘
                       ↕ optional ↕
┌──────────────────────────────────────────────────────┐
│  engelOS Cloud (proprietary, app.engelos.com)       │
│  ─────────────────────────────────────────────       │
│  - Managed Hosting (kein eigener Server nötig)       │
│  - AI Auto-Clipper (Cloud-only, wir tragen Kosten)   │
│  - AI Co-Host TTS (Cloud-only)                       │
│  - Cross-Stream-Analytics (zentrale Daten)           │
│  - Team-Seats (Multi-User-Billing)                   │
│  - Backup + Disaster Recovery                        │
│  - SLA, Premium-Support                              │
└──────────────────────────────────────────────────────┘
```

**Cloud-Aufteilung (Hybrid mit BYOK):**

| Feature | Self-Hosted Free | Self-Hosted + BYOK | Cloud Pro |
|---|---|---|---|
| Twitch/Discord/YT/Kick-Integration | ✅ | ✅ | ✅ |
| AutoMod, Commands, Loyalty | ✅ | ✅ | ✅ |
| Streaks, Pity, Live-Ops | ✅ | ✅ | ✅ |
| Stream-Wrapped-Cards | ✅ | ✅ | ✅ |
| Spotify/Game-Integrations | ✅ | ✅ | ✅ |
| Web-UI lokal | ✅ | ✅ | ✅ |
| AI Translator | ❌ | ✅ (eigener API-Key) | ✅ (inklusive) |
| Context-Aware AI-Mod | ❌ | ✅ (eigener API-Key) | ✅ (inklusive) |
| **AI Auto-Clipper** | ❌ | ✅ (eigener API-Key, lokale Excitement-Detection auf CPU) | ✅ (inklusive, optimized models) |
| **AI Co-Host TTS** | ❌ | ❌ (Voice-Clone-Setup zu spezialisiert) | ✅ (Cloud-only) |
| Cross-Stream-Analytics | ❌ | ❌ | ✅ (Cloud-only) |
| Team-Seats | begrenzt 3 User | begrenzt 3 User | unbegrenzt |
| Managed Hosting | – | – | ✅ |
| Backup + DR | manuell | manuell | ✅ automatisch |

**Konzept:** AI Co-Host bleibt Cloud-only, weil Voice-Clone-Setup hochspezialisiert ist und ein hochwertiges WebRTC-Audio-Routing-Setup braucht. Alle anderen AI-Features (Translator, AI-Mod, Auto-Clipper) erlauben BYOK self-hostbar - der User bringt eigenen API-Key. Cloud-Version bietet dieselben Features ohne Setup-Aufwand + optimierte Pipelines.

**Warum nicht alles BYOK:** Wenn alle "Wow-Features" self-hostbar wären, fehlt die Monetarisierungs-Brücke. Co-Host ist genau die richtige Balance: zu komplex für Mainstream-Self-Hoster (gut für Cloud-Conversion), aber einzelnes Premium-Feature (keine "Cloud-only-Lock-in"-Wahrnehmung).

### 2. **Cross-OS-Distribution (Single-Binary, alle 3 OS)**

```
Build-Pipeline (GitHub Actions + GoReleaser)
    │
    ├── Linux
    │   ├── .deb (Debian/Ubuntu)
    │   ├── .rpm (Fedora/RHEL)
    │   ├── .pkg.tar.zst (Arch)
    │   ├── Docker (multi-arch: amd64, arm64, armv7)
    │   ├── Raspberry Pi Image (Pi-hole-Style, flashable .img)
    │   └── curl install.sh | bash (Universal-Installer)
    │
    ├── macOS
    │   ├── .dmg (Drag-to-Install)
    │   ├── Homebrew Formula (brew install engelos)
    │   ├── Apple Silicon native (arm64)
    │   └── Intel native (amd64)
    │
    └── Windows
        ├── .msi-Installer (signed)
        ├── WinGet (winget install engelos)
        ├── Scoop / Chocolatey (Community-Packages)
        └── Optional als Windows-Service registrierbar
```

**OS-Rollen (gleichwertig, unterschiedliche Use-Cases):**

| OS | Primary Use-Case | UI-Modi (Ziel-Zustand ab Phase 2) |
|---|---|---|
| **Linux Server** (Cloud + Self-Hoster mit Homelab/Pi) | Always-on Daemon | Headless + TUI + Web |
| **macOS Desktop** (Mac-Streamer privat) | Local Companion | Native-GUI + Web/PWA |
| **Windows Desktop** (Mainstream-Streamer) | Local Companion | Native-GUI + Web/PWA |

Alles funktioniert überall - nur die *bevorzugte* UI-Form unterscheidet sich.

**Phase-1-Realität (Dezember 2026):** Native-GUI (Wails) ist noch nicht released - Code-Signing-Budget kommt erst mit Cloud-Revenue (Phase 2). Phase 1 hat **PWA (Progressive Web App)** auf allen 3 OS: User öffnet `http://localhost:8080` im Browser, klickt "Install as App", kriegt Desktop-Icon ohne Code-Signing-Pain. Phase 2 dann echte Native-Apps.

### 3. **Vier UI-Modi für vier Use-Cases**

```
                    ┌──────────────────────────────┐
                    │  engelOS Core Daemon (Go)    │
                    │  - Bot Logic                 │
                    │  - Adapters                  │
                    │  - SQLite/Postgres           │
                    │  - HTTP/WS API auf :8080     │
                    └──────────────────────────────┘
                              ▲    ▲    ▲    ▲
              ┌───────────────┘    │    │    └───────────────┐
              │                    │    │                    │
   ┌──────────┴──────┐  ┌──────────┴────┴────┐  ┌────────────┴──────┐
   │ A) Headless     │  │ B) TUI            │  │ C) Native GUI     │
   │    Daemon       │  │    (Bubble Tea)   │  │    (Wails v2)     │
   │                 │  │                   │  │                   │
   │ - kein UI       │  │ - im Terminal     │  │ - .dmg / .msi /   │
   │ - systemd-Unit  │  │ - SSH-Friendly    │  │   .deb            │
   │ - logs only     │  │ - Live-Charts     │  │ - Webview-basiert │
   │                 │  │ - Tabs            │  │ - Embedded Svelte │
   │ FOR: Server     │  │ FOR: Power-User   │  │ FOR: Desktop      │
   └─────────────────┘  └───────────────────┘  └───────────────────┘
                                                          ▲
                                                          │ uses
                                                          │ same
                                                          │ frontend
                                                          ▼
                                            ┌────────────────────────┐
                                            │ D) Web Dashboard       │
                                            │    (Svelte / SvelteKit)│
                                            │                        │
                                            │  D1) Local Variant     │
                                            │    - embedded in       │
                                            │      Daemon-Binary     │
                                            │    - localhost:8080    │
                                            │                        │
                                            │  D2) Cloud Variant     │
                                            │    - app.engelos.com   │
                                            │    - + Billing/Teams   │
                                            └────────────────────────┘
```

**Frontend-Code-Sharing-Strategie:**

```
web/                         # ein Svelte-Monorepo
├── packages/
│   ├── shared/              # 80-90% des Codes - UI-Komponenten
│   │   ├── ChatViewer/
│   │   ├── CommandEditor/
│   │   ├── AutoModRules/
│   │   ├── IntegrationSetup/
│   │   ├── OverlayConfig/
│   │   ├── AuthLogin/       # auch lokal!
│   │   ├── UserManagement/  # auch lokal!
│   │   ├── ApiKeysManager/  # auch lokal!
│   │   └── ...
│   ├── local/               # 10% - lokal-spezifisch
│   │   ├── SetupWizard/
│   │   ├── UpgradeToCloud/  # Conversion-CTA
│   │   └── DebugConsole/
│   └── cloud/               # 10% - cloud-spezifisch
│       ├── Billing/
│       ├── TeamSeats/
│       ├── AdminPanel/      # für uns
│       ├── CrossStreamAnalytics/
│       └── OnboardingWizard/
├── apps/
│   ├── wails-gui/           # Wails-Wrapper für Native-App
│   ├── local-web/           # Build der lokal in Go embedded wird
│   └── cloud-web/           # SvelteKit-App für app.engelos.com
└── ...
```

### 4. **Multi-User-Auth + RBAC (auch lokal!)**

**Begründung:** Auch Self-Hoster brauchen Mods, Remote-Access, geteilte Instanzen. Auth-by-Default ist Pflicht für ein professionelles Produkt.

**Rollen-Modell (lokal + cloud identisch):**

| Rolle | Permissions |
|---|---|
| **Owner** | Alles. Genau einer pro Instanz/Account. |
| **Admin** | Settings, Integrations, Mods einladen - nicht: Owner-Settings, Billing |
| **Mod** | Commands, AutoMod, Logs, Chat-Actions - nicht: Credentials, Tokens, API-Keys |
| **Viewer** | Nur Read-Only (Dashboards, Stats, Logs) |
| **API-Token (Service-Account)** | Scope-beschränkt, revocable, audit-loggt - für Maschinen-Use-Cases |

**Auth-Mechanismen:**

| Variante | Lokal | Cloud |
|---|---|---|
| Username + Passwort (mit Argon2id) | ✅ | – |
| OAuth: Twitch / Discord / Google | ✅ optional | ✅ primär |
| 2FA via TOTP | ✅ optional | ✅ optional |
| SSO/SAML | ❌ | ✅ Enterprise-Plan |
| Magic-Link / Email-Invite | ✅ wenn SMTP konfiguriert | ✅ |

**API-Keys (Owner-/Admin-Feature):**
- Generierbar im Dashboard mit benannten Scopes (`commands:read`, `chat:write`, `automod:write`, etc.)
- Optional Ablaufdatum, Rate-Limit, IP-Whitelist
- Vollständiges Audit-Log: "API-Key 'streamdeck-mod' hat um 14:32 Command !so geupdatet"
- Sofort revocable
- Bcrypt-gehashed in DB, Plain-Text nur einmal beim Erstellen angezeigt

**Sicherheits-Defaults:**
- Erster Start = Setup-Wizard zwingt Owner-Account-Erstellung
- Bot lauscht standardmäßig nur auf `127.0.0.1:8080` (nicht `0.0.0.0`)
- Setup-Wizard fragt explizit ob LAN/Internet-Access gewünscht → erst dann `0.0.0.0`
- Bei `0.0.0.0`-Binding wird 2FA-Pflicht empfohlen
- CSP-Header, CSRF-Tokens, Rate-Limiting by default

### 4a. **Bot-Identität & "Login mit Twitch" (Architektur-Entscheidung)**

**Entscheidung:** engelOS betreibt **gebrandete Bot-Accounts** (`engelOS_bot` auf
Twitch, eine gebrandete Discord-Application) zentral auf der eigenen Infra. Ein
Streamer onboardet per **"Login mit Twitch"**, das in einem Flow zwei Dinge tut:
SSO-Login ins Dashboard **und** Autorisierung des Bots für den eigenen Channel.
Kein manuelles Token-Kopieren. Das spiegelt das Onboarding von
Nightbot/StreamElements.

**Warum gebrandet + zentral (statt jeder ein eigener Bot):**
- Ein einzelner gebrandeter Account, der in *jedem* Channel als `engelOS_bot`
  auftritt, ist technisch nur möglich, wenn **wir** sein OAuth-Token halten.
  Das Token an Self-Hoster zu verteilen wäre ein Credential-GAU → ein geteilter
  Marken-Bot ist zwingend ein **zentral gehostetes** Feature.
- Für die Owner-Instanz (engels.wtf) und frühe User ist das der Zero-Setup-Pfad:
  Account anlegen, "Login mit Twitch", fertig.

**Self-Hosting bleibt voll funktional (kein Lock-in):** Der OSS-Daemon nutzt
denselben Twitch-OAuth-Flow, aber mit der **eigenen** Twitch-App-Registrierung
des Self-Hosters (eigene `client_id`/`secret` in Settings). Sein Bot läuft unter
seinem eigenen Account, sein Token bleibt bei ihm. Bis das OAuth-Onboarding
steht, funktioniert weiterhin der BYOK-Pfad über Env-Vars
(`ENGELOS_TWITCH_OAUTH`, `ENGELOS_DISCORD_TOKEN`).

| Deployment | Bot-Identität | Onboarding |
|---|---|---|
| **Cloud / Owner-Infra** | gebrandeter `engelOS_bot` (wir hosten Token) | "Login mit Twitch" → SSO + Bot-Auth in einem Klick |
| **Self-Hosted** | eigener Account/Bot des Hosters | gleicher OAuth-Flow mit eigener App-Registrierung; Fallback BYOK via Env-Var |

**Twitch-Lesezugriff** braucht keinen Account (anonymer IRC, bereits
implementiert). Account/OAuth ist nur für **Schreiben + Mod-Actions** nötig.

**Implikationen fürs Auth-Backend (Folge-Arbeit, noch nicht gebaut):**
- Eine registrierte Twitch-App (`client_id`/`secret`) für die Cloud-Instanz.
- OAuth-Callback-Handler + verschlüsselte Token-Speicherung im auth-Store, inkl.
  Refresh-Token-Rotation.
- Discord: gebrandete Application + Invite-Flow (`bot`+`applications.commands`
  Scopes); Self-Hoster registrieren ihre eigene Application.
- Bot-Adapter konsumieren das gespeicherte Token statt der Env-Var, sobald der
  OAuth-Pfad live ist.

### 5. **Event-Sourcing von Tag 1**

```
┌─────────────────────────────────────────────────────┐
│  Immutable Event Log (PostgreSQL Append-Only)       │
│  ─────────────────────────────────────              │
│  - channel.message.created                          │
│  - channel.user.subscribed                          │
│  - channel.moderation.timeout                       │
│  - integration.spotify.track_changed                │
│  - bot.command.executed                             │
│  - viewer.streak.updated                            │
│  (Plattform-agnostisches internes Format)           │
└─────────────────────────────────────────────────────┘
            │
            ├──► Read-Model: User-Profiles (current state)
            ├──► Read-Model: Loyalty-Ledger
            ├──► Read-Model: Stream-Analytics
            ├──► Read-Model: Wrapped-Cards (yearly aggregate)
            └──► Replay-Engine (für Debugging, AI-Training)
```

**Warum:** Ohne Event-Sourcing keine Stream-Wrapped-Cards (brauchen Historie), kein AI-Training (brauchen Replays), keine Time-Travel-Debugging, keine Cross-Stream-Analytics.

### 6. **Platform-Adapter-Layer (Plug-In-Architektur)**

```go
// internal/adapters/platform.go
type Platform interface {
    Connect(ctx context.Context) error
    Subscribe(events chan<- Event) error
    Send(action Action) error
    Disconnect() error
}

// Implementations:
// - internal/adapters/twitch/    (IRC + Helix + EventSub)
// - internal/adapters/discord/   (Gateway via discordgo)
// - internal/adapters/youtube/   (Live Chat via google-api-go-client)
// - internal/adapters/kick/      (custom WebSocket)
// - internal/adapters/tiktok/    (Phase 4, falls Live-API stable)
// - internal/adapters/x/         (Phase 4, falls Live-Audio scales)
```

**Warum:** Twitch ändert APIs alle 2 Jahre (PubSub→EventSub, IRC→Helix). Wenn die Bot-Logik direkt gegen Twitch-Datenformat codet, muss bei jeder API-Änderung der ganze Bot umgebaut werden. Mit Adapter-Layer: ein Adapter-Rewrite, Rest unverändert.

### 7. **Dual-License-Strategie (AGPL Core + Apache SDK)**

**Problem:** AGPL-3.0 schützt vor Big-Cloud-Klau, aber blockiert Beiträge von Firmen (Google/Meta/Apple verbieten AGPL per Policy). Stream Deck (Elgato), OBS-Forks, Mod-Tools können keine offizielle engelOS-Integration bauen wenn unser **Plugin-API** AGPL ist.

**Lösung:** Drei-Schichten-License-Strategie wie Grafana es macht:

| Komponente | License | Begründung |
|---|---|---|
| **Core Daemon** (engelOS-Bot, Adapters, Event-Sourcing) | **AGPL-3.0** | Schutz vor "AWS hostet's und kassiert"-Klau |
| **SDK / Plugin-API / Client-Libraries** (Go, TypeScript, Python) | **Apache 2.0** | Firmen können Integrationen bauen ohne AGPL-Compliance |
| **Integrations-Plugins** (Spotify, Game-APIs, Music) im engelOS-Repo | **Apache 2.0** | Community-Beiträge nicht behindern |
| **Cloud-Features** (Auto-Clipper-Service, Co-Host-Service, Billing) | **Proprietary** | engelos.com-exklusiv |
| **Documentation, Themes, Overlays** | **MIT** oder **CC-BY** | Maximale Wiederverwendung |

**Repo-Aufteilung:**
- `engelos/engelos` → AGPL-3.0 (Core Daemon)
- `engelos/sdk-go` → Apache 2.0 (Go-Library für Integrations)
- `engelos/sdk-ts` → Apache 2.0 (TypeScript-Library für Web-Extensions)
- `engelos/plugins-official` → Apache 2.0 (Spotify, Game-APIs, etc.)
- `engelos/cloud` → privat, proprietary

**Bonus:** Diese Trennung zwingt uns zu sauberem API-Design (Plugin-API muss als Public-Library funktionieren), was die Software-Qualität verbessert.

### 8. **Integration-Framework (Plugin-System für externe APIs)**

```
internal/integrations/
├── core.go                       # Plugin-Interface, Registry
├── spotify/                      # Spotify Now-Playing
│   ├── adapter.go
│   ├── oauth.go
│   └── manifest.json             # Display-Name, Required-Scopes, Icon
├── applemusic/
├── youtubemusic/
├── lastfm/
├── riot/                         # LoL, Valorant Match-Data
├── steam/                        # Currently-Playing
├── strava/                       # Fitness-Streamer
└── ...

User-Flow im Dashboard:
1. "Integrations" → Browse-Library mit verfügbaren Plugins
2. Click "Connect Spotify" → OAuth-Flow → fertig
3. Spotify-Daten in Event-Stream verfügbar → andere Module nutzen sie
4. z.B. Overlay-System: "Show Now-Playing as Browser-Source"
```

**Warum:** Spotify ist nur eine von vielen. Game-APIs, Music-Services, Fitness, IoT - alle müssen integrierbar sein **ohne dass wir jede einzeln in den Core hardcoden**. Wie Home Assistant Integrations.

---

## 🏆 DIE 16 KILLER-FEATURES + Overlay-System

### TIER A - Build First (Foundation + Quick Wins)

#### 1. **AI Auto-Clipper** (BYOK + Cloud) 🔥🔥🔥
- Erkennt Excitement-Peaks (Chat-Velocity, Sub-Spikes, Emote-Explosionen)
- Auto-Create Clip via Twitch Clips API
- Auto-Title via Claude
- Auto-Post auf Discord + Twitter optional
- **Competitor-Status:** Streamlabs hat manuellen Clip-Button (verified 2026), kein Auto-Detect. StreamElements: kein Auto-Clipper. Nightbot: kein Clipper. Fossabot: kein Clipper.
- **Effort:** ~4 Wochen Solo-Dev (Excitement-Detection nicht trivial)
- **Tech:** Python-Sidecar für Excitement-Score (Cloud) oder lokaler CPU-Threshold (BYOK)

#### 2. **Real-Time AI-Translator (Multi-Language Chat)** (BYOK + Cloud)
- Spanischer Viewer schreibt → Bot übersetzt zu Englisch in-chat oder Discord-Thread
- Kick wächst international, YouTube Live ist non-English-dominant
- Claude Haiku ist schneller+billiger als DeepL
- **Competitor-Status:** Twitch hat Beta-Translation (limitierte Sprachen, AutoMod-Integration unklar). StreamElements: keine Translation. Nightbot: keine Translation. Wir bieten: Bot-side mit Streamer-customizable Languages + Discord-Thread-Routing.
- **Effort:** ~3 Wochen Solo-Dev
- **Tech:** Translation als Stateless-Service, BYOK = User-API-Key in Settings

#### 3. **Pity-System (Gacha-Mechanik)** (OSS Core)
- Viewer akkumulieren "Pity Points" beim Schauen
- Nach X Stunden ohne Win → garantierte Belohnung
- Soft-Pity bei 70% (sichtbar: "Du bist nah dran!")
- Eliminiert "ich gewinne nie was"-Frustration
- **Competitor-Status:** Kein Streaming-Bot hat Pity-Mechanik (verified durch Competitor-Inventur). Gacha-Communities (Genshin/Honkai) haben es perfektioniert - wir adaptieren.
- **Effort:** ~2 Wochen Solo-Dev
- **Tech:** Pure Go, Event-Log + Read-Model

#### 4. **Streak-System mit Streak-Freeze (Duolingo-Mechanik)** (OSS Core)
- Watch-Streaks (consecutive days)
- Streak-Freeze kostet Loyalty-Points
- 7/30/100/365-Tage-Milestones
- Streak-Leaderboard in Discord
- **Competitor-Status:** MEE6 hat Daily-Rewards aber keine Streaks. Tatsu hat Streaks aber kein Freeze-System. Kein Bot hat Streak-Wager.
- **Retention-Evidence:** Duolingo (+89% DAU mit Streaks), Snapchat (Snap-Streaks), GitHub (Contribution-Graph) - alle dokumentiert
- **Effort:** ~3 Wochen Solo-Dev
- **Tech:** Pure Go, Cron-Job für Daily-Tick

### TIER B - Build Next (Engagement Magic)

#### 5. **AI Co-Host (TTS in Streamer-Stimme)** (Cloud-only) 🔥🔥🔥
- ElevenLabs Professional Voice Clone → klingt wie du
- Liest Chat-Highlights, reagiert auf @bot-Mentions
- Claude Haiku 4.5 für Antworten
- **<300ms Latenz** mit WebSocket-Streaming
- **Competitor-Status:** Streamlabs hat lokales AI Co-Host braucht RTX 3090+ (Hardware-Anforderung disqualifiziert 90% der Streamer). Kein Cloud-basiertes Co-Host mit Voice-Clone existiert.
- **Effort:** ~6-10 Wochen Solo-Dev (Audio-Routing-Komplexität: Virtual Audio Cable Setup auf Win/Mac, OBS-Integration, Latency-Tuning)
- **Tech:** Python-Sidecar mit ElevenLabs-WebSocket, Audio-Stream zurück an OBS via Virtual-Audio-Cable oder WebRTC

#### 6. **Context-Aware AI-Moderator** (BYOK + Cloud)
- LLM liest letzte 50 Messages als Context
- Versteht "das ist Krebs" als Gaming-Slang vs. Hass
- Lernt Community-Norms über Zeit
- Twitch's eigener AutoMod ist keyword-basiert (verified - Twitch AutoMod Docs)
- **Competitor-Status:** StreamElements + Fossabot haben keyword-AutoMod. Nightbot: keyword-AutoMod. Kein Bot bietet Context-Window-AI-Mod.
- **Effort:** ~6 Wochen Solo-Dev
- **Tech:** Claude Haiku 4.5 mit Rolling-Window-Prompt, BYOK in Settings

#### 7. **Spotify-Wrapped-Style "Stream Wrapped"** (OSS Core) 🌟
- Jährliche + monatliche shareable Cards pro Viewer:
  - "Du hast 47h von engelswtf geschaut"
  - "Top 3% Viewer"
  - "Most-used Emote: PogChamp (847x)"
- Streamer-Wrapped mit Year-End-Recap
- **Viral-Potential:** jeder geteilte Card = freie Werbung
- **Lock-in:** nach 6 Monaten History wechselt niemand mehr
- **Competitor-Status:** Spotify Wrapped ist annualized als Marketing-Goldstandard etabliert. Kein Streaming-Bot bietet vergleichbares Wrapped-Format (verified Nightbot, StreamElements, Streamlabs, Fossabot, Moobot).
- **Effort:** ~4-6 Wochen Solo-Dev (Headless-Browser-Rendering, schöne shareable Cards)
- **Tech:** Aggregation aus Event-Log, SVG/PNG-Generierung via Headless-Browser oder Go-Image-Lib

#### 8. **Live-Ops-Calendar** (OSS Core)
- Bot managed Event-Calendar:
  - "Double Points Weekend"
  - "Raid Week"
  - "Season 3 startet in 4 Tagen"
- 90-Tage-Seasons mit Themes
- Limited-Time-Challenges
- Auto-Post in Discord jeden Montag
- **Competitor-Status:** Mobile-Gaming (Genshin, Hoyo) hat Live-Ops perfektioniert. Streaming-Bots bieten kein vergleichbares Season-Framework.
- **Effort:** ~3 Wochen Solo-Dev
- **Tech:** Pure Go, Scheduler + Template-Engine

#### 9. **BeReal-Style "Moment Alerts"** (OSS Core) 📸
- Zufälliges (oder Streamer-triggered) Discord-Push:
  - "🚨 MOMENT: [Streamer] hat gerade was Wahnsinns gemacht - 5 Min für den Clip!"
- Viewer die in 5 Min reagieren → "I Was There"-Badge
- Moment-Archive in Discord-Channel
- Moment-Rarity-Tiers (Common/Rare/Legendary)
- **Competitor-Status:** Novel-Konzept aus BeReal-Mechanik adaptiert. Kein bekannter Streaming-Bot bietet vergleichbares Moment-System.
- **Effort:** ~3 Wochen Solo-Dev

### TIER C - The Moat (Network Effects, Phase 3+)

#### 10. **Unified Chat (Twitch + YouTube + Kick + Discord)** (OSS Core)
- Ein WebSocket aggregiert ALLE Plattform-Chats
- Mod-Aktionen sync über Platforms
- Twitch-Ban → Kick-Ban (mit Consent)
- Eine Identity, eine Bot-Personality, ein Command-Set
- **DER Moat** - Switch-Cost ist enorm wenn drauf
- **Effort:** High (4-6 Wochen)
- **Tech:** Adapter-Layer + Unified-Event-Bus

#### 11. **Cross-Streamer Loyalty-Network** (Cloud-only, EXPERIMENTELL) 💎⚠️
- Viewer-Points bei Streamer A funktionieren bei Streamer B (mit explicit Consent)
- Zweitseitiger Effect: beide Streamer profitieren (NUR wenn beweisbar)
- "Loyalty-Passport" für Viewer
- **Risiko-Caveat:** Streamer könnten Feature aktiv ablehnen ("ich will MEINE Viewer nicht teilen"). Phase 4 testet ob Mutual-Benefit beweisbar. Falls <50 Streamer nach 6 Monaten teilnehmen → Feature deprecaten.
- **DSGVO-Komplexität:** Joint-Controller Art. 26, expliziter Consent pro Viewer pro Streamer-Pair
- **Effort:** High (~12 Wochen Solo-Dev, Phase 4)
- **Warum Cloud-only:** braucht zentrale Datenbank über alle Streamer hinweg

#### 12. **VIP-Host-Bot (Casino-Mechanik)** (OSS Core)
- Bot tracked Whale-Viewer (Top 1% Watch-Time/Subs/Bits)
- Personalisierte Greetings: "Welcome back [Name], du bist auf Tag 47-Streak"
- VIP-Concierge-Commands (`!request`, `!memory`, `!anniversary`)
- Proactive DM wenn Streamer live
- Streamer-Dashboard mit Top-20-VIPs
- **Competitor-Status:** Loyalty-Tracking ist überall, aber Casino-Style VIP-Concierge-Behandlung mit personalisierter Memory ist novel.
- **Lock-in-Hypothese:** VIPs mit 2+ Jahren History wechseln ungern (zu testen Phase 3-4)

### TIER D - Platform-Layer (Trigger-Infrastruktur + Ökosystem)

> Diese Features wurden nach der initialen 12er-Liste ergänzt (Brainstorming 2026-05). Sie bilden zusammen eine **Plattform-Schicht**: Channel-Points ist der *Trigger*, Marketplace/AI-Voice/Sticker sind die *getriggerten Aktionen*. Strategisch sind sie wichtiger als einzelne Gimmick-Features, weil sie eine offene Erweiterbarkeit schaffen, die kein Konkurrent hat.

#### 13. **Twitch Channel-Points-Integration (Trigger-Engine)** (OSS Core) 🔥🔥🔥
- Twitch hat eine **native Punkte-Währung** (Kanalpunkte/Channel Points) die jeder Viewer beim Zuschauen sammelt - viel besser akzeptiert und interaktiver als bot-eigene "Loyalty-Points", weil direkt im Twitch-UI unter dem Stream sichtbar.
- engelOS erstellt/verwaltet **Custom Rewards** und reagiert live auf Einlösungen (Redemptions): `Reward eingelöst → Bot-Aktion auslösen` (Command ausführen, Counter erhöhen, Chat-/Discord-Post, Overlay-Animation, später: Marketplace-Skript ausführen).
- **Refund-Loop:** Bei ungültiger Eingabe automatisch `CANCELED` → Viewer kriegt Punkte zurück. Bei Erfolg `FULFILLED`.
- **Konfiguration über Dashboard, NICHT über Chat** - Reward↔Aktion-Mapping wird im Web-UI gebunden (Datums-/Dropdown-Auswahl statt Chat-Tipperei).
- **Competitor-Status:** Firebot (OSS) hat das am besten gelöst (Reward → Effect-List). StreamElements/Nightbot/Streamlabs bieten Channel-Point-Trigger nur rudimentär. Unser Vorteil: Channel-Points als *universelle Trigger-Schicht* für ALLE Bot-Features + den Marketplace.
- **Effort:** ~3-4 Wochen Solo-Dev (EventSub-WebSocket-Client + Reward-CRUD + Trigger-Mapping-Store + Dashboard-UI).
- **Tech:** `nicklaw5/helix/v2` (hat bereits alle Reward-CRUD- + Redemption-Methoden) + selbstgebauter EventSub-WebSocket-Client gegen `wss://eventsub.wss.twitch.tv/ws` (via `coder/websocket`, schon im Projekt). Scope `channel:manage:redemptions`, Broadcaster-User-Token.
- **🔴 Harte Einschränkung:** Custom Rewards erfordern **Twitch Affiliate oder Partner**. Non-Affiliates bekommen 403 → Feature muss hinter einem `broadcaster_type`-Check gegated werden mit klarer Fehlermeldung.
- **🔴 Plattform-Constraint:** Der Bot kann nur Rewards `FULFILLED`/`CANCELED` setzen, die seine **eigene OAuth-App erstellt** hat (`manageable`-Flag). Fremde Rewards kann er nur *beobachten*, nicht refunden.

#### 14. **Addon-/Skript-Marketplace** (OSS Core + kuratierter Store) 🔥🔥🔥💎
- Community-Skripte/Addons die auf Trigger (Channel-Points, Events, Commands) reagieren - z.B. **Trolling-Effekte**: Bildschirm kurz schwarz, Spiel minimieren, Maus zittern, Sound abspielen, Filter über Webcam, „Fake-Bluescreen", etc.
- **Kuratierter Marketplace:** Skripte werden einmal sauber gebaut, von einem **Review-Team / automatisiert analysiert** (kein Schadcode, keine Viren, vernünftige Qualität), **signiert**, und sind dann 1-Klick installierbar.
- **🔴🔒 SICHERHEIT = oberste Priorität (größte Angriffsfläche im ganzen Produkt):**
  - **Capability-/Permission-Sandbox:** Jedes Addon deklariert explizit was es darf (z.B. „darf Chat schreiben", „darf OS-Fenster steuern", „darf Datei X lesen"). Der User bestätigt diese Permissions bei Installation (wie Android-App-Permissions).
  - **Code-Signing + Review-Gate:** Nur signierte, vom Team geprüfte Addons im offiziellen Store. Unsignierte nur mit lautem „auf eigene Gefahr"-Warnhinweis.
  - **Isolation:** Addons laufen NICHT mit den Bot-Rechten/Tokens. Kein Addon darf an OAuth-Tokens, Secrets oder die DB direkt. Sie kommunizieren über eine schmale, abgesicherte API (gleiche Decoupling-Philosophie wie unsere internen Adapter).
  - **OS-Steuerung (Bildschirm schwarz, Spiel minimieren):** Diese „krassen" Effekte brauchen OS-Level-Zugriff auf der Streamer-Maschine → höchste Risikoklasse. Müssen über einen separaten, explizit autorisierten **lokalen Agent/Companion** mit eng begrenzten, deklarierten Fähigkeiten laufen - niemals beliebiger Skript-Code mit Vollzugriff.
- **Monetarisierung-Potenzial:** Kostenlose Community-Addons + optionale Premium-Addons (Revenue-Share mit Autoren) = Plattform-Ökonomie wie bei OBS-Plugins/VST.
- **Competitor-Status:** Firebot hat „Effects" + Setups-Import, aber keinen kuratierten Security-vetteten Marketplace. Streamer.bot hat mächtige Aktionen aber keine Sandbox/Review. **Ein signierter, sandboxed Addon-Store ist novel** - und der eigentliche Moat (Switch-Cost + Netzwerkeffekt Autoren↔User).
- **Effort:** Sehr hoch (~10-16 Wochen über mehrere Phasen). Permission-Modell + Sandbox + Signing-Pipeline + Store-Backend + Review-Workflow.
- **Tech:** Addon-Manifest (deklarierte Permissions), WASM- oder Prozess-Isolation für untrusted Code, Signing (minisign/cosign-Stil), Store-Backend, lokaler Companion-Agent für OS-Effekte.

#### 15. **AI-Voice / TTS-Persönlichkeiten (ElevenLabs-nativ)** (Cloud + BYOK) 🔥🔥
- Viele Streamer bauen sich eigene KI-Persönlichkeiten (Donation-Vorleser, Chat-Reaktionen). Aktuell hand-gefrickelt mit viel Aufwand. engelOS macht das **nativ + ohne Setup-Hölle**.
- **Funktionen:** Donations/Bits/Subs vorlesen, auf @bot-Mentions reagieren, Channel-Point-Reward „lass die KI etwas sagen", Discord-Voice-Ansagen.
- **Stimmen:** Auswahl aus ElevenLabs-Stimmen ODER eigene **Voice-Clone** trainieren (die eigene Stimme oder eine Wunschstimme). BYOK = User hinterlegt eigenen ElevenLabs-Key, Cloud-Tier = managed.
- **Verhältnis zu #5 (AI Co-Host):** #5 ist der „spricht-wie-du Vollblut-Co-Host mit <300ms Latenz". #15 ist die breitere, einfacher zugängliche TTS-Persönlichkeits-Schicht (Vorlesen + simple Reaktionen) - #15 ist die Einstiegsstufe, #5 die Premium-Ausbaustufe. Teilen sich die Audio-/ElevenLabs-Infrastruktur.
- **Competitor-Status:** Streamlabs Cloudbot hat TTS, aber generische Stimmen ohne Persönlichkeit/Clone. Kein Bot bietet einfaches natives Voice-Clone + Persönlichkeits-Setup.
- **Effort:** ~4-5 Wochen Solo-Dev (ElevenLabs-Integration + Voice-Management-UI + Audio-Routing nach OBS).
- **Tech:** ElevenLabs API/WebSocket, Audio-Output via Virtual-Audio-Cable/WebRTC nach OBS, BYOK-Key-Storage (verschlüsselt).

#### 16. **Multistreaming / Restream (Simulcast auf mehrere Plattformen)** (Cloud-only) 📡⚠️
- Idee (Luca, 2026-05): Der Streamer sendet **einmal** und engelOS verteilt den Stream **gleichzeitig** auf mehrere Plattformen (Twitch + YouTube + Kick + Facebook + Custom-RTMP) - wie Restream.io / Streamlabs Multistream / OBS-Multi-RTMP, aber als integrierter Teil des Bots statt als separater Drittanbieter.
- **Warum es strategisch passt:** engelOS ist eh schon der **Cross-Platform-Layer** (Unified Chat #10, Adapter für alle Plattformen). Wenn der Stream selbst auch über uns läuft, wird engelOS vom "Bot daneben" zur **zentralen Streaming-Kommandozentrale**: ein Login, ein Dashboard, das den Output UND die Community über alle Plattformen steuert. Maximaler Lock-in, klares Premium-Verkaufsargument.
- **Funktionen (Zielbild):**
  - Ein RTMP/SRT-Ingest-Endpoint, in den OBS sendet; Fan-out an N Plattform-Ziele (eigene Stream-Keys hinterlegt, verschlüsselt).
  - Pro-Ziel an/aus, pro-Ziel-Bitrate/Auflösung-Limits (Plattform-Caps respektieren).
  - Status-Dashboard: pro Ziel live/offline, Health, Dropped-Frames, Zuschauer (wo API verfügbar).
  - Optionale Aufnahme/VOD-Kopie.
  - Spätere Kür: server-seitiges **Transcoding** (eine Auflösung rein, mehrere raus) - nur wenn wir die Compute-Kosten tragen wollen.
- **⚠️ Ehrliche Einordnung - das ist ein anderes Tier als die Chat-Features:**
  - **Bandbreite/Compute:** Video-Fan-out ist teuer. Pass-Through (gleicher Stream 1:1 an alle Ziele) ist machbar mit moderater Egress-Bandbreite. Echtes **Transcoding** pro Ziel kostet ernsthaft CPU/GPU und Geld - das ist eine bewusste Infra-Investition, kein Solo-Dev-Nachmittag.
  - **Self-Hosting-Konflikt:** Multistreaming aus einem Heim-Upload ist durch die **Upstream-Bandbreite** des Streamers begrenzt (genau deshalb existiert Restream als Cloud-Dienst). Darum **Cloud-only** und NICHT OSS-Core - der OSS-Daemon eines Heim-Self-Hosters kann das physisch oft nicht leisten. Für Self-Hoster bleibt OBS-Multi-RTMP die Alternative; wir dokumentieren das ehrlich.
  - **Scope-Disziplin:** Das berührt unser Anti-Pattern "wir bauen keinen Video-Encoder/keine OBS-Alternative". Restream bleibt **Output-Distribution** (RTMP-Relay), NICHT Szenen-Komposition/Encoding. Klare Grenze ziehen.
- **Competitor-Status:** Restream.io, Streamlabs Ultra (Multistream), Castr, OBS Multi-RTMP-Plugin. Alle sind **entweder** ein reiner Multistream-Dienst **oder** ein Bot - **keiner** verbindet Multistream + Cross-Platform-Bot + Unified-Chat + Loyalty in einem Produkt. Genau da liegt unsere Lücke.
- **Effort:** Hoch und infra-lastig. MVP "Pass-Through-Relay" (kein Transcode): ~4-6 Wochen + laufende Bandbreitenkosten. Transcoding-Ausbaustufe: deutlich mehr (eigene Media-Server-Flotte).
- **Tech (zu evaluieren):** Media-Server als Relay - **MediaMTX** (Go, RTMP/SRT/WebRTC, passt zum Stack), **nginx-rtmp**, oder **LiveKit/SRS**; Stream-Key-Storage verschlüsselt (wie OAuth-Tokens, `ENGELOS_SECRETS_KEY`); pro-Ziel-Health via RTMP-Stats; Transcoding später via ffmpeg/GPU-Worker.
- **Roadmap-Einordnung:** Frühestens **Phase 3-4** (nach Unified-Chat + Cloud-Infra steht). Bis dahin als **dokumentierte Vision** geparkt - Anti-Premature-Build. Vorher validieren: wollen genug Cloud-User Multistream genug, um die Bandbreiten-/Transcode-Kosten zu rechtfertigen?

### OVERLAY-SYSTEM (eigene Feature-Klasse)

OBS Browser-Sources für Stream-Overlays. Jedes Overlay ist eine eigene URL aus dem lokalen oder Cloud-Web-Server, die der User in OBS reinzieht.

**Architektur-Prinzip:** Der Bot **hostet die Overlay-HTML selbst** - lokal (`http://localhost:8080/overlay/...`) wenn self-hosted, oder über den Cloud-Web-Server. Der User zieht die URL als OBS-Browser-Source rein und stellt alles im Dashboard ein. Keine externe Abhängigkeit, keine fremden Server. (Bereits live: events/alerts/leaderboard.)

**Overlay-Library (Phase 1-2):**
- 🎵 **Spotify Now-Playing** (Track + Cover + Progress-Bar, customizable Theme)
- 🎁 **Follower / Sub / Donation Alerts** (animiert, customizable Sound + Animation)
- 💬 **Recent-Chat-Overlay** (für Vertical-Stream-Layouts)
- 🏆 **Goal-Bars** (Follower-Ziel, Sub-Goal, Bits-Goal)
- 📊 **Live-Poll-Overlay** (Twitch-Polls schöner dargestellt)
- 🔥 **Streak-Counter** (Win-Streak, Death-Counter, manual increments)
- 📅 **Schedule / Next-Stream-Overlay**

**Overlay-Library (erweitert - Brainstorming 2026-05):**
- 📰 **AI-News-Overlay** - kuratierte/KI-zusammengefasste News-Einblendung im Stream (Idee aus Lucas früherem „KI-News-Ding"). Konfigurierbare Themen/Quellen, Auto-Refresh, schönes Theme. Lokal oder Cloud gehostet.
- ✨ **Sticker-Unlock-Overlay** - Viewer schalten per Channel-Points (Feature #13) **Sticker frei**, die mit einer **coolen Animation** über den Stream laufen (Gambling-Site-/Lootbox-Ästhetik: Rarity-Tiers Common→Legendary, Sound, Partikel). Hoher Hype-/Interaktions-Faktor, perfekter Channel-Points-Showcase.
- 🎰 **Reward-Reveal-Animationen** - generische „Channel-Point eingelöst"-Effekte (Slot-Machine-Reveal, Konfetti) als wiederverwendbare Overlay-Bausteine für Marketplace-Addons.
- 💝 **Top-Supporter-Leaderboard**
- 🎬 **Recent-Clip-Auto-Replay** ("Replay of the day")
- 🎮 **Game-Stats-Overlay** (Phase 2+, per Integration: Apex K/D, LoL Rank, etc.)

**Overlay-Architektur:**
- Jedes Overlay ist eine eigene Svelte-Komponente
- Receives Live-Updates via WebSocket vom Daemon
- Customizable via Web-Dashboard (Themes, Colors, Position)
- URL-basiert: `http://localhost:8080/overlay/spotify?theme=dark&accent=purple`
- Cloud-User: `https://overlay.engelos.com/{user-id}/spotify?...`

### Bonus-Features (Phase 3+)

- Skill-Tree-System (Khan-Academy-Style: Gamer/Chatter/Loyalist/Creator-Branches)
- DAO-Style Community-Voting (Viewer entscheiden was Streamer als nächstes spielt)
- Battle-Pass / Season-Pass (90-Tage Cycles, Free + Premium Tier)
- Clan/Guild-System mit Raid-Mechanics
- Personal Records + Achievements (Strava-Style)
- Concierge-Onboarding (7-Day Drip-Sequence für neue Members)
- Anonymous Peer-Support + Crisis-Detection (Mental Health → PR-Gold)
- IoT/Smart-Light-Triggers (Sub → Philips Hue flasht grün)
- Stream-Health-Monitoring (Bitrate, Encoding, Viewer-Anomalies)
- Workflow-Engine ("Zapier for Streaming" - Visual Rule Builder)

---

## 💻 TECH-STACK - Detaillierte Komponenten-Liste

### Core Daemon (Go)

```
Language:       Go 1.24+
Build Tool:     go build + GoReleaser für Multi-OS-Distribution
CGO:            DISABLED (CGO_ENABLED=0) - required für Cross-Compile

Critical Deps:
─────────────────────────────────────────────────────
SQLite:         modernc.org/sqlite (pure Go, no CGO)
PostgreSQL:     github.com/jackc/pgx/v5
WebSocket:      github.com/coder/websocket
HTTP:           net/http stdlib + github.com/go-chi/chi/v5 (Router)
Logging:        log/slog stdlib
Config:         github.com/spf13/viper
CLI:            github.com/spf13/cobra
Testing:        stdlib + github.com/stretchr/testify

Platform-Adapters:
─────────────────────────────────────────────────────
Twitch IRC:     github.com/gempir/go-twitch-irc
Twitch Helix:   github.com/nicklaw5/helix
Twitch ES:      custom ~200 LOC (no mature Go EventSub-WS lib)
Discord:        github.com/bwmarrin/discordgo
YouTube Live:   google.golang.org/api/youtube/v3
Kick:           custom WebSocket (no library exists in any language)

Auth + Crypto:
─────────────────────────────────────────────────────
Password Hash:  golang.org/x/crypto/argon2
JWT:            github.com/golang-jwt/jwt/v5
OAuth:          golang.org/x/oauth2 + provider-specific endpoints
TOTP:           github.com/pquerna/otp

Observability:
─────────────────────────────────────────────────────
Metrics:        github.com/prometheus/client_golang
Tracing:        go.opentelemetry.io/otel
Profiling:      net/http/pprof stdlib

Distribution:
─────────────────────────────────────────────────────
Release:        github.com/goreleaser/goreleaser
Code-Signing:   macOS: notarytool, Windows: signtool
Auto-Update:    github.com/minio/selfupdate (für Cloud-Pro-Feature)
```

### Native GUI (Wails v2)

```
Framework:      github.com/wailsapp/wails/v2
Frontend:       Svelte (embedded in Wails)
Bindings:       Go ↔ TypeScript via Wails-generated bindings
Build Targets:  darwin/amd64, darwin/arm64, windows/amd64, linux/amd64
Bundle Size:    ~30-50MB (Webview not bundled - uses system Webview)
Packaging:
  - macOS:      .app → .dmg (wails build -platform darwin/universal)
  - Windows:    .exe → .msi (wails build -platform windows/amd64 -nsis)
  - Linux:      AppImage (Phase 2)
```

### TUI (Bubble Tea)

```
Framework:      github.com/charmbracelet/bubbletea
UI-Library:     github.com/charmbracelet/bubbles
Styling:        github.com/charmbracelet/lipgloss
Charts:         github.com/NimbleMarkets/ntcharts (für Live-Charts)

Features:
  - Live-Chat-Feed
  - Bot-Status-Dashboard
  - Command-Editor (auch im Terminal!)
  - Log-Viewer mit Filter
  - Tab-Navigation
```

### Frontend (Svelte/SvelteKit)

```
Framework:      Svelte 5 + SvelteKit 2
Build:          Vite
Styling:        Tailwind CSS 4
State:          Svelte Stores + svelte-query (für REST)
WebSocket:      native WebSocket API + custom Svelte-Store-Wrapper
Components:     shadcn-svelte (Component-Library)
Forms:          superforms + zod
Charts:         layerchart oder echarts-for-svelte
i18n:           svelte-i18n
Auth:           lucia-auth (Cloud) + custom für Local

Build-Targets:
  - apps/wails-gui  → Wails embedded build
  - apps/local-web  → static build → Go embed → in Daemon-Binary
  - apps/cloud-web  → SvelteKit SSR → deploy nach Vercel/eigener Server
```

### AI-Sidecars (Python - nur Cloud / BYOK)

```
Language:       Python 3.12+
Framework:      FastAPI
Audio (Co-Host): ElevenLabs WebSocket API
LLM (Mod/Translate): Anthropic Claude (Haiku 4.5)
Speech-to-Text: openai-whisper (lokal) oder Deepgram (Cloud)
Excitement-Detection: Custom-Model (PyTorch oder hosted via Replicate)

Deployment:
  - Cloud: Docker Container neben Go-Daemon
  - BYOK Self-Hosted: docker-compose mit "ai-services"-Profil
```

### Cloud-Backend Extensions (Go)

```
Identical Go-Stack zum Core, plus:

Billing:        github.com/stripe/stripe-go/v76
Email:          AWS SES oder Resend API
Storage:        S3-kompatibel (Hetzner Object Storage oder Cloudflare R2)
Queue:          NATS oder Redis Streams (für AI-Job-Queue)
CDN:            Cloudflare (Overlays + Static Assets)

Hosting (Cloud-Production):
  - Hetzner CCX23 (Phase 3) - €25/Mo
  - Hetzner Cloud Volume für Postgres + Backups
  - Cloudflare in front (DDoS + CDN)
  - Geo-Replication ab Phase 5
```

---

## 🔒 PRIVACY, DSGVO + LEGAL FOUNDATION

**Die Tagline "remembers you" ist marketing-stark und ein DSGVO-Triggerwort.** Wir speichern personenbezogene Daten von Viewern über Zeit + Plattformen. Das ist Datenverarbeitung im Sinne der DSGVO Art. 4. Plan-Detail nicht optional.

### Datenkategorien + Verantwortlichkeiten

| Daten-Kategorie | Wer speichert | Wer ist DSGVO-Verantwortlicher |
|---|---|---|
| Streamer-Account (Owner-Email, Auth) | Cloud-DB **oder** lokale SQLite | Cloud: wir / Self-Hosted: Streamer |
| Viewer-Identitäten (Twitch/Discord/YT-Usernames) | beides | Cloud: wir (Auftragsverarbeitung im Auftrag Streamer) / Self-Hosted: Streamer |
| Chat-Nachrichten (Event-Log) | beides | wie oben |
| Loyalty-Points + Watch-Time | beides | wie oben |
| Cross-Streamer-Loyalty (Phase 4) | nur Cloud | wir + alle teilnehmenden Streamer (gemeinsame Verantwortung) |
| AI-Training-Daten | nur wenn explicit opt-in | wir |

### DSGVO-Workflows (Pflicht ab Cloud-Launch / Phase 2)

**Für Cloud-Tier:**
- [ ] **AVV-Template** (Auftragsverarbeitungsvertrag) für Streamer als "Verantwortliche", wir als "Auftragsverarbeiter"
- [ ] **Privacy-Policy** auf engelos.com (mehrsprachig: DE, EN, ES, FR)
- [ ] **Cookie-Banner** mit echter Opt-Out (kein Dark-Pattern)
- [ ] **Right-to-Access**: Viewer kann anfragen "welche Daten habt ihr über mich" → automated Export (JSON + CSV)
- [ ] **Right-to-Erasure**: Viewer kann Löschung verlangen → Workflow: aus Event-Log Anonymisieren (nicht hard-delete, würde Event-Sourcing brechen) + aus Read-Models entfernen
- [ ] **Data-Portability**: Streamer kann seine Daten exportieren + zu anderem Bot mitnehmen
- [ ] **DSA / Digital-Services-Act** Compliance (ab 50k Streamern ggf. relevant)
- [ ] **Age-Verification**: Twitch-Viewer können minderjährig sein. DSGVO Art. 8: unter 16 Jahre = Eltern-Consent. Workflow: Twitch-OAuth liefert keine Alter-Info → wir behandeln alle Viewer als potentiell minderjährig + erfassen nur das **Minimum**

**Für Self-Hosting:**
- [ ] **Template Privacy-Policy** zum Übernehmen für Streamer's eigene Website
- [ ] **Setup-Wizard fragt:** "Wo wirst du das hosten? Wo ist deine Streamer-Community ansässig?" → generiert DSGVO-/CCPA-/PIPL-relevante Hinweise
- [ ] **Hint im Setup:** "Du bist DSGVO-Verantwortlicher. Du brauchst eine Privacy-Policy auf deinem Stream-Channel."

### Cross-Streamer-Loyalty Privacy-Architektur (Phase 4 Pre-Work)

Wenn Viewer-Points zwischen Streamern portabel sind, ist das eine **gemeinsame Datenverarbeitung** unter DSGVO Art. 26. Konsequenz:
- Explicit Consent pro Viewer pro Streamer-Pair (kein implizites Opt-in)
- Vertrag zwischen teilnehmenden Streamern (Template mitliefern)
- Wenn Viewer Löschung verlangt → alle teilnehmenden Streamer informieren
- Wenn Streamer A aus dem Network austritt → was passiert mit Punkten die Viewer X bei B angesammelt hat dank Cross-Loyalty?
- **Lösung:** Punkte-Buchung mit "Origin-Streamer"-Attribut, Origin-Verluste anteilig

### Trademark + Naming (Phase-0-Action-Item!)

**Bevor wir alles auf den Namen "engelOS" branden:**
- [ ] **DPMA-Suche** (Deutsches Patent- und Markenamt) für "engelos" / "engel OS" in Klassen 9 (Software) und 42 (SaaS) - kostenlos online
- [ ] **EUIPO-Suche** (EU-Trademark) - kostenlos online
- [ ] **USPTO-Suche** (US-Trademark) - kostenlos online
- [ ] **Domain-Konflikt-Check:** engelos.app, engelos.com, engelos.org, engelos.io - alle verfügbar?
- [ ] Falls Konflikt → Plan-B-Namen vorbereiten (engelbot, streamengel, oasis, stream-os, watchbase, ...)

**Risiko:** "engel" ist deutsches Allerwelts-Wort. "Engel & Völkers" (Immobilien), "Engelhart" (Cosmetics), "Engel-Bicycles" - alle in *anderen* Klassen, sollte kein Konflikt sein. Aber: ohne Suche keine Sicherheit. Cease-and-Desist in Jahr 2 würde das ganze Projekt zerstören.

### Abuse + Trust + Safety

Open-Source + Selfhostable = Bad-Actor-Vektor:
- **AI Auto-Clipper als Doxxing-Tool:** Bösartiger Streamer clipped Viewer-Reactions ohne Consent
  - Mitigation: Auto-Clipper darf nur **Streamer-eigenes Video** clippen, niemals Viewer-Webcams oder externe Sources
  - Plus: Clip-Notification an Viewer der "Star der Szene" war (opt-in)
- **Cross-Streamer-Ban-List als Weaponization-Tool:** Streamer A bannt Viewer X, der wird auf Streamer B/C/D auto-gebannt
  - Mitigation: Bans sind nur Empfehlungen, jeder Streamer entscheidet selbst
  - Plus: Bans brauchen Reason-Field, Viewer kann Appeal einreichen
- **Bot-Misuse für Chat-Spam:** Self-Hoster konfiguriert Bot als Spam-Cannon
  - Mitigation: Twitch-/Discord-ToS-Compliance hard-coded (Rate-Limits, Block-Lists)
  - Plus: Wenn Bot-Account von Platform gebannt wird → Daemon zeigt Error, refuses to operate
- **AGPL-Loophole:** Bösartiger Streamer modifiziert Source, deaktiviert Privacy-Workflows
  - Mitigation: AGPL zwingt zu Source-Veröffentlichung. Plus: official Cloud-Version + Code-Audit-Badge

### OSS-Governance

- **Code of Conduct:** Contributor-Covenant 2.1 von Tag 1
- **Maintainership:** Phase 0-2 = Du alleine (BDFL)
- **Phase 3+:** Top-5-Contributors kriegen Co-Maintain-Rechte
- **Phase 4+:** "engelOS Foundation" gründen (Verein nach BGB, optional später gGmbH)
- **Trademark im Foundation-Besitz** → schützt gegen Hostile-Fork-Branding
- **Conflict-of-Interest-Policy:** Wir entscheiden Cloud-Roadmap, Community entscheidet Core-Roadmap (mit Veto-Recht des Foundation-Boards)

---

## 💸 FUNDING & OPERATIONAL COSTS

**Realität:** Phase 0-2 (14 Monate, Mai 2026 – Juni 2027) generieren **€0 Revenue** aber haben reale Kosten. Ohne Funding-Plan kein Projekt.

### Cost-Projektion nach Phase (realistisch)

| Phase | Dauer | Monatliche Fixkosten | AI-API-Kosten (variabel) | Gesamt |
|---|---|---|---|---|
| **Phase 0** (Mai-Juni 2026) | 2 Mo | €40 (Domains €25/Jahr + GitHub-Org free + Twitter free) | €0 | **~€80** |
| **Phase 1** (Juni-Dez 2026) | 7 Mo | €15/Mo (Cloud-Backup, Email-Service) | €20-50/Mo (Solo-Testing) | **~€200-500** |
| **Phase 2** (Jan-Juni 2027) | 6 Mo | €150/Mo (Hetzner CCX23 €25 + Cloudflare-Pro €20 + Postgres Backup-Storage €5 + Apple Dev €99/Jahr → €8/Mo + Windows-Cert €300/Jahr → €25/Mo + Email-Service €10 + Misc €60) | €300-800/Mo (100 Beta-User AI-Usage) | **~€2.700-5.700** |
| **Phase 3** (Juli 2027-Juni 2028) | 12 Mo | €300/Mo (Hetzner-Skalierung + Multi-Region-Vorbereitung) | bis €2000/Mo bei 5000 User × moderate AI | revenue-positive ab Monat 4-6 |
| **Phase 4+** | – | €500-2000/Mo | revenue-deckt | self-sustaining |

**Gesamtaufwand Phase 0-2 (vor Revenue):** **~€3.000-6.500**

### Funding-Strategien (eine wählen)

**Option A: Self-Funded aus Sparkonto (Status Quo Default)**
- 14 Monate × €300-500/Mo = €4.000-7.000 aus eigener Tasche
- Risiko: real Geld weg wenn Projekt floppt
- Vorteil: keine Verpflichtungen

**Option B: Early-Monetization in Phase 2 (Recommended)**
- "engelOS Cloud Early-Backer Tier" zu €4.99/Mo ab Phase 2 (Cloud-Launch)
- Versprechen: Lifetime-Discount, alle Premium-Features, Founder-Badge
- Realistisches Ziel: 50-100 Backer × €5 = €250-500/Mo
- Deckt **75-100% der Phase-2-Fixkosten**
- Bonus: Frühe paying-User = valides Pricing-Signal

**Option C: GitHub Sponsors + Open-Collective**
- Ab Phase 1 (OSS-Launch) parallel zu A oder B
- Realistisches Ziel: €50-200/Mo bei ~500 GitHub-Stars
- Niedrige Erwartungen, aber psychologisch wichtig (signal)

**Option D: Sponsor-Programm für Streamer-Tools-Firmen**
- Stream Deck (Elgato), GoXLR, Beacn, Boom - alle haben Marketing-Budget
- "engelOS sponsored by [Brand]" Badges + Integration-Highlights
- Phase 2-3 realistisch, ~€500-1000/Mo wenn 1-2 Sponsoren

**Option E: Bootstrapping mit Side-Income aus Streaming**
- Wenn dein engelswtf-Stream wächst → Twitch-Subs/Bits/Donations
- Vorteil: Dogfooding-Loop ("Stream finanziert Bot, Bot verbessert Stream")
- Phase 2-3 realistisch

**EMPFOHLENE KOMBINATION:** A (Self-Funded Phase 0-1) + B (Early-Backer Phase 2) + C (GitHub Sponsors als Bonus) + ggf. E je nach Stream-Wachstum.

### Resilienz gegen Vendor-Preisanstieg

**Was wenn Anthropic API 10× teurer wird?**
- Claude Haiku 4.5 heute: ~$0.30 / 1M input tokens
- Worst-Case 10×: $3.00 / 1M input - Auto-Clipper-Margins kollabieren
- **Mitigation:**
  - BYOK-Modell für alle BYOK-fähigen Features → User bringt eigene API-Keys
  - Alternative-LLM-Adapter (GPT-4o-mini, Gemini Flash, Mistral, lokales Llama)
  - Cloud-Tier-Preis-Anpassung jederzeit möglich (Stripe Subscription-Update)
  - "Fair-Use-Limits" im Pro-Tier dokumentiert (z.B. "bis 200 Auto-Clips/Monat inklusive, drüber Pay-Per-Use")

**Was wenn ElevenLabs API 10× teurer wird?**
- AI Co-Host kostet schon heute ~€0.30/min Voice-Stream
- Mitigation: Alternative-TTS-Provider (PlayHT, OpenAI TTS, Cartesia)
- Plus: lokales Voice-Cloning (Coqui-XTTS) als Option für Power-User Phase 3+

---

## 🛠️ OPERATIONAL READINESS

**Kritisch ab Phase 2 Cloud-Launch:** SLA-Versprechen, On-Call, Incident-Response, Backups.

### SLA-Realismus

| Tier | Versprochene Uptime | Realistisch für Solo-Dev | Anpassung |
|---|---|---|---|
| Free/Beta (Phase 2) | "best effort" | machbar | – |
| Pro €9.99 (Phase 3) | 99% | machbar (~7h Downtime/Monat erlaubt) | – |
| Team €24.99 (Phase 3) | 99.5% | grenzwertig (~3.6h erlaubt) | OK mit Auto-Recovery |
| ~~Enterprise €99 (Phase 4): 99.9% SLA~~ | **STREICHEN** | **unmöglich solo** | → "Premium-Support, business-hours response within 4h" statt SLA |

**SLA-Versprechen vor Phase 4 = realistisch unmöglich.** Plan musste das ursprünglich falsch versprochen. Korrektur: 99.9% kommt erst wenn 24/7-On-Call durch zweite Person abgedeckt ist (Phase 4 Hire), und auch dann nur als Roadmap-Versprechen für 12-Monate-später.

### On-Call-Strategie

**Phase 0-2:** Du. Best-Effort. Status-Page kommuniziert ehrlich.
**Phase 3 (€2-3k MRR):** Statuspage + UptimeRobot + ntfy/Telegram-Alert
**Phase 4 (€10k+ MRR):** Part-Time-Hire teilt sich On-Call. PagerDuty oder OpsGenie.
**Phase 5+:** Full on-call rotation mit 3+ Personen.

### Incident-Response-Playbook (ab Phase 2)

- [ ] **Status-Page** auf status.engelos.com (instatus.com oder selbst-hosted Hugo)
- [ ] **Incident-Communication-Templates** (4 Levels: investigating / identified / monitoring / resolved)
- [ ] **Runbooks** für die 10 wahrscheinlichsten Outage-Szenarien
- [ ] **Postmortem-Template** (RCA, Action-Items)
- [ ] **Bug-Bounty-Program** ab Phase 3 (HackerOne oder Self-Hosted via Issue-Template)

### Backup + Disaster-Recovery

**Cloud-Tier:**
- [ ] **Postgres-Backups:** stündlich nach Hetzner Object Storage (Phase 2), zusätzlich täglich verschlüsselt nach 2. Region (Phase 4)
- [ ] **RTO (Recovery Time Objective):** 4h (Phase 2-3) → 1h (Phase 4)
- [ ] **RPO (Recovery Point Objective):** 1h (Phase 2-3) → 5 min (Phase 4)
- [ ] **Disaster-Recovery-Drill:** 1× pro Quartal (Phase 3+)

**Self-Hoster:**
- [ ] **Backup-Wizard im Web-UI:** "Schedule daily SQLite backup to Dropbox/Google Drive/S3/local-folder"
- [ ] **Documentation:** Backup-Anleitung in Quick-Start
- [ ] **Wir sind NICHT verantwortlich für Self-Hoster-Backups** - explizit dokumentiert in Terms

### Observability (Cloud-Tier)

- [ ] **Prometheus-Metriken** im Daemon (rate of events, error rate, AI-API-latency, etc.)
- [ ] **Grafana-Dashboard** für uns intern (Phase 2)
- [ ] **Sentry** oder selbst-hosted GlitchTip für Error-Tracking
- [ ] **Loki** für aggregierte Logs (existiert auf eurem home-pve schon)
- [ ] **Alerting:** ntfy / Telegram (existiert auf Hetzner schon)

### Opt-In Telemetry (für realistische User-Counts)

**Problem:** Plan verspricht "100+ Self-Hoster"-Metriken, aber Anti-Pattern verbietet User-Tracking ohne Opt-In. Auflösung:

- [ ] **Anonymous Heartbeat opt-in:** Setup-Wizard fragt "Sende anonymes Lebenszeichen 1×/Woche an engelos.org?" mit klarer Erklärung
- [ ] **Daten:** nur `version`, `os`, `arch`, anonymes Instance-UUID (rotated yearly), `uptime`
- [ ] **NICHT übertragen:** Streamer-Names, Channel-Names, Viewer-Data, Configs
- [ ] **Opt-Out:** ein-Klick im Settings
- [ ] **Public Stats:** `stats.engelos.org` zeigt aggregierte Counts ("3.247 active instances, mostly Linux + Docker")

**Bonus-Effekt:** Public-Stats sind selbst Marketing-Material ("Wow, 3000+ Self-Hoster!").

---

## 🛤️ ROADMAP - 5-7 Jahre Realistic Path

### PHASE 0: Bridge & Foundation (Mai 2026 - Juni 2026) ⚡

**Ziel:** EngelGuard wieder live, engelOS-Skelett aufsetzen, OSS-Repos initialisieren

#### EngelGuard-Rescue (parallel)
- [ ] **Bot-Service fixen** (Systemd-Hardening inkompatibel mit LXC - siehe `engelguard-bot-audit.md`)
- [ ] **Git aufräumen** (4.857 Lines uncommitted in logische Commits packen)
- [ ] **DB-Migration verifizieren** (`database.py` vs `database_new.py`)
- [ ] **Backup-Strategie** (täglich nach `/root/.sisyphus/state/`)
- [ ] Bot läuft stabil für engelswtf

#### engelOS-Initialisierung (parallel)
- [ ] **Domains reservieren:** engelos.app, engelos.com, engelos.org, engelos.io
- [ ] **GitHub-Org:** `engelos` (oder `engelos-bot` falls vergeben)
- [ ] **Twitter/X:** `@engelos`
- [ ] **Repos initialisieren:**
  - `engelos/engelos` - Core (AGPL-3.0)
  - `engelos/web` - Frontend Monorepo
  - `engelos/cloud` - Cloud-only Backend (private)
  - `engelos/docs` - Docs-Site (hugo, später)
- [ ] **Initial-Commit:** Skeleton mit Go-Modul, Wails-Boilerplate, Svelte-Setup
- [ ] **CI/CD:** GitHub Actions für Multi-OS-Builds
- [ ] **License:** AGPL-3.0 in alle OSS-Repos

**Output:** EngelGuard läuft, engelOS hat Repo + leeres Skelett, Distribution-Pipeline ready

### PHASE 1: Core + 4 Killer-Features (Juni 2026 - Dezember 2026)

**Ziel:** Bot mit 4 Industry-First-Features den du täglich nutzt, OSS-Public

> **Effort-Realismus:** Alle Wochen-Angaben sind **Solo-Dev-Wochen mit 15-20h Verfügbarkeit neben Vollzeit-Job**. Eine "1-Wochen-Aufgabe" entspricht ~15-20h echter Arbeit, kein Vollzeit-Sprint. Wenn andere Schätzungen "1-2 Wochen Vollzeit" sagen, lesen wir das als 3-5 Wochen Side-Project-Zeit.

#### Sub-Phase 1A: Core-Skelett (Juni-August, ~10 Wochen)

**Multi-Platform-First - Day 1 Twitch UND Discord parallel:**
- [ ] **Event-Sourcing-Engine** (Go) - PostgreSQL Append-Only-Log mit Snapshot-Strategie (~2 Wochen)
- [ ] **Platform-Adapter-Interface** + **Twitch-Adapter** (IRC + Helix) (~3 Wochen - EventSub-WS-Custom-Code dauert)
- [ ] **Discord-Adapter** (discordgo) - First-Class, nicht "auch dabei" (~2 Wochen)
- [ ] **Auth-System** (Owner/Admin/Mod/Viewer + API-Keys + Scopes + Argon2id) (~2 Wochen)
- [ ] **Setup-Wizard** (Erststart-Flow im Web-UI) (~1 Woche)
- [ ] **Web-Dashboard v1** (Svelte) - Chat-Viewer, Command-Editor, Status (~3 Wochen)
- [ ] **GoReleaser Pipeline** - Linux .deb/.rpm/Docker built + released (~1 Woche)
- [ ] **PWA-Manifest** für Local-Web-UI (für jetzt **statt** Native-GUI - siehe 1D)

**Warum Twitch + Discord parallel:** Twitch ist Amazon-owned und kann jederzeit Konkurrenz integrieren. Multi-Platform ist unser einziger struktureller Schutz davor. Ein nur-Twitch-Bot in Phase 1 wäre strategisch fragil.

#### Sub-Phase 1B: Quick-Win Features (September-Oktober, ~8 Wochen)
- [ ] **Pity-System** (~2 Wochen)
- [ ] **Streak-System mit Streak-Freeze** (~3 Wochen)
- [ ] **Real-Time Translator** (BYOK Claude + Cloud-Optional) (~3 Wochen)

#### Sub-Phase 1C: AI-Killer-Features (November, ~5 Wochen)
- [ ] **AI Auto-Clipper** (Python-Sidecar, BYOK + Cloud) (~4 Wochen - Excitement-Detection nicht trivial)
- [ ] **Context-Aware AI-Mod** (BYOK Claude, Rolling-Window-Prompts) (~6 Wochen, läuft in 1D weiter)

#### Sub-Phase 1D: Polish + Migration + OSS-Launch (Dezember, ~6 Wochen)

**Migration-Tools (Pflicht für OSS-Launch - sonst kommt niemand):**
- [ ] **Nightbot-Import** (Commands, Timer, Quotes) - ~1 Woche
- [ ] **StreamElements-Import** (Commands, Loyalty-Best-Effort, Quotes) - ~1 Woche
- [ ] **Moobot-Import** (Commands, Timer) - ~3 Tage
- [ ] **One-Click "Switch Bot in My Channel"-Flow** im Onboarding

**Polish + Launch:**
- [ ] **Stream-Wrapped-Cards** (Year-End perfectly timed!) - ~4 Wochen
- [ ] **TUI v1** (Bubble Tea) - Chat-Viewer, Status, Logs - ~2 Wochen
- [ ] **AI Co-Host TTS** als Cloud-Preview (private Beta) - ~6 Wochen (läuft in Phase 2 weiter)
- [ ] **Live-Ops-Calendar** - ~3 Wochen
- [ ] **BeReal Moment-Alerts** - ~3 Wochen
- [ ] **OSS-Public-Launch:** GitHub-Repo öffentlich, README, Quick-Start
- [ ] **Docs-Site** (engelos.org mit Hugo)
- [ ] **Discord-Server** für engelOS-Community
- [ ] **HackerNews-Post:** "Show HN: engelOS - Open-Source Streaming Bot"

**Was BEWUSST NICHT in Phase 1 ist (verschoben):**
- ❌ **Native-GUI mit Wails** (verschoben nach Phase 2) - siehe Begründung unten
- ❌ **macOS .dmg / Windows .msi mit Code-Signing** (verschoben nach Phase 2)
- ❌ **YouTube-Live + Kick-Adapter** (Phase 2)

**Begründung Native-GUI-Verschiebung:** Wails ist elegant, aber Cross-OS-Code-Signing ist eine versteckte Komplexitäts-Bombe:
- macOS Apple Developer Account: €99/Jahr + jeder Build muss notarized werden
- Windows Code-Signing-Zertifikat: €200-500/Jahr von DigiCert/Sectigo (sonst SmartScreen-Warning bei User)
- Auto-Update-Frameworks OS-spezifisch (Sparkle für macOS, MSIX für Windows)
- Apple-Silicon vs Intel = 2× Build-Matrix für macOS
- Webview-Verhalten unterschiedlich zwischen OS → CSS-Bugs only-on-one-OS

In Phase 1 nehmen wir stattdessen **PWA (Progressive Web App)**: User öffnet `http://localhost:8080` im Browser, klickt "Install as App", kriegt Desktop-Icon, fühlt sich an wie native App - **kein Code-Signing nötig**. Phase 2 dann echte Native-GUI mit Wails wenn Cashflow Code-Signing finanziert.

**Output Phase 1:** Funktionierender Bot mit 8 Killer-Features, Linux-Binary + Docker + PWA-Web-UI, Migrations-Tools für Nightbot/StreamElements/Moobot, OSS-Public-Launch auf GitHub, du nutzt ihn live auf engelswtf

### PHASE 2: Open Beta + Cloud-Launch + Native-GUI (Januar 2027 - Juni 2027)

**Ziel:** 100-1.000 Streamer, Cloud-Version live (kostenlos in Beta), Desktop-Native-Apps für Mac/Windows

#### Cloud-Aufbau
- [ ] **Cloud-Infrastruktur** (Hetzner CCX23, Postgres, Cloudflare) - Setup ~2 Wochen
- [ ] **app.engelos.com** Web-Dashboard live
- [ ] **OAuth-Onboarding** (Twitch/Discord/Google)
- [ ] **Managed Hosting** für Cloud-User
- [ ] **AI Auto-Clipper + Co-Host** als Cloud-Services (Production-ready)
- [ ] **Stripe-Integration vorbereitet** (noch nicht aktiviert - Phase 3)

#### Native-GUI (jetzt mit Cashflow-Backup)
- [ ] **Apple Developer Account** (€99/Jahr) + Code-Signing-Workflow
- [ ] **Windows Code-Signing-Certificate** (~€300/Jahr DigiCert/Sectigo OV-Cert)
- [ ] **Wails-Native-App v1** für macOS (.dmg) + Windows (.msi) - ~6 Wochen
- [ ] **Auto-Update-Framework** (Sparkle macOS / MSIX Windows) - ~3 Wochen
- [ ] **Apple Silicon + Intel Universal Binary** für macOS

#### Feature-Erweiterung
- [ ] **Channel-Points-Trigger-Engine (#13)** (EventSub-WS + Reward-CRUD + Trigger-Mapping + Dashboard) - ~3-4 Wochen - **Foundation für Marketplace, Sticker, AI-Voice-Trigger; nach hinten priorisierbar aber strategisch früh wertvoll**
- [ ] **AI-Voice/TTS-Persönlichkeiten (#15)** (ElevenLabs nativ, BYOK + Voice-Clone) - ~4-5 Wochen
- [ ] **Kick-Adapter** (custom WebSocket) - ~3 Wochen
- [ ] **YouTube-Live-Adapter** (google-api-go-client) - ~3 Wochen
- [ ] **Spotify-Integration** (erste Plugin im Integration-Framework) - ~2 Wochen
- [ ] **Overlay-System v1** (5 Overlays: Spotify, Alerts, Goal-Bar, Recent-Chat, Streak) - ~5 Wochen
- [ ] **Overlay-System v2** (AI-News-Overlay + Sticker-Unlock-Animationen via Channel-Points) - ~4 Wochen
- [ ] **VIP-Host-System** - ~4 Wochen
- [ ] **Unified Chat** (Twitch + Discord + YouTube + Kick aggregiert) - ~6 Wochen

#### Growth & Distribution
- [ ] **Homebrew Formula** (brew install engelos)
- [ ] **WinGet Submission** (winget install engelos)
- [ ] **Apt-Repository** (apt install engelos)
- [ ] **Raspberry Pi Image** (Pi-hole-Style, flashable)
- [ ] **Build-in-Public:** Twitter/X-Posts, GitHub-Aktivität
- [ ] **5 Mid-Tier-Streamer** (5k-50k Follower) als Test-Pilots
- [ ] **Reddit-Engagement:** r/Twitch, r/streaming (genuine, nicht spam)
- [ ] **Early-Monetization-Option:** "engelOS Cloud Early-Backer" €4.99/Mo (für Beta-User, Lifetime-Discount versprechen)

**Output:** 100-1.000 aktive Streamer, Cloud + Self-Hosted + Native-Desktop-Apps laufen, Discord-Community 200+ Member, 10+ OSS-Contributors

### PHASE 3: Monetization + PMF (Juli 2027 - Juni 2028)

**Ziel:** 5.000 Streamer, profitabel, Freemium aktiviert

#### Monetization-Flip
- [ ] **Free Tier:** Alle Core-Features unlimited (keine Limits)
- [ ] **engelOS Pro: €9.99/Mo:**
  - Cloud-AI-Features (Auto-Clipper, Co-Host) ohne BYOK
  - Premium-Voices, mehr Translator-Languages
  - Advanced Analytics
  - Priority Support
- [ ] **engelOS Team: €24.99/Mo:**
  - Pro-Features
  - Multi-Channel-Support
  - Mod-Team-Seats unbegrenzt
  - Cross-Channel-Mod-Sync
- [ ] **Stripe-Integration** live

#### Network-Effect-Foundation
- [ ] **Cross-Streamer Ban List** (erstes Network-Feature)
- [ ] **Workflow-Engine** ("Zapier for Streaming")
- [ ] **TikTok-Live-Integration** (early-mover wenn API stable)
- [ ] **Addon-/Skript-Marketplace (#14)** - kuratierter, signierter, sandboxed Store für Community-Addons (inkl. Trolling-Effekte: Bildschirm schwarz, Spiel minimieren, etc.). **Security-First: Permission-Sandbox + Code-Signing + Review-Team + lokaler Companion-Agent für OS-Effekte.** Über mehrere Phasen (~10-16 Wochen). DER eigentliche Moat (Autoren↔User-Netzwerkeffekt).
  - Sub-Schritt 1 (Phase 3): Addon-Manifest + Permission-Modell + Sandbox-Runtime (WASM/Prozess-Isolation) + Signing-Pipeline
  - Sub-Schritt 2 (Phase 3-4): Store-Backend + Review-Workflow + 1-Klick-Install
  - Sub-Schritt 3 (Phase 4): Lokaler Companion-Agent für OS-Level-Effekte (höchste Risikoklasse, eng begrenzte Capabilities) + Premium-Addon-Revenue-Share
- [ ] **Plugin-Marketplace v1** (Community kann Integrations einreichen) - geht im Addon-Marketplace #14 auf
- [ ] **Analytics-Dashboard** (Daten-Lock-in)

#### Growth-Acceleration
- [ ] **Twitch-Extension** auf Marketplace
- [ ] **YouTube-Tutorials** ("engelOS Tutorial Part 1: Setup")
- [ ] **TikTok-Content** ("5 things your streaming bot should do")
- [ ] **SEO:** "best twitch bot 2027", "open source twitch bot", "self-hosted twitch bot"
- [ ] **Influencer-Partnerships:** Free Pro für 10-20 Mid-Tier-Streamer
- [ ] **engelOS Conf 2027** (online, 1 Tag, OSS-Community + Streamer)

**Output:** €30-50k ARR, Bot trägt sich selbst, OSS-Community 50+ Contributors

### PHASE 4: Network Effects (Juli 2028 - Juni 2030)

**Ziel:** 50.000 Streamer, €15-50k/Mo ARR, "Industry-Standard"-Wahrnehmung

#### Experimentelle Network-Features (NICHT als "großer Moat" verkaufen)

> **Wichtige Einschränkung:** Cross-Streamer-Features sind **experimentell** und werden teilweise von Streamern aktiv abgelehnt werden ("ich will MEINE Viewer nicht teilen"). Wir testen Phase 4 ob Mutual-Benefit beweisbar ist. Wenn nicht - Feature wird verworfen, kein "großer Moat" verloren.

- [ ] **Shared-Community-Discovery** ("Viewers wie du sehen auch...") - **akzeptierter Weg**, bringt neue Viewer (nicht teilen bestehende)
- [ ] **Multi-Streamer-Raid-Coordination** (Raid-Schedule, Mutual-Promotion)
- [ ] **Cross-Streamer Ban-List 2.0** (mit Reason-Field + Appeal-System) - explicit consent pro Streamer
- [ ] **Cross-Streamer Loyalty-Network** (EXPERIMENTELL):
  - Viewer-Points portable zwischen opt-in Streamern
  - "Loyalty-Passport" für Viewer
  - DSGVO Art. 26 Joint-Controller-Vertrag zwischen Streamern
  - 1% Transaction-Fee auf Point-Exchanges = potentielle Platform-Revenue
  - **Wenn nach 6 Monaten <50 Streamer aktiv** → Feature deprecaten, akzeptieren dass es kein Moat ist
- [ ] **DAO-Style Community-Voting** mit Cross-Streamer-Polls (Phase 4 spät)

#### Premium-Tier-Expansion
- [ ] **engelOS Enterprise: €99/Mo:**
  - Team-Features
  - White-Label-Branding
  - API-Access mit höheren Rate-Limits
  - SLA (99.9%)
  - Dedicated Support-Channel
  - SSO/SAML

#### Skalierung
- [ ] **First Hire** (Part-Time Community-Manager, €2-3k/Mo)
- [ ] **Multi-Region** Cloud-Hosting (US-East zusätzlich zu EU)
- [ ] **OSS-Maintainer-Programm** (Top-5-Contributors kriegen Co-Maintain-Rechte)
- [ ] **engelOS Conf 2029** (in-person, 200 Leute, Berlin oder Amsterdam)

**Output:** €180-500k ARR, real Business, du bezahlst dich selbst Vollzeit

### PHASE 5: Commercial Inflection (Juli 2030 - 2031+)

**Ziel:** 100.000+ Streamer, €500k-2M ARR

**Decision-Point - Du wählst:**
- **A) Lifestyle-Business behalten** (du + 3-5 Leute, gemütlich profitable, €500k-1M/Jahr)
- **B) VC-Money raisen** ($2-5M Series-A, accelerate, exit-driven)
- **C) Strategic Acquisition** durch Logitech/Amazon/Twitch/Discord ($5-20M)

**Bezugspunkte:**
- Streamlabs wurde 2019 für $89M an Logitech verkauft
- StreamElements raised $100M in 2022
- Du wirst Inbound-Gespräche haben - entscheide bewusst

---

## 💰 BUSINESS-MODEL EVOLUTION

| Phase | User-Count | Modell | Monatliche Revenue |
|---|---|---|---|
| 0-1: Foundation | 1-100 | Free + OSS | €0 |
| 2: Open Beta | 100-1k | Free + Cloud-Beta | €0 |
| 3: Public + Freemium | 1k-5k | Free OSS + €9.99/€24.99 Pro/Team | €500-5k |
| 4: Tier-Expansion | 5k-50k | + €99 Enterprise + Transaction-Fees | €15-50k |
| 5: Industry-Standard | 50k-100k+ | + White-Label + API-Tier | €50k-200k |
| 6: Real Business | 100k+ | Full SaaS-Stack | €200k+ |

**Goldene Regel:** Monetize-Flip bei **5.000 aktiven Streamern**. Davor: zu wenige Daten für validierte Pricing. Self-Hoster zahlen niemals (das ist Feature, nicht Bug - sie sind Marketing und Trust-Beweis).

**Per-Tier-Margin-Erwartung:**
- Self-Hosted: 0% Revenue, ~0% Cost (Marketing-Funktion)
- Pro €9.99: AI-API-Kosten ~€2-3 (Auto-Clipper + Co-Host bei moderate-use), Margin ~70%
- Team €24.99: AI-Kosten + Multi-Channel-Overhead ~€5-7, Margin ~75%
- Enterprise €99: AI-Kosten + SLA-Support-Cost ~€20-30, Margin ~70%

---

## 🌍 DISTRIBUTION-STRATEGY - Wie der Bot "der Standard" wird

### Das Netdata-Playbook (was wir kopieren)

| Netdata-Tactic | engelOS-Übersetzung |
|---|---|
| `curl install.sh \| bash` Universal-Installer | Identisch - ein Befehl, läuft überall |
| Sofort beeindruckendes UI (kein Setup-Pain) | Setup-Wizard in <3 Min, UI sofort funktional |
| Docker als First-Class-Citizen | Multi-Arch-Image (amd64/arm64/armv7), `docker run engelos/engelos` |
| Cloud-Tier als optionaler Upgrade-Pfad | Identisch - Cloud nie nötig, aber leicht zu wechseln |
| Aggressive Cross-OS-Distribution | apt/rpm/brew/winget/Docker/Pi-Image - alles |
| Open-Source-Core mit klarer Cloud-Differenzierung | AGPL-Core + Cloud-Features, klar getrennt |
| Massive GitHub-Presence | Ziel: 5k Stars Jahr 1, 20k Jahr 2, 50k Jahr 3 |

### Tactical Playbook (eigene Adaptierungen)

1. **Build in Public** - Twitter/X, TikTok, GitHub-Aktivität
2. **Reddit-Engagement** (r/Twitch, r/streaming, r/discordapp) - genuine Hilfe
3. **5 Mid-Tier-Streamer einsammeln** (5k-50k Follower Sweet-Spot)
4. **"Switching from X to engelOS"-Guides** (Migrations-Tools mitliefern!)
5. **Discord-Server** für engelOS-Community von Tag 1
6. **Twitch-Extension-Marketplace** (Free Distribution-Channel)
7. **TikTok-Content** ("5 things your streaming bot should do")
8. **YouTube-Tutorials** (long-tail SEO)
9. **Hacker News + Lobste.rs** Posts bei Major-Milestones (Phase 1 Launch, 1.0-Release)
10. **Conference-Talks** (FOSDEM, GopherCon, Twitch-Streamer-Conferences) ab Phase 3

### Migration-Tools (kritisch für Adoption)

Wenn ein Streamer von Nightbot/StreamElements wechseln will:
- [ ] **Command-Import:** Lädt Nightbot-Export-JSON → konvertiert zu engelOS-Format
- [ ] **Timer-Import:** Gleiches für Auto-Messages
- [ ] **Quote-Database-Import:** Aus StreamElements / Moobot-Export
- [ ] **Loyalty-Migration:** Best-Effort-Übernahme der Punkte
- [ ] **One-Click "Switch Bot in My Channel"-Flow** im Onboarding

Ein Streamer mit 200 Custom-Commands wechselt nicht freiwillig. Wenn der Switch *2 Minuten* dauert, schon.

---

## 🛡️ DEFENSIBILITY - Was macht uns unkopierbar?

### Schwache Moats (jeder kann)
- ❌ Feature-Parity (Konkurrenz kopiert Features)
- ❌ Price (immer einer billiger)
- ❌ UI/UX (kann verbessert werden)

### Starke Moats (was wir bauen)

1. **Open-Source-Trust** - Closed-Source-Konkurrenz kann das nicht ohne komplettes Rewrite
2. **Data-History** - 2 Jahre Loyalty-Daten = ungerne weg
3. **Custom-Commands-Portability** - 200+ Commands schwer migrieren
4. **Cross-Streamer-Network-Effects** - funktioniert nur wenn andere drauf sind
5. **Plugin-Marketplace-Ecosystem** - Community-Built, Lock-in via Plugin-Investments
6. **GitHub-Community** - Stars, Contributors, Issues = Brand-Equity die nicht kaufbar ist
7. **Multi-UI-Choice** - wir sind die einzigen die alle 4 Modi anbieten
8. **Cross-OS-Native** - Win/Mac-Streamer haben heute Cloud-only Bots, wir bieten Desktop-App

### Day-1-Investitionen die in 5 Jahren zahlen

1. **Event-Sourcing** → enables Wrapped-Cards, Replays, AI-Training, Time-Travel
2. **Multi-Tenant-Schema** → kein Refactor bei Commercial-Launch
3. **Consent-Permission-System** → Cross-Streamer-Features ready when needed
4. **API-First** → Integrations + Plugin-Marketplace ready
5. **Platform-Adapter** → neue Platforms in 1-2 Wochen statt 3 Monaten
6. **OSS-Lizenz von Tag 1** → Trust-Equity die nicht nachträglich aufbaubar ist
7. **Cross-OS-Build-Pipeline** → keine "iOS-Style"-Lock-in-Probleme
8. **Go als Sprach-Wahl** → 10-Jahre-Compatibility-Promise

---

## 🚨 RISIKEN + Mitigationen

| Risk | Wahrscheinlichkeit | Impact | Mitigation |
|---|---|---|---|
| Twitch-API ändert sich | Hoch (jährlich) | Hoch | Platform-Adapter, Monitoring, Fallbacks |
| **Twitch baut Konkurrenz-Feature kostenlos integriert** | **HOCH** | **EXISTENZIELL** | **Multi-Platform Day-1** (Twitch+Discord+YT+Kick), Cloud-AI-Differenzierung, OSS-Trust |
| AI-native Bot disruptet uns | Mittel (2-4 Jahre) | Hoch | Wir SIND der AI-native Bot |
| Streamlabs kopiert Killer-Feature | Mittel | Mittel | Network-Effects + OSS-Trust schwer kopierbar |
| Twitch/Discord-Ban des Bots | Niedrig | Catastrophic | ToS-Compliance, Platform-Relationships, Multi-Platform |
| Big-Cloud kopiert OSS-Code | Mittel | Mittel | AGPL-3.0 schützt - sie müssten Source releasen |
| **Amazon (Twitch-Owner) sieht uns als Bedrohung** | Mittel (ab 10k User) | Hoch | Multi-Platform-Strategy, OSS-Reputation, Cross-Platform-Streamer (die nicht nur Twitch sind) |
| **AGPL blockiert Tool-Hersteller-Integrationen** | Hoch | Mittel | Dual-License: Plugin-SDK Apache-2.0, Core AGPL |
| **Code-Signing-Probleme blockieren Win/Mac-Release** | Mittel | Mittel | Phase-1 nur PWA, Native erst Phase 2 mit Budget |
| **Funding-Gap Phase 0-2 nicht abgedeckt** | Mittel | Hoch | Early-Backer-Tier Phase 2, GitHub-Sponsors, Self-Funding-Limit |
| **Burnout** (15-20h/Woche, 5 Jahre) | **HOCH** | Hoch | Realistic Scoping, Dogfooding, Community-Co-Build |
| No Product-Market-Fit | Mittel | Hoch | Continuous User-Talks, schnell pivoten |
| Kick/TikTok-Live kollabieren | Mittel | Niedrig | Modular, nicht über-investieren in eine Plattform |
| Self-Hoster-Support-Last | Hoch ab 1k User | Mittel | Excellent Docs, Discord-Community, FAQ-Bot |
| Cloud-Down-Time | Niedrig | Hoch | SLA-Investments ab Phase 4, Multi-Region |
| Security-Vulnerability in OSS-Core | Mittel | Hoch (Trust!) | Audit, Bug-Bounty ab Phase 3, Responsible-Disclosure-Policy |

### Größter Risikofaktor: **Du selbst**

5 Jahre Marathon neben Vollzeit-Job ist real. Mitigationen:
- **Dogfooding:** Du nutzt engelOS täglich auf engelswtf → Bot-Verbesserung = direkte QoL-Verbesserung
- **OSS-Community-Effekt:** Ab 50+ Contributors machen andere Features für dich
- **Klare Sub-Phase-Grenzen:** alle 2-3 Monate ein "definitiv-shippbarer" Meilenstein
- **Cloud-Revenue-Reinvest:** sobald Phase 3 monetarisiert ab €2-3k/Mo, optional Part-Time-Hire

---

## 🎯 ANTI-PATTERNS - Was wir NICHT machen

1. ❌ **NFTs / Crypto-Tokens** - regulatorisches Risiko, Community-Backlash, kein User-Value
2. ❌ **Loot-Boxes mit Echtgeld** - Belgien/Niederlande verboten, Image-Schaden
3. ❌ **Aggressive Push-Notifications** - kopiert BeReal schlecht = Churn
4. ❌ **Mood-Tracking ohne Consent** - DSGVO-Minenfeld
5. ❌ **Toxische Clan-PvP** - braucht careful Tuning gegen Harassment
6. ❌ **Premium-Walls für Basics** (wie Moobot) - Free Tier hat alles Wichtige
7. ❌ **Too-Early-Monetization** - vor 5.000 User keine Paywall
8. ❌ **Closed-Source-Pivot** - wäre Vertrauensbruch mit OSS-Community, Reputations-Tod
9. ❌ **Cloud-only-Lock-in** - würde unser eigenes Differential zerstören
10. ❌ **Type-Suppression** (`as any`, `interface{}`-Spam) - wir bauen es richtig
11. ❌ **Self-hosted "downgraded"** - Free-Tier-Self-Hoster bekommt vollständige Features (nur Cloud-AI-Sachen fehlen, das ist verständlich)
12. ❌ **Telemetrie ohne Opt-In** - anonymes Crash-Reporting opt-in, keine User-Tracking
13. ❌ **Audience-Growth-Versprechen** - wir verkaufen Tools, nicht Wachstums-Garantien. Niemals "Streamer wachsen schneller mit engelOS" in Marketing - kausal nicht beweisbar, würde zur Reklamation
14. ❌ **"First-Mover X/10"-Rating-Notation** - nicht messbar, klingt confidence-stark aber ist subjektiv. Ersetzen durch: konkrete Competitor-Verified-References ("Streamlabs hat manuellen Clip-Button, verified 2026-04, kein Auto-Detect")
15. ❌ **Cross-Platform-Datenteilung ohne explicit Consent** - DSGVO-Risiko + Streamer-Backlash
16. ❌ **Cloud-AI als Lock-in-Mechanik** - User kann jederzeit zu BYOK self-hosted wechseln, das ist Feature nicht Bug

---

## 📊 SUCCESS-METRICS (was wir messen)

**Mess-Methodik:** Alle "User-Count"-Metriken werden via **anonymous opt-in heartbeats** gemessen (siehe Operational-Readiness-Kapitel). GitHub-Stars ergänzend, aber wir wissen dass sie kaufbar/gameable sind - Forks + Issues + PRs sind die echteren Signale.

### Phase 1 Success (Dec 2026)
- [ ] Bot läuft 30+ Tage ohne Crash auf engelswtf
- [ ] 6+ Killer-Features funktional (Pity, Streak, Translator, Auto-Clipper, AI-Mod, Wrapped, Moment-Alerts, Live-Ops, Co-Host-Preview)
- [ ] OSS-Repo: 500+ GitHub-Stars **+** 50+ Forks **+** 10+ PRs eingegangen
- [ ] 5+ external Self-Hoster (verified via Heartbeat)
- [ ] Nightbot/StreamElements/Moobot-Importer funktional

### Phase 2 Success (Jun 2027)
- [ ] 100+ aktive Self-Hoster (verified via Heartbeat)
- [ ] 100+ Cloud-Beta-User
- [ ] 50+ Early-Backer-Tier paying (€4.99/Mo)
- [ ] OSS-Repo: 5.000+ GitHub-Stars + 500+ Forks
- [ ] 10+ external Contributors mit gemergedten PRs
- [ ] 5+ Community-Plugin-Integrations
- [ ] Native-Desktop-Apps (Win/Mac) released
- [ ] Status-Page mit ehrlicher Uptime-Historie

### Phase 3 Success (Jun 2028)
- [ ] 5.000+ aktive Streamer (verified via Heartbeat)
- [ ] €5k+ MRR (verified via Stripe-Dashboard)
- [ ] OSS-Repo: 15.000+ Stars + 1.500+ Forks
- [ ] In Top-10 der "Best Twitch Bots 2028"-Articles (verified via Search-Result-Audit)
- [ ] 1+ Twitch-Partner (10k+ Avg-Viewer) als public Reference

### Phase 4 Success (Jun 2030)
- [ ] 50.000+ aktive Streamer (verified via Heartbeat)
- [ ] €30k+ MRR
- [ ] OSS-Repo: 30.000+ Stars + 3.000+ Forks
- [ ] Default-Choice in 3+ "Setup Your Stream"-YouTube-Tutorials (Top-10-Results)
- [ ] Cross-Streamer-Network entweder: 1.000+ teilnehmende Channels ODER explizit deprecated als nicht-funktionierendes Experiment

### Anti-Goals (was wir NICHT messen)
- ❌ Total Discord-Server-Count (Vanity-Metric)
- ❌ Commands-Executed-per-Day (irrelevant für Geschäft)
- ❌ Cloud-only-User-Growth (würde gegen OSS-Mission gehen)

---

## 📚 RELATED-DOCS

- `engelguard-bot-audit.md` - EngelGuard Tech-Status + Fix-Plan
- `engelswtf-community-masterplan.md` - Discord-Server-Strategy (engelswtf-Server, nicht engelOS!)
- `engelssongg-audio-setup.md` - Audio-Setup (engelswtf-Streaming)
- `engelssongg-streaming-launch.md` - Streaming-Launch-Plan

---

## 🎬 ACTION-ITEMS - Was als nächstes

### Sofort (diese Woche) - Trademark-First!
1. **Trademark-Suche (KRITISCH, vor allem anderen):**
   - DPMA (Deutschland) online-Suche für "engelos" + "engel os" in Klasse 9 + 42
   - EUIPO (EU) online-Suche
   - USPTO (USA) online-Suche
   - Plan-B-Namen vorbereiten falls Konflikt: engelbot, streamengel, stream-os, watchbase, oasisbot
2. **Domain-Reservierung** (nach Trademark-Clear): engelos.app, engelos.com, engelos.org, engelos.io
3. **GitHub-Org reservieren:** `engelos` (nach Trademark-Clear)
4. **Twitter/X:** `@engelos` Handle sichern (nach Trademark-Clear)
5. **EngelGuard fixen** (Phase-0-Hauptaufgabe, parallel zur Trademark-Recherche)

### Kurzfristig (4 Wochen)
6. **Git-Cleanup EngelGuard** (4.857 uncommitted lines)
7. **engelOS Repo-Skeleton:**
   - `engelos/engelos` mit Go-Modul, Svelte-Setup (kein Wails Phase 1, nur PWA)
   - `engelos/sdk-go` (Apache-2.0)
   - `engelos/sdk-ts` (Apache-2.0)
   - GoReleaser-Config für Multi-OS-Build (Linux first, Win/Mac später)
   - AGPL-3.0 License-File für Core
   - Initial README
   - GitHub Actions CI
8. **Tech-Spike:**
   - Hello-World-Daemon der Twitch-Chat reads (gempir/go-twitch-irc)
   - Hello-World-Daemon der Discord-Gateway connected (discordgo)
   - Hello-World-Svelte-Frontend embedded in Go-Binary
   - Hello-World-Bubble-Tea-TUI
   - Manuelle Twitch-EventSub-WS-Implementierung (200 LOC)

### Mittel (3 Monate)
9. **Phase-1A komplett:** Event-Sourcing, Twitch+Discord-Adapters, Auth, Web-UI v1
10. **Erstes Killer-Feature live:** Pity-System (Easy-Win zum Validieren)
11. **Erstes Public-Posting:** "I'm building engelOS" auf r/Twitch + Twitter

### Mittel-Lang (6-9 Monate)
12. **Migration-Tools:** Nightbot/StreamElements/Moobot Importer (Phase 1D)
13. **AI Auto-Clipper + Translator** beide via BYOK + Cloud-Optional (Phase 1B)
14. **OSS-Public-Launch:** GitHub-Repo öffentlich, "Show HN: engelOS"
15. **Early-Backer-Tier vorbereiten** für Phase 2 Cloud-Launch (€4.99/Mo Lifetime-Discount)

---

## 🚀 SCHLUSSWORT

**Die Realität:** Niemand sonst baut einen Open-Source-Streaming-Bot der wirklich überall läuft. Die Top-Bots sind 8-14 Jahre alt, Closed-Source, Cloud-only. Keine moderne Architektur, keine OSS-Trust-Story, keine Multi-OS-Native-Apps, keine Network-Effects.

**Die Chance:** Markt ist groß genug für ein €5-50M-Business, aber zu "boring" für VCs und zu spezialisiert für Big-Tech. Indie-Dev mit langem Atem und OSS-Strategie kann gewinnen.

**Der Plan:** 5-7 Jahre. Side-Hustle-Tempo (15-20h/Woche). Dogfooding (du nutzt es täglich). Dual-License (AGPL-3.0 Core + Apache-2.0 SDK + Proprietary Cloud). Cloud-Premium für Wow-Features (Co-Host als Cloud-Anchor). Vier UI-Modi für vier User-Typen. Alle drei OS gleichwertig (Win/Mac Native erst Phase 2 wenn Code-Signing finanziert). Multi-Platform Day-1 (Twitch+Discord). Privacy by Design (DSGVO Workflows). Cross-Streamer-Network-Effects experimentell - Hauptmoat ist OSS-Trust + Daten-History + Multi-Platform.

**Das Risiko:** Du. 5 Jahre Marathon ist hart. Lösung: bau was du SELBST jeden Tag nutzen willst, lass OSS-Community helfen ab Jahr 2, monetarisiere ab Jahr 3 um Part-Time-Hire zu finanzieren.

**Die Vision:** In 5 Jahren ist engelOS der Bot über den Tutorials geschrieben werden, der Default-Choice für neue Streamer, der Standard den Konkurrenten kopieren wollen - *und an dem sie scheitern weil OSS-Trust und Network-Effects nicht kaufbar sind*.

**Der erste Schritt:** EngelGuard wieder live kriegen (Phase 0). Dann engelOS-Skelett aufsetzen. Dann jeden Monat ein Feature mehr. Dann in 6 Monaten OSS-Public-Launch.

---

**Codename:** engelOS - *The streaming bot that remembers you. Open source. Run it anywhere.*
