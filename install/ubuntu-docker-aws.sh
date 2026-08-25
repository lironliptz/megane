#!/usr/bin/env bash
# install/ubuntu-docker-aws.sh
#
# Install everything needed to run Docker on Ubuntu (AWS EC2).
# Does not build or start the app — use docker compose in your project after this.
#
# Usage (Ubuntu 26.04 LTS on EC2 — also works on 24.04/22.04):
#   sudo ./install/ubuntu-docker-aws.sh
#
set -euo pipefail

log() { echo "[install] $*"; }
die() { echo "[install] ERROR: $*" >&2; exit 1; }

require_root() {
  if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
    die "Run as root: sudo $0"
  fi
}

check_ubuntu() {
  if [[ ! -f /etc/os-release ]]; then
    die "Cannot detect OS; this script targets Ubuntu on AWS EC2."
  fi
  # shellcheck source=/dev/null
  source /etc/os-release
  if [[ "${ID:-}" != "ubuntu" ]]; then
    die "Expected Ubuntu; found ${PRETTY_NAME:-unknown}. Aborting."
  fi
  log "OS: ${PRETTY_NAME} (${VERSION_CODENAME:-unknown})"
  case "${VERSION_ID:-}" in
    26.04) log "Target LTS: Ubuntu 26.04 Resolute Raccoon" ;;
    24.04|22.04) log "Supported LTS: ${VERSION_ID} (primary target is 26.04)" ;;
    *) log "WARNING: Untested Ubuntu ${VERSION_ID:-?}; script is written for 26.04 LTS (resolute)." ;;
  esac
}

install_docker() {
  log "Installing Docker Engine, Compose plugin, and buildx…"

  apt-get update -qq
  apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg

  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc

  # shellcheck source=/dev/null
  source /etc/os-release
  local suite="${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}"
  if [[ -z "$suite" ]]; then
    die "Could not detect Ubuntu suite (VERSION_CODENAME); expected resolute on 26.04."
  fi
  log "Docker apt suite: ${suite}"

  rm -f /etc/apt/sources.list.d/docker.list
  tee /etc/apt/sources.list.d/docker.sources >/dev/null <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: ${suite}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF

  apt-get update -qq
  apt-get install -y --no-install-recommends \
    docker-ce \
    docker-ce-cli \
    containerd.io \
    docker-buildx-plugin \
    docker-compose-plugin

  systemctl enable docker
  systemctl start docker
}

add_deploy_user_to_docker_group() {
  local u="${SUDO_USER:-ubuntu}"
  if id "$u" >/dev/null 2>&1 && ! groups "$u" | grep -q '\bdocker\b'; then
    usermod -aG docker "$u"
    log "Added user '$u' to group docker (log out/in to run docker without sudo)."
  fi
}

verify_docker() {
  log "Verifying installation…"
  docker --version
  docker compose version
  docker buildx version

  if docker info >/dev/null 2>&1; then
    log "Docker daemon is running."
  else
    die "Docker installed but daemon is not responding."
  fi
}

print_next_steps() {
  echo ""
  echo "=============================================="
  echo " Docker is ready on this instance."
  echo ""
  echo " Next (in your app directory):"
  echo "   cp .env.example .env    # edit secrets + PORT"
  echo "   docker compose up -d --build"
  echo ""
  echo " Or:  sudo ./docker_build.sh"
  echo ""
  echo " Non-root docker: log out and back in after this script."
  echo "=============================================="
}

main() {
  require_root
  check_ubuntu
  install_docker
  add_deploy_user_to_docker_group
  verify_docker
  print_next_steps
}

main "$@"
