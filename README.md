# p2pnode

Прототип узла **защищённой децентрализованной оверлейной P2P-сети**,
работающей поверх TCP/IP. Узел обнаруживает других участников через
распределённую хэш-таблицу (Kademlia DHT), обменивается подписанными
записями и защищает канал связи аутентифицированным шифрованием
(собственный AKE на библиотечных примитивах), а также умеет строить
туннели через ретрансляторы с end-to-end шифрованием.

## Возможности

| Этап | Возможность |
|------|-------------|
| 1 | Транспорт: TCP, 24-байтовый формат кадра, диспетчеризация |
| 2 | Ed25519-идентичность, Kademlia DHT (PING, FIND_NODE), bootstrap, итеративный lookup |
| 3 | Подписанные `NodeRecord` (Ed25519), STORE/FIND_VALUE, репликация R=3, TTL, anti-rollback, псевдонимы |
| 4 | Собственный AKE (Ed25519 + X25519 + HKDF + ChaCha20-Poly1305), anti-replay |
| 5 | Туннели через ретрансляторы: E2E-шифрование, build-ACK, TTL, восстановление, пул |
| 6 | Docker Compose, 4 bootstrap-схемы (star/ring/tree/multiseed), N=15–21, сценарии отказа |
| 7 | Передача файлов, ограниченный underlay, tc/netem, профиль ретранслятора, Eclipse/Sybil защита |

## Требования

- **Go** 1.26 или новее.
- **Python 3** — для скриптов анализа, генерации отчёта, Sybil-identity.
  - **Python-пакет** `cryptography` — для E7-8 (`pip install cryptography`).
- **Docker** ≥ 20.10 + `docker compose` v2.
- **Graphviz** (опционально) — для визуализации графов.
- **bash**, `sha256sum`, `mktemp` — стандартные утилиты Linux.
- **`nsenter`** (util-linux) — для `tc`/`nft` внутри контейнеров.
- **`sudo`** — для применения `tc`/`nft` через `nsenter`.
- **tcpdump** (опционально) — для демонстрации E2E-защиты.

**Go-зависимости** (устанавливаются автоматически):

- `github.com/vmihailenco/msgpack/v5` — сериализация.
- `golang.org/x/crypto` — HKDF, ChaCha20-Poly1305.
- `filippo.io/edwards25519` — конвертация Ed25519 ↔ X25519.

Установка:

```bash
go mod download
pip install cryptography
```

## Быстрый старт

### Сборка и тесты

```bash
go build ./...
go test ./...
```

### Запуск одного узла

```bash
go run ./cmd/node \
    -state-dir /tmp/node-01 \
    -listen-host 127.0.0.1 \
    -listen-port 9001 \
    -log-level INFO
```

### Запуск второго узла с bootstrap

```bash
go run ./cmd/node \
    -state-dir /tmp/node-02 \
    -listen-host 127.0.0.1 \
    -listen-port 9002 \
    -bootstrap 127.0.0.1:9001 \
    -log-level INFO
```

### Полный эксперимент на N=15 (локально)

```bash
chmod +x scripts/*.sh
./scripts/run_experiment.sh 15 20
```

## Быстрый старт в Docker

```bash
# Один прогон: build + 15 узлов + 30 lookup'ов + отчёт
./scripts/run_experiment_docker.sh 15 star 30

# Открыть отчёт
xdg-open report.html
```

**Подробнее:** см. **[docs/DEPLOY.md](docs/DEPLOY.md)** — 4
bootstrap-схемы, скрипты управления, диагностика, ограничения.

## Демонстрационные сценарии

### 1. Обнаружение узлов

```bash
./scripts/run_star.sh 15
sleep 20
./scripts/collect_routing.sh
./scripts/check_ne_degenerate.sh
./scripts/stop_all.sh
```

### 2. STORE / FIND_VALUE

```bash
./scripts/demo_store.sh 5 15
```

**Важно:** `Publish` возвращает ошибку `ErrNotEnoughReplicas`, если
получено **меньше 2 подтверждений** из 3 (отказоустойчивость хранения).
`FindValue` возвращает запись с **максимальным `sequence_number`**
среди реплик (устаревшая реплика не перекрывает актуальную).

### 3. Защищённый канал

```bash
./scripts/demo_secure_channel.sh
```

Все RPC идут через handshake + AEAD. Трафик зашифрован
ChaCha20-Poly1305. **Единый дедлайн** (`HandshakeTimeout`, `RPCTimeout`)
на dial + handshake + write + read — не «10s на handshake + отдельный
read timeout».

### 4. Туннели через ретрансляторы

```bash
./scripts/demo_tunnel.sh
./scripts/demo_tunnel_recovery.sh
./scripts/demo_tunnel_pcap.sh
./scripts/demo_tunnel_pool.sh
```

**Детали:** см. **[docs/TUNNEL.md](docs/TUNNEL.md)**.

### 5. Docker: 4 bootstrap-схемы

```bash
./scripts/docker_up.sh 15 star
./scripts/docker_up.sh 15 ring
./scripts/docker_up.sh 15 tree
./scripts/docker_up.sh 15 multiseed

# Полное сравнение всех 4
./scripts/compare_4schemes_docker.sh 15 30
```

**Детали:** см. **[docs/DEPLOY.md](docs/DEPLOY.md)**.

### 6. Docker: сценарии отказа

```bash
# E6-5: работа без seed'а
./scripts/demo_seed_down.sh 15

# E6-6: 3 сценария отказа
./scripts/demo_failures.sh 7
```

**Дополнительно:** `Bootstrap` возвращает **ошибку**, если после
попыток присоединения к seed'ам routing-таблица **пустая** — явная
сигнализация «не удалось присоединиться к сети».

### 7. Docker: N=21 (продвинутый масштаб)

```bash
./scripts/run_experiment_n21.sh 21 40
```

### 8. Передача файла через туннель

```bash
# N=5 узлов, max-hops=2, файл 1 МБ
./scripts/demo_file_transfer.sh 5 1 2

# N=7 узлов, max-hops=3, файл 10 МБ
./scripts/demo_file_transfer.sh 7 10 3
```

**Что проверяется:** разбиение на чанки, ACK на каждый чанк,
SHA-256 всего файла, дедупликация. **Артефакт:** `visualization/file-transfer/`.

**Безопасность имени:** `Header.Name` проходит `SanitizeFileName` —
отклоняются `../`, абсолютные пути, `\x00`, Windows reserved
(`CON`, `NUL`, `COM1..9`, …), имена > 255 байт. Коллизии
разрешаются суффиксом `.1`, `.2` (`UniqueDestPath`).

### 9. Ограниченный underlay (nftables)

```bash
./scripts/demo_underlay.sh 7 star
```

**Что проверяется:** IP-достижимость между узлами ограничена
nftables-правилами. Пара «bootstrap-связь» → OK, «не bootstrap» →
BLOCKED.

**Артефакт:** `visualization/underlay/` (`summary.json`, `rules.json`,
`lookups/`).

### 10. Управляемая деградация (tc/netem)

```bash
# Отдельные режимы (рекомендуется)
./scripts/demo_netem.sh baseline 5
./scripts/demo_netem.sh delay-50 5
./scripts/demo_netem.sh loss-5 5
./scripts/demo_netem.sh stress 5

# Все 4 режима подряд
./scripts/demo_netem.sh all 7

# Сводная таблица
./scripts/demo_netem.sh summary
```

**Что проверяется:** влияние delay/loss на lookup latency, success
rate, file goodput.

**Артефакт:** `visualization/netem/` (`baseline/`, `delay-50/`,
`loss-5/`, `stress/`, `summary.json`).

**Типичные результаты:** `baseline ≈ 3ms`, `delay-50 ≈ 154ms`,
`loss-5 ≈ 124ms`, `stress ≈ 724ms` — подтверждают, что
единый дедлайн RPC применяется ко всему вызову.

### 11. Профиль ретранслятора

```bash
./scripts/demo_relay_selection.sh 9 10
```

**Что проверяется:** 3 режима выбора ретранслятора —
`ranked` (EMA-score), `random`, `subnet` (ranked + subnet-diversity).
Сравниваются `unique_subnets`, `subnet_diversity_rejects`, `duration_ms`.

**Артефакт:** `visualization/relay-selection/` (`ranked/`, `random/`,
`subnet/`, `summary.json`).

**Типичный результат:** в режиме `subnet` — `subnet_diversity_rejects > 0`
(2-й кандидат из той же /24 отклоняется); в `ranked`/`random` — `0`.

### 12. Eclipse/Sybil защита

```bash
# N=11 честных + 5 Sybil'ов, 30 lookup'ов, 3 пути
./scripts/demo_eclipse.sh 11 3 30 5
```

**Что проверяется:** 5 Sybil-узлов с NodeID, близким к victim'у
(префикс совпадает), атакуют lookup. Single lookup компрометирован,
multi-path — устойчив.

**Ожидаемый результат:**
- `single` — **0/30** (атака успешна).
- `multipath` — **30/30** (контрмера работает).

**Артефакт:** `visualization/eclipse/` (`single/`, `multipath/`,
`summary.json`).

### 13. HTML-отчёт с графами

```bash
./scripts/generate_report.sh
xdg-open report.html
```

В отчёте:

- **Граф сети** (D3.js): overlay, topk, tree; клик — подсветка связей.
- **Routing-таблицы** всех узлов.
- **Lookup-логи** с RPC/iterations/timeouts.
- **Star vs Ring** — сравнение bootstrap-схем.
- **Сериализация** — msgpack vs JSON vs gob.

## Структура репозитория

```
p2pnode/
├── README.md                       ← этот файл
├── Dockerfile                      ← multi-stage сборка
├── docker-compose.yml              ← автогенерируемый
├── docs/
│   ├── ARCHITECTURE.md             ← архитектура, решения
│   ├── PROTOCOL.md                 ← протокол и алгоритмы
│   ├── TUNNEL.md                   ← туннели через ретрансляторы
│   ├── DEPLOY.md                   ← Docker-развёртывание
│   ├── EXPERIMENTS.md              ← план экспериментов
│   └── VISUALIZATION.md            ← Graphviz, HTML-отчёт
├── cmd/
│   └── node/main.go                ← точка входа
├── internal/
│   ├── config/                     ← конфигурация (CLI + env + YAML)
│   ├── crypto/                     ← AKE, AEAD, anti-replay
│   ├── events/                     ← event stream (JSONL)
│   ├── filetransfer/               ← передача файлов
│   ├── identity/                   ← Ed25519, NodeID
│   ├── integration/                ← интеграционные тесты
│   ├── metrics/                    ← экспорт метрик
│   ├── node/                       ← фасад узла
│   ├── protocol/                   ← формат кадра, типы сообщений
│   ├── record/                     ← подписанные NodeRecord
│   ├── routing/                    ← XOR-метрика, k-buckets
│   ├── rpc/                        ← RPC (PING, FIND_NODE, STORE, FIND_VALUE, handshake, TunnelHandler, LookupNodeMultiPath)
│   ├── store/                      ← локальное хранилище DHT
│   ├── transport/
│   │   ├── transport.go            ← интерфейсы Conn/Listener/Transport
│   │   └── tcp/                    ← TCP-реализация (SetDeadline)
│   └── tunnel/                     ← туннели через ретрансляторы
├── scripts/
│   ├── common.sh                   ← общие переменные
│   │
│   ├── ── Этап 1–5: базовое ──
│   ├── run_star.sh                 ← star-стенд (локально)
│   ├── run_ring.sh                 ← ring-стенд (локально)
│   ├── run_experiment.sh           ← полный эксперимент (локально)
│   ├── demo_store.sh               ← STORE / FIND_VALUE
│   ├── demo_tunnel.sh              ← туннельная доставка
│   ├── demo_tunnel_recovery.sh     ← восстановление
│   ├── demo_tunnel_pcap.sh         ← E2E-защита через pcap
│   ├── demo_tunnel_pool.sh         ← пул из 3 туннелей
│   ├── demo_secure_channel.sh      ← защищённый канал
│   ├── check_ne_degenerate.sh      ← проверка невырожденности
│   ├── compare_bootstrap.sh        ← star vs ring
│   ├── compare_serialization.sh    ← msgpack vs JSON vs gob
│   ├── compare_visual.sh           ← star vs ring графы
│   ├── generate_report.sh          ← HTML-отчёт
│   ├── visualize.sh                ← Graphviz-графы
│   ├── collect_routing.sh          ← сбор снапшотов
│   ├── lookup_batch.sh             ← 30 lookup'ов (локально)
│   ├── capture_and_verify.sh       ← pcap + проверка шифрования
│   ├── stop_all.sh                 ← остановка локального стенда
│   │
│   ├── ── Этап 6: Docker ──
│   ├── docker_gen_compose.sh       ← генератор docker-compose.yml
│   ├── docker_up.sh                ← запуск N узлов
│   ├── docker_down.sh              ← остановка + очистка
│   ├── docker_collect.sh           ← сбор routing из контейнеров
│   ├── docker_lookup_batch.sh      ← 30 lookup'ов через Docker
│   ├── docker_check_lookup.sh      ← один lookup с DEBUG
│   ├── run_experiment_docker.sh    ← полный эксперимент в Docker
│   ├── demo_seed_down.sh           ← работа без seed'а
│   ├── demo_failures.sh            ← 3 сценария отказа
│   ├── compare_4schemes_docker.sh  ← сравнение 4 схем
│   ├── run_experiment_n21.sh       ← N=21
│   │
│   ├── ── Этап 7: расширения ──
│   ├── demo_file_transfer.sh       ← передача файлов
│   ├── gen_underlay_rules.sh       ← генерация nftables-правил
│   ├── apply_underlay.sh           ← применение underlay
│   ├── clear_underlay.sh           ← снятие underlay
│   ├── demo_underlay.sh            ← ограниченный underlay
│   ├── apply_netem.sh              ← применение tc/netem
│   ├── clear_netem.sh              ← снятие tc/netem
│   ├── demo_netem.sh               ← управляемая деградация
│   ├── demo_relay_selection.sh     ← выбор ретранслятора
│   ├── gen_sybil_identity.sh       ← генерация Sybil-identity
│   └── demo_eclipse.sh             ← Eclipse/Sybil защита
├── deploy/
│   └── configs/                    ← примеры YAML-конфигов
└── go.mod / go.sum
```

## Конфигурация

Все параметры узла задаются **CLI-флагами**, **переменными окружения**
или **YAML-файлом**. Приоритет: **CLI > env > YAML > defaults**.

### Основные параметры

| Параметр | CLI | Env | Default |
|----------|-----|-----|---------|
| Каталог состояния | `-state-dir` | `NODE_STATE_DIR` | `./state` |
| Адрес прослушивания | `-listen-host` | `LISTEN_HOST` | `0.0.0.0` |
| Анонсируемое имя | `-advertise-host` | `ADVERTISE_HOST` | — |
| Порт | `-listen-port` | `LISTEN_PORT` | `9000` |
| Bootstrap | `-bootstrap` | `BOOTSTRAP_PEERS` | — |
| Размер k-bucket | `-k` | `K_BUCKET_SIZE` | `4` |
| Параллелизм lookup | `-alpha` | `ALPHA` | `3` |
| Таймаут connect | `-connect-timeout-ms` | `CONNECT_TIMEOUT_MS` | `3000` |
| Таймаут PING | `-ping-timeout-ms` | `PING_TIMEOUT_MS` | `5000` |
| Таймаут handshake (входящий) | `-handshake-timeout-ms` | `HANDSHAKE_TIMEOUT_MS` | `5000` |
| Таймаут одного RPC (FindNode/Store/FindValue) | `-rpc-timeout-ms` | `RPC_TIMEOUT_MS` | `10000` |
| Таймаут всего lookup | `-lookup-timeout-ms` | `LOOKUP_TIMEOUT_MS` | `30000` |
| Интервал refresh таблицы | `-refresh-interval-ms` | `REFRESH_INTERVAL_MS` | `60000` |
| Лимит subnet diversity (0 = отключено) | `-subnet-diversity-limit` | `SUBNET_DIVERSITY_LIMIT` | `2` |
| Размер кадра | `-max-frame-payload` | `MAX_FRAME_PAYLOAD` | `65536` |
| Уровень логов | `-log-level` | `LOG_LEVEL` | `INFO` |
| Каталог метрик | `-export-dir` | `EXPORT_DIR` | — |

### Параметры туннелей

| Параметр | CLI | Env | Default |
|----------|-----|-----|---------|
| Макс. ретрансляторов | `-max-hops` | `MAX_HOPS` | `3` |
| Размер пула | `-tunnel-pool-size` | `TUNNEL_POOL_SIZE` | `3` |
| Таймаут ACK | `-tunnel-ack-timeout-ms` | `TUNNEL_ACK_TIMEOUT_MS` | `5000` |
| TTL туннеля | `-tunnel-ttl-sec` | `TUNNEL_TTL_SEC` | `300` |
| Режим клиента | `-no-serve` | `NO_SERVE` | `false` |
| Пауза перед publish | `-publish-wait-ms` | `PUBLISH_WAIT_MS` | `3000` |
| Получатель | `-send-to` | `SEND_TO` | — |
| Текст | `-send-text` | `SEND_TEXT` | — |

### Параметры передача файлов

| Параметр | CLI | Env | Default |
|----------|-----|-----|---------|
| Файл для отправки | `-send-file` | `SEND_FILE` | — |
| Каталог для приёма | `-recv-file-dir` | `RECV_FILE_DIR` | — |
| Размер чанка | `-file-chunk-size` | `FILE_CHUNK_SIZE` | `32768` |
| Макс. размер файла | `-file-max-size` | `FILE_MAX_SIZE` | `104857600` |

### Параметры выбор ретранслятора

| Параметр | CLI | Env | Default |
|----------|-----|-----|---------|
| Режим выбора | `-relay-select-mode` | `RELAY_SELECT_MODE` | `ranked` |

Значения: `ranked` | `random` | `subnet`.

### Параметры Eclipse/Sybil

| Параметр | CLI | Env | Default |
|----------|-----|-----|---------|
| Число lookup-путей | `-lookup-paths` | `LOOKUP_PATHS` | `1` |
| Sybil-режим | `-sybil-mode` | `SYBIL_MODE` | `false` |
| Victim NodeID | `-sybil-victim` | `SYBIL_VICTIM` | — |

### Замечания по таймаутам

- **Единый дедлайн на RPC:** `HandshakeTimeout` + `RPCTimeout`
  применяются к **dial + handshake + write + read** одного вызова,
  а не раздельно. `Ping(timeout=5s)` реально укладывается в ~5s.
- **Refresh таблицы:** раз в `RefreshInterval` узел делает
  `LookupNode(self)` (стандартный Kademlia refresh).
- **Subnet diversity:** `0` — отключено (bucket заполняется до `K`),
  `>=K` — фактически отключено, `2` — default (защита от Sybil).
- **`Bootstrap`** возвращает ошибку, если после попыток присоединения
  routing-таблица пустая — явная сигнализация неуспешности.

## Документация

- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** — слои, модули,
  инженерные решения (TCP, msgpack, Ed25519, Kademlia, AKE, туннели,
  Docker, file-transfer), модель угроз, границы.
- **[PROTOCOL.md](docs/PROTOCOL.md)** — формат кадра, типы сообщений,
  payload'ы, алгоритмы lookup / STORE / handshake / туннелей /
  file-transfer / multipath lookup.
- **[TUNNEL.md](docs/TUNNEL.md)** — туннели через ретрансляторы +
  file-transfer поверх туннелей.
- **[DEPLOY.md](docs/DEPLOY.md)** — Docker-развёртывание: 4
  bootstrap-схемы, скрипты управления, диагностика.
- **[EXPERIMENTS.md](docs/EXPERIMENTS.md)** — план экспериментов
  этапа 7 (E7-1 … E7-8): гипотезы, метрики, критерии.
- **[VISUALIZATION.md](docs/VISUALIZATION.md)** — Graphviz-графы,
  HTML-отчёт с D3.js, метрики всех экспериментов.
