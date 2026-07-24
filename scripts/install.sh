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
  if ! command -v apt-get >/dev/null 2>&1; then
    command -v curl >/dev/null 2>&1 || die "нужен curl — установи его вручную и повтори"
    command -v unzip >/dev/null 2>&1 || die "нужен unzip (его требует установщик Xray) — установи вручную"
    return
  fi
  export DEBIAN_FRONTEND=noninteractive
  # curl, unzip и ca-certificates нужны установщику Xray; ставим всегда, а не только когда нет curl
  log "обновляю списки пакетов и ставлю зависимости (curl, unzip, ca-certificates)…"
  apt-get update -y || die "apt-get update не удался — проверь сеть/репозитории сервера"
  apt-get install -y curl unzip ca-certificates || die "не удалось поставить curl/unzip/ca-certificates"
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
  local arch tmp; arch="$(detect_arch)"; tmp="${BIN}.new"
  log "скачиваю beacon ($arch)…"
  # качаем во временный файл и делаем atomic rename — запись прямо в $BIN
  # падает с ETXTBSY, если сервис beacon уже запущен и держит бинарник открытым
  if ! curl -fsSL "$BASE_URL/beacon-linux-$arch" -o "$tmp"; then
    rm -f "$tmp"
    die "не удалось скачать бинарник beacon. Проверь BASE_URL или собери из исходников: go build -o $BIN ./cmd/beacon"
  fi
  chmod +x "$tmp"
  mv -f "$tmp" "$BIN"
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
    log "порты ${VPN_PORT}/tcp и ${PANEL_PORT}/tcp открыты в ufw"
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
  # AmneziaWG временно отключена в установщике: PPA amnezia/ppa не поддерживает
  # свежие релизы Ubuntu (напр. 26.04 "resolute" — 404), ломает apt. Панель
  # умеет переключаться на amneziawg (beacon protocol amneziawg), но ставить
  # awg-quick пока нужно вручную.
  install_beacon           # всегда качает свежий бинарник — это же путь обновления
  mkdir -p "$BEACON_DIR"

  local ip fresh=0; ip="$(public_ip)"
  if [ -f "$BEACON_DIR/config.json" ]; then
    log "beacon уже настроен — обновляю бинарник, конфиг и ключи не трогаю"
  else
    fresh=1
    log "первичная настройка (host=$ip)…"
    # setup генерит Reality-ключи, пароль, первого пользователя и печатает сводку с QR
    "$BIN" setup --host "$ip" --listen ":${PANEL_PORT}" --port "${VPN_PORT}" --sni "${SNI}"
  fi

  install_service
  systemctl enable xray  >/dev/null 2>&1 || true
  systemctl restart xray >/dev/null 2>&1 || log "предупреждение: сервис xray не запустился"
  systemctl enable beacon >/dev/null 2>&1 || true
  systemctl restart beacon || die "не удалось запустить сервис beacon"
  open_firewall

  echo
  if [ "$fresh" = "1" ]; then
    log "готово. Панель: https://${ip}:${PANEL_PORT} — пароль и первый QR в сводке выше."
  else
    log "обновлено. Панель: https://${ip}:${PANEL_PORT} (пароль и ключи прежние)."
  fi
  log "версия: $("$BIN" version 2>/dev/null). Логи: journalctl -u beacon -f"
}

main "$@"
