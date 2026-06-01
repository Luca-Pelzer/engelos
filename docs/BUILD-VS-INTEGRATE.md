# EngelOS - Build vs. Integrate Strategy

> **Erstellt:** 2026-06-01 | **Ueberarbeitet:** 2026-06-01 (nach Durchsprache aller Punkte mit Luca)
> **Zweck:** Eine konsistente Entscheidungslogik fuer JEDES Feature: selbst bauen, anbinden, oder hybrid. Grundlage sind die committeten 16 Killer-Features (siehe `MASTER-VISION.md`) plus die neuen Vorschlaege aus dem KI-Brainstorming, alle bewertet gegen die echte API-Realitaet im Juni 2026 (recherchiert mit Quellen).
> **Owner:** Luca (engelswtf)
>
> **Entscheidungen aus der Durchsprache (2026-06-01):** OBS/Channel-Points/Alerts bestaetigt. Action-Engine = Kern, industry-standard+ mit Memory. Chat-Uebersetzung ueber vorhandene Claude-Sub (gratis), KEINE Live-Untertitel. Auto-Clipper + TTS selbst (Modell angebunden). AI-Mod vierstufig + person-aware + Scam-Schalter + Link-Black/Whitelist. Musik/Now-Playing GESTRICHEN. Social-Auto-Post NUR Discord (Bluesky raus). Restream = Cloud-only, gut gebaut. Cloud-vs-Self-Hosted-Aufteilung definiert (Abschnitt 2b).

---

## 0. Die Entscheidungslogik (das eigentliche Werkzeug)

Bevor ein Feature gebaut wird, durchlaeuft es fuenf Fragen. Das Ergebnis ist eine von vier Strategien.

1. **Ist es reguliert oder haftungsbehaftet?** (Geld, Lizenzen, Urheberrecht)
   - Ja -> niemals selbst bauen. Anbinden. Der Drittanbieter traegt das Risiko.
2. **Ist es ein Commodity ohne Differenzierung?** (etwas, das alle gleich machen)
   - Ja -> anbinden, wenn eine API existiert. Keine Zeit mit Nachbau verschwenden.
3. **Ist es unser Wedge?** (der Grund, warum jemand wechselt)
   - Ja -> selbst bauen, voll besitzen. Hier liegt der Wert.
4. **Existiert eine reife, wartungsarme Library/API in unserem Stack (Go)?**
   - Ja -> Library nutzen, duenne Schicht drumherum. Nicht das Protokoll nachbauen.
   - Nein -> selbst bauen, aber Aufwand ehrlich einplanen.
5. **Faellt es unter unser Anti-Pattern?** (Video-Encoder, OBS-Alternative, Payment-Processor)
   - Ja -> Grenze ziehen. Relay/Anbindung statt Nachbau.

**Die vier Strategien:**

| Symbol | Strategie | Bedeutung |
|---|---|---|
| **BUILD** | Selbst bauen | Voll besitzen. Kernlogik, Wedge, oder kein guter Drittanbieter. |
| **WRAP** | Library/API einbinden | Reife Library existiert. Wir bauen die duenne Integrations- und UI-Schicht, nicht das Protokoll. |
| **INTEGRATE** | An Drittdienst anbinden | Reguliert/Commodity. Wir empfangen Events (Webhook/WS), triggern unsere Logik. Wir halten kein Geld/keine Lizenz. |
| **PARK** | Dokumentiert vertagen | Strategisch sinnvoll, aber infra-/kosten-lastig. Erst nach Validierung. |

**Wichtigste Erkenntnis aus der Recherche:** Die Trennung ist fast nie "ganz bauen" vs. "ganz kaufen". Das Muster ist fast immer **WRAP**: eine reife Go-Library oder eine Drittanbieter-API uebernimmt den schweren, sich-aendernden Teil (Protokoll, Modell, Compliance), und WIR bauen die wertvolle Schicht obendrauf - die Trigger-Engine, das UI, die Memory-Verknuepfung, das Cross-Platform-Verhalten. Genau das ist, was Firebot und Streamer.bot tun.

---

## 1. Master-Tabelle (alle Features auf einen Blick)

### Table-Stakes (Pflicht, sonst kein ernstes Tool)

| Feature | Strategie | Begruendung (Kurzform) | Konkretes Werkzeug |
|---|---|---|---|
| OBS-Steuerung (Szenen/Sources/Recording) | **WRAP** | Reifer Go-Client deckt v5-Protokoll voll ab | `andreykaipov/goobs` v1.8.3 |
| Alerts/Overlays (Follow/Sub/Raid/Donation, Goal-Bars) | **BUILD** | Kernprodukt, schon teils live, nur lokaler WS-Server + HTML | stdlib `net/http` + `coder/websocket` (bereits im Stack) |
| Channel-Point-Redemptions als Trigger | **WRAP** | Helix hat Reward-CRUD; EventSub-WS-Lifecycle selbst | `nicklaw5/helix` v2.34.0 |
| Twitch EventSub (Follow/Sub/Raid/Cheer) | **WRAP** | Library liefert Typen; WS-Verbindung selbst managen | `nicklaw5/helix` v2.34.0 |
| Song-Requests mit Queue | **BUILD** | Eigene Queue-Logik; haengt an Now-Playing-Integration | Pure Go (Store existiert teils) |

### Wedge (der Grund zu wechseln - selbst besitzen)

| Feature | Strategie | Begruendung | Konkretes Werkzeug |
|---|---|---|---|
| Action/Automation-Engine (Event -> Effect) | **BUILD** | DAS Kernprodukt. Firebot/Streamer.bot-Differenzierer, aber memory-aware | Eigenes Plugin-Registry-Pattern in Go |
| Sponsor-/Ad-Management (kleine Streamer) | **BUILD** | Echter Wedge, macht fast niemand gut. MUSS top gebaut sein (Aushaengeschild) | Pure Go + Scheduler |
| Regel-Mod (Scam + Links + Regeln) | **BUILD** | Immer gratis, ohne KI, fuer jeden. Laeuft lokal | Pure Go + gebundelte Scam-Liste |
| AI-Mod (person-aware, optional) | **WRAP** | Optionaler Aufsatz, BYOK. LLM fuer Graufaelle, Memory ist unser IP | Claude (BYOK) + Memory-Layer |
| Chat-Uebersetzung | **WRAP** | Laeuft ueber vorhandene Claude-Sub, KEIN neuer Dienst noetig. Nur Chat, keine Live-Untertitel | Claude via anthropic-proxy (gratis) |
| AI Auto-Clipper | **WRAP** | Kein Anbieter hat Auto-Detect; Clip-API von Twitch, Detection unser IP | Helix Clips + eigener Score |
| Cross-Platform Unified Chat + Mod-Sync | **BUILD** | Der Moat. Adapter-Layer existiert schon | Eigener Event-Bus (live) |
| Stream-Wrapped / Pity / Streak / Live-Ops / Moments | **BUILD** | Novel-Mechaniken, kein Anbieter hat sie | Pure Go (groesstenteils live) |

### Commodity / Reguliert (anbinden, nicht bauen)

| Feature | Strategie | Begruendung | Konkretes Werkzeug |
|---|---|---|---|
| Donations/Tips | **INTEGRATE** | Payment = reguliert. Nie Merchant-of-Record werden | Ko-fi Webhook + StreamElements WS |
| TTS-Stimmen | **WRAP** | Modell nicht bauen, Bot-Logik selbst | ElevenLabs (Premium) / Piper (self-host gratis) |
| Going-Live Auto-Post | **INTEGRATE** | Bot sendet automatisch | Discord-Webhook (nur Discord) |
| Discord-Sub-Sync | **WRAP** | Standard-Discord-API, kein Partner-Status noetig | `bwmarrin/discordgo` (im Stack) |
| Stream Deck | **INTEGRATE** | An vorhandene Hardware andocken | Elgato-Plugin-SDK |
| Payments/Billing (Cloud) | **INTEGRATE** | Reguliert | Stripe (nur Cloud-Tier) |

> **Gestrichen (Gespraech 2026-06-01):** DMCA-Musik/Now-Playing komplett raus (juristisches Minenfeld: Spotify-ToS-Verbot, Pretzel nur lokale Datei, Soundtrack tot). Dafuer gibt es YouTube etc. Bluesky-Auto-Post gestrichen (nur Discord gewuenscht).

### Park (vertagt, erst nach Validierung)

| Feature | Strategie | Begruendung |
|---|---|---|
| Multistreaming/Restream | **PARK** | Bandbreite/Transcode teuer, Cloud-only, Phase 3-4 |
| Cross-Streamer-Loyalty | **PARK** | DSGVO Art. 26, experimentell, Phase 4 |
| Addon-Marketplace mit Sandbox | **PARK** | Groesste Angriffsflaeche, 10-16 Wochen, mehrphasig |

---

## 2. Detail-Analyse pro Kategorie

### 2.1 OBS-Steuerung -> WRAP

**Entscheidung:** Reife Library einbinden, nicht das Protokoll nachbauen.

OBS-WebSocket v5 ist ein JSON-RPC-Protokoll ueber WebSocket (aktuell v5.7.3). Auth ist ein simples SHA-256 Challenge-Response mit lokalem Passwort - kein OAuth, kein Token-Tanz. Es gibt genau **einen** gepflegten, vollstaendigen Go-Client: `andreykaipov/goobs` (v1.8.3, April 2026, deckt v5.7.3 ab, Typen sind code-generiert aus der Protokoll-Spec).

```go
client, _ := goobs.New("localhost:4455", goobs.WithPassword("..."))
client.Scenes.SetCurrentProgramScene(
    scenes.NewSetCurrentProgramSceneParams().WithSceneName("Gaming"))
```

Szenen wechseln, Sources togglen, Recording/Streaming steuern - alles 3 Zeilen Go. **Selbst bauen waere reine Verschwendung.** Wir bauen nur die Bindung an unsere Trigger-Engine (Channel-Point eingeloest -> Szene wechseln).

**Aufwand:** Niedrig. ~1 Woche fuer Adapter + Dashboard-UI zum Mappen.

---

### 2.2 Alerts + Overlays -> BUILD

**Entscheidung:** Selbst bauen. Ist bereits teilweise live und braucht keine Fremdabhaengigkeit.

Eine OBS-Browser-Source ist nur eine Chromium-Instanz, die eine URL laedt. Die Overlay-Seite verbindet sich per WebSocket zurueck zum Bot und animiert das DOM bei Events. Das ist exakt, was Firebot (Open Source, GPL-3.0) macht: ein lokaler HTTP+WS-Server, Browser-Source zeigt auf `localhost`, Events werden gepusht.

Wir haben den lokalen WS-Server schon (`internal/runtime/wsbridge.go`, events/alerts/leaderboard-Overlays sind laut MASTER-VISION bereits live). Der Rest ist Svelte-Komponenten + WS-Push. **Kein Cloud-Dienst, keine StreamElements-Abhaengigkeit** - das ist genau unser Self-Hosting-Vorteil.

Die Overlay-Library aus der Vision (Spotify-Now-Playing, Sub/Donation-Alerts, Goal-Bars, Sticker-Unlock, Reward-Reveal) ist alles dieselbe Architektur: Svelte-Komponente + WS-Updates + Dashboard-Customizing.

**Aufwand:** Mittel pro Overlay-Typ, aber repetitiv. Foundation steht.

---

### 2.3 Twitch EventSub + Channel-Points -> WRAP

**Entscheidung:** `nicklaw5/helix` fuer die API-Typen + Reward-CRUD, EventSub-WebSocket-Lifecycle selbst verdrahten.

`nicklaw5/helix` (v2.34.0, April 2026) liefert alle EventSub-Typen als Konstanten (`channel.follow`, `channel.subscribe`, `channel.raid`, `channel.cheer`, `channel.channel_points_custom_reward_redemption.add`) und die komplette Channel-Points-Reward-CRUD-API.

Transport: **WebSocket** (`wss://eventsub.wss.twitch.tv/ws`), nicht Webhook - so brauchen wir keinen oeffentlichen Endpoint hinter NAT. Helix hat aber noch keinen High-Level-"EventSub-WS-Manager"; den Verbindungs-Lifecycle (connect -> session_id -> subscribe) verdrahten wir selbst (~200 LOC, in der Vision schon eingeplant).

Channel-Point-Redemption feuert -> wir lesen `reward.id` + `user_input` -> triggern unsere Aktion -> setzen via API `FULFILLED` oder `CANCELED` (Refund-Loop). Das ist die universelle Trigger-Schicht (Feature #13).

**Harte Constraints (aus der Vision, durch Recherche bestaetigt):**
- Custom Rewards brauchen Twitch **Affiliate/Partner** -> hinter `broadcaster_type`-Check gaten.
- Wir koennen nur Rewards refunden, die unsere **eigene OAuth-App** erstellt hat.

**Aufwand:** Niedrig-Mittel. Library nimmt den Grossteil ab.

---

### 2.4 Action/Automation-Engine -> BUILD (das Kernprodukt)

**Entscheidung:** Voll selbst bauen. Das ist der eigentliche Wettbewerbsvorteil gegen Streamer.bot.

Sowohl Firebot als auch Streamer.bot teilen dieselbe Architektur-Erkenntnis: **Event-Quellen von Effekt-Ausfuehrung entkoppeln ueber eine typisierte Plugin-Registry.**

```
EventSource (Twitch/YT/OBS/Discord) registriert benannte Events
  -> EventManager routet: Event feuert -> passende Settings finden -> Effekte ausfuehren
EffectType (play-sound/show-image/chat/http-request/...) registriert sich mit Trigger-Typen
```

Firebots eingebaute Effekte: `chat`, `play-video`, `show-image`, `text-to-speech`, `http-request`, `eval-js`, `run-program`, `delay`, `conditional-effects`, `effect-group`, `update-channel-reward`, `take-screenshot`, etc. Streamer.bots Differenzierer: Sub-Actions mit Queuing, Gruppen, Random-Selection (mehr visuelles Scripting als simples if-event-then-effect).

**Was wir lernen + besser machen:** Das Plugin-Registry-Pattern von Tag 1 in unsere Go-Interfaces einbauen (passt zum Adapter-Layer, den wir schon haben). Unser Vorteil: Die Engine sitzt auf dem **Memory-Layer + Cross-Platform-Event-Bus**, den die anderen nicht haben. Ein Trigger kann plattformuebergreifend feuern und Viewer-Historie kennen.

**UI-Form (Entscheidung Luca 2026-06-01): visueller Flow-Editor wie Streamer.bot / n8n / SAP iFlow** - Node-and-Wire, Kaesten ziehen und mit Linien verbinden (Trigger-Node -> Bedingungs-Node -> Aktions-Nodes), nicht nur eine simple Liste.

**Bau-Reihenfolge (kritisch, damit nie etwas blockiert ist):**
1. **Engine-Fundament zuerst** (Go): Trigger -> Bedingungen -> Aktionen als Datenmodell + Ausfuehrer. Funktioniert auch ohne huebschen Editor (konfigurierbar via simpler Form/JSON).
2. **Visueller Node-Editor als Aufsatz** (Svelte): zeichnet/editiert dasselbe Datenmodell. Der n8n-Style ist die Praesentationsschicht, die Logik darunter ist identisch.

So ist die Logik nie an den Editor gekoppelt - das Fundament liefert sofort Wert, der Node-Editor kommt als Ausbaustufe. Die Engine ist ausserdem die Basis, auf der spaeter der Addon-Marketplace (#14) aufsetzt.

**Aufwand:** Hoch (Fundament) + Hoch (Node-Editor) - aber das ist der Kern, nicht Beiwerk. Node-Editor-Tech zu evaluieren: Svelte-Flow (`@xyflow/svelte`) als Basis.

---

### 2.5 Chat-Uebersetzung -> WRAP (JETZT, GRATIS ueber vorhandene Claude-Sub)

**Entscheidung (Gespraech 2026-06-01):** Es geht NUR um die Uebersetzung von **Chat-Nachrichten** in anderen Sprachen. **Keine Live-Untertitel** (kein STT, kein Mikro-Mitschnitt) - das war ein Missverstaendnis und ist explizit nicht gewollt.

Spanischer/englischer Viewer schreibt -> wird ins Deutsche uebersetzt (in-chat oder Discord-Thread). Das laeuft ueber die **bereits vorhandene Claude-Subscription** (lokaler `anthropic-proxy`, Port 3033, `claude-haiku-4-5`). **Kein DeepL, kein Google, kein neuer Account, kein Cent extra.**

- Claude uebersetzt sehr gut und ist nur minimal langsamer als DeepL (paar hundert ms) - fuer Chat-Nachrichten voellig egal, die brauchen keine Echtzeit.
- Self-Hoster ohne unsere Sub: BYOK (eigener Claude-Key) oder lokales Modell (geringere Qualitaet).
- STT/Deepgram/whisper.cpp sind hier **nicht** noetig - die waeren nur fuer gesprochene Live-Untertitel, die wir nicht bauen.

**Aufwand:** Niedrig. Claude haengt schon im Stack, nur die Chat-Pipeline + Sprach-Erkennung + Zielsprachen-Config.

---

### 2.6 AI-Auto-Clipper + TTS -> WRAP (selbst, Modell angebunden)

Bestaetigt im Gespraech: Hier gibt es **nichts Vernuenftiges zum Integrieren** - selbst bauen, nur das Modell anbinden.

- **AI Auto-Clipper:** Kein Anbieter hat Auto-Detection (StreamElements/Nightbot/Fossabot: nichts; Streamlabs: nur manueller Knopf). Die Clip-*Erstellung* macht Twitch via Helix Clips API. Die **Erkennung "jetzt war ein Highlight"** (Chat-Velocity, Sub-Spikes, Emote-Explosion) existiert als Produkt nicht -> unser IP, wir bauen sie. Das ist ein Wedge, kein Nachbau.
- **TTS:** **ElevenLabs** fuer Premium-Stimmen/Voice-Clone, **Piper** (`OHF-Voice/piper1-gpl`, GPL-3.0, `pip install piper-tts`) gratis fuer self-hosted Alerts (echtzeitfaehig auf CPU). Donations/Bits/Subs vorlesen, Channel-Point "lass die KI was sagen". Die Bot-Logik (was wird vorgelesen, welche Stimme, Missbrauchs-Filter) gehoert uns.

**Wichtig:** Bei beiden bauen wir Schnittstelle + Logik, nie das Modell. Richtige Grenze.

---

### 2.6b AI-Moderator -> WRAP (mehrstufig, person-aware) - der grosse Wedge

**Entscheidung (Gespraech 2026-06-01):** Der Mod muss WIRKLICH verstehen, was abgeht. Aber "verstehen" heisst NICHT "alles durch die KI jagen". Wir bauen ihn **vierstufig**, von hart+schnell+gratis nach schlau+teuer. Jede Stufe faengt ab, was sie kann, und gibt nur den Rest weiter.

**ZWEI getrennte Mod-Bots (wichtig, Entscheidung Luca):**
- **Regel-Mod (immer, gratis, fuer jeden):** Stufen 1+2 (Scam-Schutz + Link-Kontrolle + klassische Regeln). Laeuft komplett lokal, ohne KI, ohne Kosten. Das ist die Basis - vergleichbar mit Nightbot/StreamElements, aber sauberer.
- **AI-Mod (optionales Feature, einschaltbar, BYOK):** Stufen 3+3b (KI mit Personen-Gedaechtnis + Poaching-Erkennung). Braucht eigenen Claude-Key (Self-Hoster zahlt selbst) bzw. ist im Cloud-Tier inklusive. Ohne KI funktioniert die Moderation weiter, nur weniger schlau.

So kriegt JEDER einen funktionierenden Mod (auch ohne KI/Kosten), und der AI-Mod ist der optionale Aufsatz fuer die, die mehr wollen.

**Stufe 1 - Scam-Schutz-Schalter (an/aus, von UNS gepflegt) [Regel-Mod]**
- Ein eigener Schalter, getrennt von den Listen: `🛡️ Scam-Schutz [an|aus]`.
- Wenn an: bekannte Betrugsmaschen sofort raus, **ohne KI, in Millisekunden**: "free/buy/cheap viewers", "get followers", Fake-Gewinnspiele, Account-Phishing ("dein Account wird gesperrt, klick hier"), bekannte Spam-Domains.
- **Wir pflegen diese Liste zentral** und liefern sie mit -> jeder Streamer profitiert automatisch bei neuen Maschen, ein Klick, kein eigener Aufwand. Starkes "funktioniert sofort"-Argument.
- Abschaltbar (z.B. Tech-Streamer, der ueber genau diese Scams redet).

**Stufe 2 - Link-Kontrolle (Black/Whitelist, vom Streamer) [Regel-Mod]**
- Grund-Modus waehlbar: **(a)** alle Links erlaubt, **(b)** alle gesperrt ausser Whitelist, **(c)** alle erlaubt ausser Blacklist.
- Whitelist + Blacklist pflegbar (youtube.com erlauben, Konkurrenz sperren, etc.).
- **Domain-genau, nicht Text-Match (kritisch fuer "gut gebaut"):** `youtube.com` auf der Whitelist erfasst `www.youtube.com`, `youtu.be`, `m.youtube.com` - aber NICHT `youtube.com.scam.ru` (der klassische Spammer-Trick gegen Wortlisten). Wir parsen die echte Domain.
- Wildcards (`*.deineseite.de`), optional pro Plattform getrennt.
- **Rollen-Ausnahmen:** Mods/VIPs/Stammzuschauer duerfen Links, neue Accounts nicht -> faengt ~90% des Link-Spams ohne echte Viewer zu nerven.

**Pruef-Reihenfolge (entscheidend, Sicherheit gewinnt):** Scam-Regeln -> Blacklist -> Whitelist -> Grund-Modus. So kann eine Whitelist niemals versehentlich Scam/Blacklist aushebeln.

**Stufe 3 - KI mit Personen-Gedaechtnis (die Graufaelle) [AI-Mod, optional/BYOK]**
- Nur was Stufe 1+2 durchlassen und trotzdem grenzwertig ist, geht an Claude - mit vollem Kontext: letzte ~50 Nachrichten + **wer ist die Person** (Account-Alter, Historie, bisheriges Verhalten).
- **Person-aware (Lucas Kern-Anforderung):** Bringt "DerHorst42" zum 5. Mal seinen schwarzen Humor und die Community lacht (keine Reports), **lernt der Mod: bei dem ist das Stil, nicht toxisch** - flaggt es nicht mehr vorschnell. Derselbe Satz von einem brandneuen Account wird anders bewertet. Der Mod fragt nicht "ist dieser Satz boese?", sondern "ist dieser Satz **von dieser Person** in **diesem Verlauf** boese?". Geht nur, weil wir den Memory-Layer haben - kein anderer Bot kann das.

**Stufe 3b - Poaching/Lure-Erkennung (kontextabhaengig) [AI-Mod, optional/BYOK]**
- "Can I join you?", "join my discord", ungefragte Call-Anfragen: harmlose Woerter, NICHT listbar (echte Viewer fragen das legitim).
- Der Unterschied ist messbar: Scammer macht es **sofort, kalt, ohne Vorgeschichte**; echter Viewer **nach echtem Interesse** (redet ueber Spiel, interagiert).
- Regel: verdaechtig = (Anfrage Leute woanders hinzuziehen) **UND** (kein echter Vorlauf: frischer/stiller Account, keine Interaktion, kalter Einstieg). **Beide** Bedingungen noetig. Plattformuebergreifend: schreibt er dasselbe in 10 Streams, wissen wir das.

**Eskalations-Stufen (einstellbar):** `nur melden` ("verdaechtig, schau mal") / `loeschen` / `timeout`. **Default anfangs vorsichtig (nur melden)**, bis der Mod die Community gelernt hat - nichts ist schlimmer als ein Bot, der echte Fans rauswirft. Mit der Zeit mehr Autonomie.

**🔴 DSGVO-Hinweis:** Person-aware-Moderation speichert + bewertet Viewer-Verhalten ueber Zeit = personenbezogene Daten (Art. 4). Machbar (Grundlagen im Master-Plan), aber transparent + mit Loesch-Moeglichkeit bauen. Frueh mitdenken.

**Aufwand:** Mittel-Hoch. Regel-Engine (Stufe 1+2) + KI-Schicht mit Memory-Anbindung (Stufe 3). Echter Wedge - Twitch-AutoMod (stumpfe Wortliste) und alle anderen versagen hier komplett.

---

### 2.6c Sponsor-/Ad-Management -> BUILD (Aushaengeschild-Wedge)

**Entscheidung (Gespraech 2026-06-01):** Selbst bauen, MUSS top sein. Loest einen echten, unterversorgten Schmerz: kleine Streamer kriegen Sponsoren-Deals, haben aber kein Werkzeug, sie sauber abzuwickeln (vergessen Ad-Reads, verlieren Link-Ueberblick, haben keine Zahlen fuer den Sponsor). Scope = **A + B + C jetzt, D geparkt.**

**A) Ad-Read-Reminder [bauen]**
Streamer traegt ein "heute NordVPN-Spot faellig". Bot erinnert diskret im Stream (zeitbasiert "alle 90 Min" oder per Knopf im passenden Moment). Loest den haeufigsten Weg, einen Sponsor zu veraergern (vergessener Read). Einfach zu bauen.

**B) Sponsor-Slot-Planung [bauen]**
Simpler Kalender: welcher Sponsor wann, wie viele Reads im Deal vereinbart, wann Vertrag auslaeuft. "3 von 8 NordVPN-Reads diesen Monat gemacht." Loest den Ueberblick ueber mehrere parallele Deals.

**C) Affiliate-Link-Tracking [bauen] - der eigentliche Wedge**
Streamer hinterlegt Affiliate-Links. Bot postet sie auf `!nordvpn` in den Chat und **zaehlt die Klicks**. Streamer + Sponsor sehen, was performt. **Das ist die Zahl, die Sponsoren wollen und die kleine Streamer nie haben.**
- **Technik-Hinweis:** Klick-Zaehlung braucht einen eigenen Redirect (Kurz-URL, die durchzaehlt und weiterleitet). Kleines Stueck Infra, machbar, aber bewusst einplanen. Self-Hosted: laeuft ueber den lokalen Server; Cloud: ueber unsere Domain.

**D) Media-Kit-Generierung [PARK]**
Auto-generierte PDF/Seite: durchschnittliche Viewer, Reichweite, Demografie, vergangene Sponsor-Performance, die der Streamer neuen Sponsoren schickt. Baut auf den Analytics-Daten auf, die wir erst sammeln muessen -> Phase 2.

**Aufwand:** A+B niedrig, C mittel (wegen Redirect-Infra). Klarer Differenzierer, macht fast niemand fuer kleine Streamer.

---

### 2.7 Donations/Tips -> INTEGRATE (niemals selbst)

**Entscheidung:** An Ko-fi (Webhook) oder StreamElements (WebSocket) anbinden. Niemals Merchant-of-Record werden.

Der saubere Pfad: Ein Drittanbieter haelt das Geld, wir empfangen ein Event. Wir beruehren nie Kartendaten, halten nie Gelder, loesen nie PCI-DSS/KYC/AML aus.

- **Ko-fi:** Webhook (POST mit `verification_token`). Payload hat `from_name`, `amount`, `message`, `currency`. Kein OAuth, nur Token-Validierung. Braucht aber einen erreichbaren HTTPS-Endpoint (oder Tunnel). **Einfachste Anbindung.**
- **StreamElements:** WebSocket (`wss://realtime.streamelements.com`, Socket.IO, JWT). Reicherer Event-Stream - deckt auch Subs/Follows/Cheers ab. Selbe Mechanik wie deren eigene Overlays.
- **NICHT Stripe/PayPal direkt:** Das IST der "werde-zum-Payment-Processor"-Pfad. PCI-DSS, KYC/AML, Chargebacks, Steuer-Reporting, Account-Freezes. Genau deshalb existieren Ko-fi/StreamElements. Stripe nur im **Cloud-Billing** (da sind wir ohnehin das Geschaeft).

**Aufwand:** Niedrig. Webhook-Handler + Event-Mapping in die Alert-Engine.

---

### 2.8 DMCA-Musik / Now-Playing -> GESTRICHEN

**Entscheidung (Gespraech 2026-06-01): komplett raus.** Dafuer gibt es YouTube etc., und das Feld ist juristisch ein Minenfeld:
- **Spotify:** Developer-Policy **verbietet** Now-Playing-Overlays explizit (dokumentierte C&D).
- **Pretzel:** keine API, nur lokale Datei.
- **Soundtrack by Twitch:** 2023 tot. **Epidemic Sound:** nur B2B-Enterprise.

Kein sauberer Pfad, kein Wert den Aufwand/das Risiko wert. Gestrichen. (Song-**Requests** als Queue-Feature bleiben - das ist Bot-Logik, nicht Musik-Lizenzierung.)

---

### 2.9 Going-Live Auto-Post -> INTEGRATE (nur Discord)

**Entscheidung (Gespraech 2026-06-01):** Nur **Discord**. Der Bot sendet beim Streamstart automatisch eine Nachricht in den konfigurierten Discord-Channel.

- **Discord-Webhook:** trivial, gratis, kein App-Review, kein OAuth (Webhook-URL ist das Credential). Genau der gewuenschte Use-Case.
- **Bluesky: gestrichen** (war nur optionales Extra, nicht gewuenscht).
- X/Twitter ($0.20/Post mit Link), Instagram (kein Story/Reel per API, App-Review), TikTok (video-only, kein Text-Post) - alle nicht im Scope.

**Aufwand:** Niedrig. Ueberlappt mit der geplanten Announcement-Funktion.

<details>
<summary>Recherche-Referenz (verworfene Social-Optionen, Stand Juni 2026)</summary>

| Plattform | Machbar? | Kosten | Auth-Friktion |
|---|---|---|---|
| Discord-Webhook | Ja, trivial | Gratis | Keine (Webhook-URL) |
| Bluesky (AT Protocol) | Ja, einfachste | Gratis | App-Password, kein Review |
| X/Twitter v2 | Ja | **$0.20/Post mit URL** | OAuth pro User |
| Instagram Graph | Muehsam | Gratis | App-Review + Pro-Account, nur Feed-Posts |
| TikTok | Falscher Fit | Gratis | App-Review, nur Video, kein Text |

Discord-Webhook und Bluesky waeren technisch beide gratis/easy. Entscheidung Luca: nur Discord. X funktioniert, kostet aber ~$6/Monat bei taeglichem Go-Live. Instagram (kein Story/Reel per API) und TikTok (video-only, kein Text-Post) sind fuer "Going Live"-Ankuendigungen ungeeignet.

</details>

---

### 2.10 Discord-Sub-Sync -> WRAP

**Entscheidung:** Standard-Discord-API, kein Partner-Status. Wir haben `discordgo` schon im Stack.

Zwei Schritte: (1) Twitch-Sub-Event via EventSub empfangen, (2) Discord-Rolle vergeben via `PUT /guilds/{guild}/members/{user}/roles/{role}`.

**Voraussetzungen:**
- Bot braucht `MANAGE_ROLES` und muss in der Rollen-Hierarchie ueber der zu vergebenden Rolle stehen.
- User muss den Bot einmal mit `identify` + `guilds.join` autorisieren (Identity-Linking: Twitch-ID <-> Discord-ID mappen).

Das Identity-Linking ist Pflicht-UX (genau wie StreamElements/Mee6 es machen: "Connect Discord"-Button). Kein Sonderstatus noetig - reine Standard-API.

**Aufwand:** Klein, aber Community-Kleber. Identity-Mapping-Store + Link-Flow.

---

### 2.11 Park-Liste (dokumentiert vertagt)

- **Multistreaming/Restream (#16):** Video-Fan-out ist bandbreiten-/compute-teuer. Pass-Through-Relay machbar (~4-6 Wochen + Bandbreitenkosten), echtes Transcoding ist eine Infra-Investition. Cloud-only, Phase 3-4. Tech zu evaluieren: MediaMTX (Go, passt zum Stack). Beruehrt das Anti-Pattern "kein Video-Encoder" - Grenze: Output-Distribution, nicht Szenen-Komposition.
- **Cross-Streamer-Loyalty (#11):** DSGVO Art. 26 Joint-Controller, expliziter Consent pro Viewer pro Streamer-Pair. Experimentell, Phase 4, deprecaten falls <50 Streamer.
- **Addon-Marketplace (#14):** Groesste Angriffsflaeche im ganzen Produkt. Permission-Sandbox + Code-Signing + Review-Gate + lokaler Companion-Agent. 10-16 Wochen, mehrphasig. Der eigentliche Moat, aber erst wenn die Foundation steht.

---

## 2c. AI-Co-Host + KI-Feature-Landschaft (recherchiert 2026-06-01)

Anlass: Frage nach **ai_licia** und "anderen coolen KI-Features". Ergebnis: ai_licia ist genau das, was unsere Features #5 (AI Co-Host) + #15 (AI-Voice/TTS) abdecken. Wir haben die Kategorie also NICHT uebersehen - die Frage ist, ob wir es so gut/besser machen.

### Was ai_licia ist (und unsere Luecke dagegen)

ai_licia = **cloud-only, closed-source SaaS** KI-Co-Host (~$10-30/Monat): persistente KI-Persoenlichkeit, liest Chat (Twitch/YT/Kick), reagiert auf @mentions, Viewer-Gedaechtnis, TTS-Stimme, Channel-Point-Trigger, OBS-Overlay mit Avatar, Mod-Assist. **Kein Self-Hosting, keine API, kein Voice-Besitz.**

**Unsere Luecke, die ai_licia NICHT fuellt (= unser Pitch):** Open Source, self-hostbar, **streamer-eigene Stimme** (nicht an Cloud-Persoenlichkeit gekettet), laeuft auf Linux, in den Gesamt-Bot integriert (nicht separates Tool daneben), keine Monatsgebuehr im Self-Host. Das ist die komplette Value-Proposition.

### KI-Co-Host: build vs. integrate (Stack)

Der Co-Host ist KEIN Monolith, sondern eine Pipeline. Modell anbinden, Orchestrierung bauen:

```
Mikro -> [VAD] -> [STT] -> [Kontext: Sprache + Chat-Highlights + Memory] -> [LLM] -> [Streaming-TTS] -> [Audio-Routing] -> OBS
```

| Baustein | Strategie | Werkzeug |
|---|---|---|
| VAD (Voice Activity Detection) | **WRAP** | `silero-vad` (MIT, lokal, exzellent) |
| STT (Streamer hoeren) | **WRAP** | whisper.cpp (lokal/gratis) / Deepgram (Cloud, schnell) |
| LLM (Persoenlichkeit) | **WRAP** | Claude Haiku (vorh. Sub) / lokaler Llama 3.1 8B als Self-Host-Fallback |
| TTS (Stimme/Clone) | **WRAP** | ElevenLabs / **Inworld** (#1 TTS-Leaderboard 5/2026, P90 130ms, billiger at scale) / Piper (self-host) |
| **Streaming-TTS-Orchestrierung** | **BUILD** | Satz-Splitter -> TTS-Queue -> Audio-Out. Der Klebstoff, den keiner gut packt |
| **Viewer-Memory** | **BUILD** | KV-Store pro Username + optional Vektor-Suche. SQLite reicht |
| **Chat-Highlight-Filter** | **BUILD** | Heuristik (mentions/donations/hype) + optional LLM-Scoring |
| Audio-Routing | **DOKU** | VB-Cable (Win) / BlackHole (mac) / PipeWire (Linux). OS-Tools, nicht bauen |
| OBS-Overlay (Avatar) | **BUILD** | Browser-Source + WS. Differenzierer ggü ai_licia (wir besitzen die UI) |

**Die harten Teile (frueh einplanen):** (1) VAD ohne Echo (KI darf sich nicht selbst hoeren -> Feedback-Loop; Mic-Ducking waehrend TTS), (2) Streaming-TTS (erste Saetze synthetisieren waehrend LLM noch generiert, sonst >1s Latenz), (3) Audio-Routing auf Linux (PipeWire, unterdokumentiert - bewusst loesen, weil self-hosted). Realistisch Cloud-Stack 400-700ms, sub-300ms nur mit lokalem STT + schnellem LLM + Streaming-TTS.

**OSS zum Lernen (NICHT forken, Architektur studieren):**
- `moeru-ai/airi` (MIT, 40k Stars) - ernsthaftester Neuro-sama-Klon; deren `unspeech` (STT/TTS-Proxy-Abstraktion) ist genau unsere noetige Schicht.
- `Open-LLM-VTuber/Open-LLM-VTuber` (MIT, 8k) - vollstaendigster self-hostbarer AI-VTuber; riesige STT/TTS/LLM-Backend-Matrix als Referenz.
- `emqnuele/projectBEA` - architektonisch am naechsten an uns (OBS-WS + RAG-Memory + Plugin-Skills + FastAPI).

### Andere KI-Features: REAL vs. HYPE (was wir nehmen)

**REAL (bauen/anbinden, wenn Co-Host steht):**
- **AI-Clip/Highlight-Detection** - schon auf unserer Liste (#1). Einfacher Audio-Spike + Chat-Spike-Korrelator reicht, kein ML-Modell noetig.
- **AI-Chat-Summary "was hab ich verpasst"** - LLM + Chat-Log, klein, ueber vorhandene Claude-Sub. **Kandidat fuer Aufnahme.**
- **AI-Stream-Titel/Tags** - LLM, trivial. Nice-to-have.
- **Vibe/Sentiment-Meter** - kein grosses Produkt macht es; Chat-Sentiment + Overlay. DIY-Wedge moeglich.

**SKIP/PARK (Hype oder schlechtes Verhaeltnis):**
- **Realtime-AI-Image-Gen auf Channel-Point** - Latenz 3-15s, NICHT live-tauglich. Spaeter als async "Art-Redemption" (Ergebnis kommt verzoegert).
- **AI-Voice-Changer** - eigenes Feld (VoiceMod/NVIDIA Broadcast), nicht unser Kern. Skip.
- **AI-Thumbnails** - Tools existieren, Qualitaet inkonsistent, Streamer kuratieren eh manuell. Skip.
- **Voll-LLM-Moderation** - zu langsam/teuer bei Volumen. Unser zweistufiger Mod (Regeln + KI nur Graufaelle) ist die richtige Antwort, nicht "alles durch LLM".

### Einordnung in den Plan

Co-Host bleibt **Cloud-only Premium** (#5, Audio-Routing zu komplex fuer Mainstream-Self-Host), aber die **TTS-Persoenlichkeits-Stufe (#15)** ist die einfachere BYOK-self-hostbare Einstiegsform. Neu aufzunehmen erwaegen: **AI-Chat-Summary** ("was hab ich verpasst", billig ueber Claude-Sub) und **Vibe-Meter** (DIY-Wedge). Rest geskippt/geparkt wie oben.

---

## 2b. Cloud vs. Self-Hosted (welches Feature wohin)

**Leitlinie:** Self-hosted = alles, was auf einem normalen Rechner/Server laeuft. Cloud = alles, was **zentrale Infrastruktur, geteilte Daten ueber viele Streamer, oder gebuendelte laufende Kosten** braucht. Das ist die Netdata-Logik: der Self-Hoster kriegt sofort ein volles Tool, keine kastrierte Demo - aber die paar Cloud-only-Features sind die Monetarisierungs-Bruecke.

### Drei Schubladen

**1. Laeuft ueberall (Self-Hosted = Cloud, die grosse Mehrheit):**

| Feature | Hinweis |
|---|---|
| OBS-Steuerung | reine lokale Verbindung |
| Alerts/Overlays | lokaler Webserver reicht |
| Channel-Points / EventSub | nur API-Anbindung |
| Action-Engine | Kernlogik, gehoert ueberall hin |
| Pity / Streak / Wrapped / Live-Ops / Moments | pure Logik, groesstenteils live |
| Sponsor-/Ad-Management | pure Logik |
| AI-Mod (Regel-Stufen 1+2) | harte Regeln laufen lokal, ohne KI-Kosten |
| Discord-Sub-Sync | Standard-API |
| Going-Live-Post (Discord) | Webhook, trivial |
| Song-Requests | eigene Queue |

**2. Hybrid mit BYOK (alles mit KI-Kosten):**

| Feature | Self-Hosted | Cloud |
|---|---|---|
| Chat-Uebersetzung | vorhandene Claude-Sub / eigener Key | inklusive |
| AI-Mod (KI-Stufe 3, Graufaelle) | eigener Claude-Key (oder unsere Sub) | inklusive |
| AI Auto-Clipper | BYOK / lokale CPU-Detection | inklusive, optimiert |
| TTS-Stimmen | Piper gratis / ElevenLabs-Key | inklusive |

Prinzip: Self-Hoster bringt eigenen Key ODER nutzt die Gratis-Variante (Piper, lokales Modell) mit Latenz-/Qualitaets-Kompromiss. So zahlt nie jemand fuer KI, die er nicht nutzt - und Cloud hat trotzdem ein Verkaufsargument (kein Setup).

**3. Nur Cloud (die Monetarisierungs-Bruecke):**

| Feature | Warum nur Cloud |
|---|---|
| **AI Co-Host (Voice-Clone, <300ms)** | Audio-Routing/WebRTC zu komplex fuer Mainstream-Selbstbau |
| **Restream / Multistreaming** | Heim-Upload reicht physisch nicht fuer Fan-out an N Plattformen; Bandbreite/Transcode = bewusste Infra-Investition. **Wird gut gebaut, bleibt Cloud-only.** |
| **Cross-Streamer-Loyalty** | braucht zentrale DB ueber alle Streamer |
| **Cross-Stream-Analytics** | dito, zentrale Daten |

Diese vier teilen ein Merkmal: **entweder zentrale Infrastruktur oder zentrale Daten.** Ein Heim-Selbsthoster kann sie physisch nicht leisten - deshalb sind sie der natuerliche, ehrliche Grund fuers Cloud-Abo (kein kuenstliches Beschraenken). Restream zuerst als **Pass-Through-Relay** (Stream 1:1 an alle Ziele), echtes Transcoding (eine Aufloesung rein, mehrere raus) ist die teure Ausbaustufe spaeter.

**Monetarisierungs-Balance:** Nicht zu viel hinter die Paywall (sonst "Lock-in"-Gefuehl), aber genug dass Cloud sich lohnt. Diese vier + "kein Setup, kein eigener Server" sind die Bruecke.

---

## 3. Empfohlene Reihenfolge (nach Wert/Aufwand)

**Jetzt bauen (Pflicht + niedriger Aufwand, hoher Wert):**
1. **Action-Engine-Grundgeruest** (BUILD) - alles andere haengt dran. Plugin-Registry von Tag 1.
2. **OBS-Steuerung** (WRAP, `goobs`) - 1 Woche, sofort Streamer.bot-Konkurrent.
3. **EventSub + Channel-Points-Trigger** (WRAP, `helix`) - die Trigger-Schicht.
4. **Alerts/Overlays vervollstaendigen** (BUILD) - Foundation steht schon.

**Dann der Wedge (mittlerer Aufwand, echtes Differenzierungsmerkmal):**
5. **AI-Mod (mehrstufig, person-aware)** (WRAP: Regeln + Claude) - der grosse Wedge, Twitch-AutoMod versagt hier.
6. **Sponsor-/Ad-Management** (BUILD) - unterversorgt, klarer Wedge fuer kleine Streamer, MUSS top sein.
7. **Chat-Uebersetzung** (WRAP: vorhandene Claude-Sub, gratis) - klein, sofort machbar.
8. **Discord-Sub-Sync** (WRAP, `discordgo`) - klein, Community-Kleber.

**Anbinden, wenn Foundation steht (niedriger Aufwand, Commodity):**
9. **Donations** (INTEGRATE: Ko-fi/StreamElements) - Webhook/WS empfangen.
10. **Going-Live Auto-Post** (INTEGRATE: nur Discord-Webhook) - gratis, einfach.
11. **Song-Requests** (BUILD) - eigene Queue-Logik.

**Vertagen bis nach Validierung:**
12. Multistreaming/Restream, Cross-Streamer-Loyalty, Addon-Marketplace (PARK).

---

## 4. Konkrete Library-/API-Referenz (Stand Juni 2026)

| Zweck | Werkzeug | Version/Stand | Lizenz | Stack-Fit |
|---|---|---|---|---|
| OBS v5 | `andreykaipov/goobs` | v1.8.3 (2026-04) | MIT | Go nativ |
| Twitch Helix + EventSub | `nicklaw5/helix` | v2.34.0 (2026-04) | MIT | Go nativ (schon geplant) |
| Twitch Chat/IRC | `gempir/go-twitch-irc` | v4.4.1 (2026-03) | MIT | Go nativ (schon geplant) |
| Discord | `bwmarrin/discordgo` | aktuell | BSD-3 | Go nativ (im Stack) |
| WebSocket (Overlays/EventSub) | `coder/websocket` | aktuell | ISC | Go nativ (im Stack) |
| Chat-Uebersetzung + AI-Mod + Auto-Title | Claude `claude-haiku-4-5` via `anthropic-proxy` | vorhanden (Sub) | - | gratis, im Stack |
| TTS Premium | ElevenLabs | Credit-basiert | API | BYOK/Cloud |
| TTS self-host | `OHF-Voice/piper1-gpl` | v1.4.2 (2026-04) | GPL-3.0 | Sidecar |
| Donations | Ko-fi Webhook / StreamElements WS | gratis | API | INTEGRATE |
| Going-Live | Discord-Webhook (nur Discord) | gratis | API | INTEGRATE |
| Billing (Cloud) | `stripe/stripe-go` | aktuell | MIT | nur Cloud |

**Referenz-Architektur (Open Source zum Lernen):** Firebot (`Firebottle/Firebot`, GPL-3.0, sehr aktiv) - bester Open-Source-Beleg fuer das lokale-WS-Overlay + Event/Effect-Registry-Muster. Streamer.bot ist nur source-available (kein echtes OSS).

---

## 5. Scam-Liste: Beschaffung + Lizenz (recherchiert 2026-06-01)

**Befund:** Es gibt KEINE fertige, permissiv-lizenzierte Twitch-spezifische Scam-Wortliste. Phrasen sind ohnehin Fakten/Daten (nicht urheberrechtlich schuetzbar) - wir kompilieren unsere eigene Liste, geseedet aus Community-Wissen. Domain-Listen sind gut abgedeckt durch MIT/GPL-Projekte.

**Drei-Schichten-Strategie (genau wie pajbot/Nightbot: Link-Block + erweiterbare Liste):**

| Schicht | Inhalt | Quelle | Wie |
|---|---|---|---|
| **1. Phrasen** | "free/buy/cheap viewers", "become famous", "we promote twitch", "t.me/", unsolicited "discord.gg/" | selbst schreiben, Seed aus `zlKxZukii/Website` (Referenz) + `Bocon778/TwitchScamDomains` | **im Repo bundeln** `data/scam_phrases.txt` |
| **2. Streaming-Scam-Domains** | streamboo.com, dogehype.com, nezhna.com, fake-steam-Domains etc. (~16 Seed) | `Bocon778/TwitchScamDomains` (als Seed, keine Lizenz -> nur Referenz, eigene Liste) | **im Repo bundeln** `data/streaming_scam_domains.txt` |
| **3. Allg. Phishing-Domains** | ~496K aktuelle Phishing-Domains | `Phishing-Database/Phishing.Database` (**MIT**, ~11 MB, mehrmals taeglich) | **Runtime-Fetch** + Cache, in `map[string]struct{}` (O(1)) |

**Lizenz-Entscheidung:**
- Schicht 1+2 selbst gebaut (Fakten/Daten), im Repo mitversioniert - kommt mit jedem Update mit, genau wie Luca wollte.
- Schicht 3: `Phishing-Database/Phishing.Database` ist **MIT** = voll kompatibel mit AGPL/Apache, bundle- ODER fetchbar. Wegen 11 MB Groesse: **Runtime-Fetch mit lokalem Cache** (Self-Hoster ohne Verbindung nutzt Schicht 1+2 + letzten Cache).
- Optional Schicht 4: `jarelllama/Scam-Blocklist` Light (18K, GPL-3.0) als gebundelter Snapshot mit Attribution moeglich (Daten, nicht Code -> GPL infiziert das Go-Binary nicht).
- **NICHT nutzen:** OpenPhish-Free (non-commercial), PhishTank (Redistribution verboten), `Discord-AntiScam` (keine Lizenz).

**Verworfen fuer Bundling (keine/restriktive Lizenz):** `Discord-AntiScam/scam-links`, `Bocon778` (nur als Phrasen-Seed), OpenPhish, PhishTank.

**Update-Mechanismus (Lucas Frage geklaert):** Schicht 1+2 im Repo (kommt mit Software-Update). Schicht 3 optionaler Auto-Fetch beim Start/taeglich, mit Cache als Fallback. So funktioniert der Scam-Schutz auch offline mit den gebundelten Listen.
