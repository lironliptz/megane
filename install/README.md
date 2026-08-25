# megane

megane - eyeglasses in Japanese

Install **Docker only** on a fresh Ubuntu EC2 instance. App deploy (`docker compose up`, `.env`, etc.) stays in your project — use `docker_build.sh` or compose directly.

## Prerequisites

- Ubuntu **26.04 LTS** (Resolute Raccoon) on AWS EC2 — primary target
- Also works on **24.04** and **22.04** LTS

## Install Docker

```bash
sudo ./install/ubuntu-docker-aws.sh
```

Installs from Docker’s official apt repo:

- Docker Engine (`docker-ce`)
- CLI (`docker-ce-cli`)
- containerd
- Compose plugin (`docker compose`)
- Buildx plugin

Enables and starts the daemon, adds your SSH user (`ubuntu` / `SUDO_USER`) to the `docker` group.

## Run the app (after Docker is installed)

```bash
cd jump-starter   # or your sprout
cp .env.example .env
# Edit .env: GEMINI_API_KEY, SESSION_SECRET, ADMIN_EMAIL, ADMIN_PASSWORD, PORT

docker compose up -d --build
# or: sudo ./docker_build.sh
```

Security group: inbound **TCP 80** and **443** (Caddy). The app listens only inside Docker on `PORT` (default **8080**).

## Useful commands

```bash
docker compose logs -f
docker compose logs -f caddy
docker compose restart    # or ./docker_restart.sh
docker compose down
curl http://127.0.0.1/health          # via Caddy (:80)
curl http://127.0.0.1:8080/health     # only if you expose app port for debug
```

## Persistence on AWS

When you run compose, bind-mounts use host dirs:

- `./.db` — SQLite
- `./projects` — uploads

Keep these on EBS if you replace the instance.

## Sprouts

Jump-Start copies `install/` into new sprouts unchanged.
