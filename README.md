# beacon

Self-hosted VPN за одну команду. Протокол — VLESS + Reality (движок Xray-core),
управление — через минималистичную веб-панель. Никаких «зайти на сервер и
настроить»: поставил, получил ссылку на панель и первый QR, раздаёшь ключи с сайта.

```
curl -fsSL https://github.com/FrogHoppingAmongTulips/beacon/releases/latest/download/install.sh | bash
```

(и `install.sh`, и бинарники приходят из одного релиза)

## Что делает установка

1. Определяет ОС/архитектуру, ставит один бинарник `beacon` (панель + логика в одном файле).
2. Ставит и настраивает Xray-core (VLESS + Reality): генерит X25519-ключи, `shortId`, маскировку под чужой SNI.
3. `beacon setup` создаёт конфиг, автопароль и первого пользователя.
4. Поднимает systemd-сервисы `beacon` и `xray`, включает автозапуск.
5. Печатает в терминал: адрес панели, логин/пароль и QR первого ключа.

## Архитектура

```
cmd/beacon        точка входа + CLI (serve | setup | add-user | list | reset-password)
internal/config   постоянные настройки и секреты (config.json), автопароль
internal/metrics  CPU / RAM / сеть / аптайм из /proc, дельты между замерами
internal/store    пользователи в JSON-файле (users.json), потокобезопасно
internal/vpn      сборка ссылки vless://
internal/qr       генерация QR (PNG) из ссылки
internal/xray     Reality-ключи, генерация config.json для Xray, reload
internal/server   HTTP+TLS (самоподписанный), авторизация, REST API, SSE-стрим метрик
web               вшитая панель (go:embed) + клиент к API
scripts           install.sh, systemd-юниты
configs           шаблон конфига Xray для справки
```

Единственная внешняя зависимость — `github.com/skip2/go-qrcode`. Всё остальное на
стандартной библиотеке, поэтому бинарник собирается без cgo и не тянет зависимостей на сервере.

## Локальная сборка (для разработки)

```
make build        # бинарник под текущую ОС (go build с версией)
make run          # собрать и запустить панель
make vet          # go vet
make dist         # кросс-сборка dist/beacon-linux-{amd64,arm64} + checksums + install.sh
```

Метрики читаются из `/proc`, поэтому реальные значения будут только на Linux;
на macOS панель поднимется, но нагрузка покажет нули (для отладки верстки норм).

## Релизы (GitHub Actions)

- [.github/workflows/ci.yml](.github/workflows/ci.yml) — на каждый push/PR: `go vet`, сборка и кросс-сборка под linux amd64/arm64.
- [.github/workflows/release.yml](.github/workflows/release.yml) — на пуш тега `vX.Y.Z`: собирает статические бинарники (`CGO_ENABLED=0`) и публикует релиз с ассетами `beacon-linux-amd64`, `beacon-linux-arm64`, `checksums.txt`, `install.sh`.

Выпуск новой версии:

```
git tag v0.1.0
git push origin v0.1.0
```

После этого `curl -fsSL https://github.com/FrogHoppingAmongTulips/beacon/releases/latest/download/install.sh | bash`
поставит VPN на сервер одной командой. Версию собранного бинарника показывает `beacon version`.

## Доступ к панели

По умолчанию `https://<IP>:8443`, самоподписанный сертификат (браузер предупредит —
это ожидаемо), логин по автопаролю из `beacon setup`. Сбросить пароль: `beacon reset-password`.

> Статус: скелет проекта. Runtime-специфичные места (reload Xray, systemd) помечены
> в коде и рассчитаны на Linux-сервер.
