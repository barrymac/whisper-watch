# whisper-watch

Self-hosted audio translation pipeline. Translates voice messages to English via Whisper large-v3 on a local GPU. Delivers translations to Telegram and WhatsApp without any data leaving your cluster.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  k8s namespace: whisper-watch                                       │
│                                                                     │
│  ┌──────────────┐   POST /v1/translate    ┌─────────────────────┐  │
│  │ whisper-bot  │ ──────────────────────▶ │     speaches        │  │
│  │   (Go)       │ ◀────────────────────── │  Whisper large-v3   │  │
│  │              │   {text: "..."}         │  GPU / CUDA         │  │
│  │ • REST API   │                         └─────────────────────┘  │
│  │ • Telegram   │                                                   │
│  │ • WA webhook │   webhook MESSAGES_UPSERT                        │
│  └──────────────┘ ◀──────────────────────┐                         │
│         │                          ┌─────┴──────────────────────┐  │
│         │ sendText (self-message)  │  evolution-api             │  │
│         └────────────────────────▶ │  WhatsApp bridge (Baileys) │  │
│                                    │  LAN: <your-evolution-ip>  │  │
│                                    └────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
         │                    │
         ▼                    ▼
    Telegram DM          WhatsApp self-message
    (your account)       (📞 Contact:\n\nTranslation)
```

**External dependencies** (pre-existing, not managed by this chart):
- PostgreSQL — for Evolution API session storage
- Redis — dedicated pod in this namespace (`redis.enabled: true` by default)
- GPU node — CUDA-capable with `nvidia.com/gpu.present: "true"` label

## Components

| Component | Image | Purpose |
|-----------|-------|---------|
| `speaches` | `ghcr.io/speaches-ai/speaches:0.8.3-cuda-12.4.1` | Whisper inference, OpenAI-compatible API |
| `whisper-bot` | `your-registry/whisper-bot` | REST API, Telegram bot, WhatsApp webhook |
| `evolution-api` | `your-registry/evolution-api:2.3.7` | WhatsApp Web bridge |
| `redis` | `redis:7-alpine` | Session cache for evolution-api |

## Deploy with Helm

### 1. Pre-flight: create PostgreSQL database

```bash
kubectl exec -n <pg-namespace> deploy/postgresql -- \
  psql -U <admin-user> -c "CREATE DATABASE evolution;"
kubectl exec -n <pg-namespace> deploy/postgresql -- \
  psql -U <admin-user> -c "CREATE USER evolution WITH PASSWORD '<pw>';"
kubectl exec -n <pg-namespace> deploy/postgresql -- \
  psql -U <admin-user> -c "GRANT ALL PRIVILEGES ON DATABASE evolution TO evolution;"
kubectl exec -n <pg-namespace> deploy/postgresql -- \
  psql -U <admin-user> -c "ALTER DATABASE evolution OWNER TO evolution;"
```

### 2. Create secrets

**Never commit secrets to git.** Create them manually once — Helm upgrade will never touch them.

```bash
# Telegram + owner WhatsApp number
kubectl create secret generic whisper-bot -n whisper-watch \
  --from-literal=TELEGRAM_BOT_TOKEN=<bot_token_from_botfather> \
  --from-literal=TELEGRAM_CHAT_ID=<your_telegram_numeric_id> \
  --from-literal=OWNER_PHONE=<whatsapp_number_e164_no_plus>

# Evolution API
kubectl create secret generic evolution-api -n whisper-watch \
  --from-literal=AUTHENTICATION_API_KEY=$(openssl rand -hex 32) \
  --from-literal=DATABASE_CONNECTION_URI="postgresql://evolution:<pw>@<pg-host>:5432/evolution" \
  --from-literal=CACHE_REDIS_URI="redis://<redis-host>:6379/0"
```

### 3. Configure your cluster

Copy the example local values file and fill in your cluster details:

```bash
cp chart/values.local.yaml.example chart/values.local.yaml
# edit chart/values.local.yaml — this file is gitignored
```

Key values to set:
- `clusterDomain` — your cluster DNS domain (e.g. `cluster.local`)
- `storageClass` — your cluster's storage class
- `whisperBot.image` / `evolutionApi.image` — your registry
- `whisperBot.ollamaURL` — optional, for LLM translation

### 4. Install / Upgrade

```bash
# First install
helm install whisper-watch ./chart -n whisper-watch --create-namespace \
  -f chart/values.local.yaml

# Subsequent upgrades — or use make ship
make ship
```

### 5. Build and push whisper-bot image

```bash
# Set your registry in Makefile.local (gitignored), then:
make ship   # builds :$(git rev-parse --short HEAD), pushes, helm upgrades
```

Or manually:
```bash
docker buildx build \
  --platform linux/amd64 \
  --push \
  -t your-registry/whisper-bot:$(git rev-parse --short HEAD) \
  .
```

### 6. Connect WhatsApp (QR scan)

Expose Evolution API on your LAN (LoadBalancer or NodePort). Then:

```bash
# Get the evolution API key from the secret
kubectl get secret evolution-api -n whisper-watch \
  -o jsonpath='{.data.AUTHENTICATION_API_KEY}' | base64 -d
```

**Create instance:**
```bash
curl -X POST http://<evolution-api-ip>:8080/instance/create \
  -H "apikey: <your-api-key>" \
  -H "Content-Type: application/json" \
  -d '{"instanceName": "my-instance", "qrcode": true}'
```

**Get QR code** — open in browser and scan with WhatsApp:
```
http://<evolution-api-ip>:8080/instance/qrcode/<instance>/base64?apikey=<your-api-key>
```

**Set webhook** (points to whisper-bot inside the cluster):
```bash
curl -X POST http://<evolution-api-ip>:8080/webhook/set/<instance> \
  -H "apikey: <your-api-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "enabled": true,
    "url": "http://whisper-bot.whisper-watch.svc.<cluster-domain>:8080/webhook/evolution",
    "events": ["MESSAGES_UPSERT"]
  }'
```

After connecting, get your instance ID and set it:
```bash
helm upgrade whisper-watch ./chart -n whisper-watch \
  -f chart/values.local.yaml \
  --set whisperBot.evolutionInstanceId=<uuid-from-fetchInstances>
```

## Local file watcher

Watches a directory for new audio files and POSTs them to whisper-bot. Result saved as `.txt` and delivered to Telegram.

```bash
pip install -r watcher/requirements.txt
python watcher/watcher.py
```

Copy `config.ini.example` to `config.ini` (gitignored) and configure:

```ini
[Local]
WatchDirectory = /home/user/Downloads/
ProcessedDirectory = /home/user/Downloads/whisper/translations
DownloadDirectory = /home/user/Downloads/whisper/downloads
AllowedExtensions = ogg,mp3

[Service]
URL = http://whisper-bot.whisper-watch.svc.<cluster-domain>:8080
```

## Telegram commands

```
/status          — dashboard: model, toggles, contact/message counts
/models          — list available ollama models with sizes
/model <name>    — switch LLM model at runtime

/contacts <q>    — search WhatsApp contacts
/groups          — list WhatsApp groups
/group <name>    — group details + members
/who <jid|name>  — resolve JID ↔ contact name

/mute <name>     — mute contact by name or JID
/unmute <name>   — unmute
/muted           — list muted contacts

/groups on|off   — toggle group message translation
/audio on|off    — toggle voice note translation
/texts on|off    — toggle text message translation
/replies on|off  — toggle LLM draft reply suggestions

/history <name>  — last N messages from a contact
/recent          — last N conversations
/summary         — LLM summary of today's messages
/translate <text>— English → Portuguese (Bahian) via LLM
```

All toggle state persists across restarts via Postgres (`ww_settings` table).

## REST API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/translate` | POST | Multipart audio upload → `{"filename":"...","text":"..."}` |
| `/webhook/evolution` | POST | Evolution API webhook receiver |
| `/healthz` | GET | Liveness — returns `ok` |
| `/readyz` | GET | Readiness — checks speaches backend |

## Upgrade safety

- **PVCs** (`whisper-model-cache`, `evolution-data`) annotated `helm.sh/resource-policy: keep` — survive `helm uninstall`
- **Secrets** referenced by name only — `helm upgrade` cannot overwrite them
- **State** persisted in Postgres — survives pod restarts and redeploys
- **Deployment strategy** `Recreate` — prevents dual Telegram polling conflicts

## Environment variables (whisper-bot)

| Variable | Source | Description |
|----------|--------|-------------|
| `SPEACHES_URL` | chart template | speaches service URL |
| `WHISPER_MODEL` | values.yaml | Whisper model (default: large-v3) |
| `WHISPER_LANGUAGE` | values.yaml | Source language (default: pt) |
| `TELEGRAM_BOT_TOKEN` | `whisper-bot` secret | Bot token from @BotFather |
| `TELEGRAM_CHAT_ID` | `whisper-bot` secret | Your Telegram numeric ID |
| `OWNER_PHONE` | `whisper-bot` secret | Your WhatsApp number (E.164, no +) |
| `EVOLUTION_API_URL` | chart template | evolution-api ClusterIP URL |
| `EVOLUTION_API_KEY` | `evolution-api` secret | Evolution API authentication key |
| `EVOLUTION_INSTANCE` | values.yaml | WhatsApp instance name |
| `EVOLUTION_INSTANCE_ID` | values.yaml | Evolution DB instance UUID |
| `OLLAMA_URL` | values.yaml | Ollama endpoint (optional) |
| `OLLAMA_MODEL` | values.yaml | Default LLM model |
| `DATABASE_URL` | `evolution-api` secret | Postgres DSN (for contact lookup + state) |
| `MUTE_GROUPS` | ConfigMap | Mute group messages on startup |
| `MUTED_JIDS` | ConfigMap | Comma-separated JIDs to mute on startup |
