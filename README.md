# megane

megane - eyeglasses in Japanese

Go template for building web-based AI document-processing applications.

## Quickstart

```bash
cp .env.example .env
# Fill in GEMINI_API_KEY, SESSION_SECRET, ADMIN_EMAIL, ADMIN_PASSWORD
make run
# Open http://localhost:8080
```

Login with the admin credentials from `.env`. Upload files — the pipeline extracts text, calls the LLM, and stores a typed JSON result.

## Docker

```bash
cp .env.example .env   # set CADDY_DOMAIN, secrets, etc.
docker compose up -d --build
```

**Caddy** terminates HTTP/HTTPS on ports **80** and **443** and proxies to the app container. The app is not published on the host except internally on the compose network.

- **IP-only (AWS):** `CADDY_DOMAIN=:80` — open security group **80** (and **443** if you add a domain later).
- **Domain + TLS:** `CADDY_DOMAIN=app.example.com`, `CADDY_ACME_EMAIL=you@example.com`, DNS A record → instance.

Direct app port `8080` is not mapped to the host by default (use Caddy). For local debugging without Caddy, temporarily add `ports: ["8080:8080"]` on `app`.

**Ubuntu on AWS (EC2):** install Docker with `sudo ./install/ubuntu-docker-aws.sh` ([`install/README.md`](install/README.md)), then `docker compose up -d --build`.

## Creating a sprout-application

Start with **`AGENTS.md`** and **`sprout.index.json`** — they tell humans and agents exactly where to plug in domain logic (prompts, structured LLM output, UI) without exploring the whole tree.

Typical edits:

1. **`prompts/system.txt`** (and **`prompts/analyze.txt`** if you use it) — domain instructions for the model.
2. **`internal/models/pipeline_output.go`** — `PipelineLLMOutput` JSON shape + `description` tags (Gemini schema).
3. **`static/`** — branding and how results are shown (`js/analyze.js`).

See **`CLAUDE.md`** for full architecture.

## Crawler (optional)

`internal/crawler` wraps **Colly** (static HTML) and **go-rod** (JavaScript pages). Example against MAYA financial reports (needs Chromium on the machine; rod can provision one):

```bash
go run ./cmd/maya-financial-reports -out ./downloads/maya -headless=false
```

Visible window + DevTools + pause before scraping:

```bash
go run ./cmd/maya-financial-reports -headless=false -devtools -debug-hold=30s -out ./downloads/maya
```

Respect site terms of service and applicable law.
