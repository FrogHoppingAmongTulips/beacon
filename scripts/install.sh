#!/usr/bin/env bash
#
# beacon — установка self-hosted VPN (Xray VLESS+Reality) и веб-панели одной командой.
# Запускать от root на Debian/Ubuntu:
#   curl -fsSL https://<твой-хост>/install | bash
#
set -euo pipefail

BEACON_DIR="/etc/beacon"
BIN="/usr/local/bin/beacon"
# Репозиторий с релизами (замени на свой) — или переопредели через переменные окружения.
BEACON_REPO="${BEACON_REPO:-FrogHoppingAmongTulips/beacon}"
BASE_URL="${BEACON_URL:-https://github.com/${BEACON_REPO}/releases/latest/download}"
PANEL_PORT="${PANEL_PORT:-8443}"
VPN_PORT="${VPN_PORT:-443}"
SNI="${SNI:-www.microsoft.com}"

log()  { printf '\033[36m[beacon]\033[0m %s\n' "$*"; }
die()  { printf '\033[31m[beacon] %s\033[0m\n' "$*" >&2; exit 1; }

require_root() { [ "$(id -u)" = "0" ] || die "запусти от root (sudo bash …)"; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "архитектура $(uname -m) не поддерживается" ;;
  esac
}

ensure_deps() {
  command -v curl >/dev/null 2>&1 && return
  log "ставлю curl…"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y && apt-get install -y curl ca-certificates
}

install_xray() {
  if command -v xray >/dev/null 2>&1; then
    log "Xray-core уже установлен"
    return
  fi
  log "ставлю Xray-core…"
  bash -c "$(curl -fsSL https://github.com/XTLS/Xray-install/raw/main/install-release.sh)" @ install
}

install_beacon() {
  local arch; arch="$(detect_arch)"
  log "скачиваю beacon ($arch)…"
  if ! curl -fsSL "$BASE_URL/beacon-linux-$arch" -o "$BIN"; then
    die "не удалось скачать бинарник beacon. Проверь BASE_URL или собери из исходников: go build -o $BIN ./cmd/beacon"
  fi
  chmod +x "$BIN"
}

public_ip() {
  curl -fsSL https://api.ipify.org 2>/dev/null \
    || curl -fsSL https://ifconfig.me 2>/dev/null \
    || hostname -I | awk '{print $1}'
}

open_firewall() {
  if command -v ufw >/dev/null 2>&1 && ufw status | grep -q active; then
    ufw allow "${VPN_PORT}"/tcp   >/dev/null 2>&1 || true
    ufw allow "${PANEL_PORT}"/tcp >/dev/null 2>&1 || true
    log "порты ${VPN_PORT} и ${PANEL_PORT} открыты в ufw"
  fi
}

install_service() {
  cat >/etc/systemd/system/beacon.service <<EOF
[Unit]
Description=beacon VPN panel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN} serve
Restart=always
RestartSec=3
User=root
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
}

main() {
  require_root
  ensure_deps
  install_xray
  install_beacon
  mkdir -p "$BEACON_DIR"

  local ip; ip="$(public_ip)"
  log "первичная настройка (host=$ip)…"
  # setup генерит Reality-ключи, пароль, первого пользователя и печатает сводку с QR
  "$BIN" setup --host "$ip" --listen ":${PANEL_PORT}" --port "${VPN_PORT}" --sni "${SNI}" --force

  install_service
  systemctl enable --now xray   >/dev/null 2>&1 || log "предупреждение: не удалось запустить сервис xray"
  systemctl enable --now beacon >/dev/null 2>&1 || die "не удалось запустить сервис beacon"
  open_firewall

  echo
  log "готово. Панель: https://${ip}:${PANEL_PORT}"
  log "пароль и первый QR — выше в сводке setup. Логи: journalctl -u beacon -f"
}

main "$@"
