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
| 7 | Метрики, статистика, графики |

## Требования

- **Go** 1.26 или новее.
- **Python 3** — для скриптов анализа и генерации отчёта.
- **Docker** ≥ 20.10 + `docker compose` v2.
- **Graphviz** (опционально) — для визуализации графов.
- **bash**, `sha256sum`, `mktemp` — стандартные утилиты Linux.
- **tcpdump** (опционально) — для демонстрации E2E-защиты.

**Go-зависимости** (устанавливаются автоматически):

- `github.com/vmihailenco/msgpack/v5` — сериализация.
- `golang.org/x/crypto` — HKDF, ChaCha20-Poly1305.
- `filippo.io/edwards25519` — конвертация Ed25519 ↔ X25519.

Установка:

```bash
go mod download
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

Узел сгенерирует Ed25519-ключи в `state-dir`, поднимет TCP-слушатель
и будет ждать входящих соединений.

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

### 3. Защищённый канал

Все RPC идут через handshake + AEAD. Отдельная демонстрация:

```bash
# Терминал 1: seed
./bin/node -listen-port 9500 -state-dir /tmp/n1 -log-level DEBUG

# Терминал 2: клиент
./bin/node -listen-port 9501 -state-dir /tmp/n2 \
    -bootstrap 127.0.0.1:9500 -log-level DEBUG
```

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
# star
./scripts/docker_up.sh 15 star

# ring
./scripts/docker_up.sh 15 ring

# tree
./scripts/docker_up.sh 15 tree

# multiseed (3 seed'а)
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

### 7. Docker: N=21 (продвинутый масштаб)

```bash
./scripts/run_experiment_n21.sh 21 40
```

### 8. HTML-отчёт с графами

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
│   └── VISUALIZATION.md            ← Graphviz, HTML-отчёт
├── cmd/
│   └── node/main.go                ← точка входа
├── internal/
│   ├── config/                     ← конфигурация (CLI + env + YAML)
│   ├── crypto/                     ← AKE, AEAD, anti-replay
│   ├── events/                     ← event stream (JSONL)
│   ├── identity/                   ← Ed25519, NodeID
│   ├── integration/                ← интеграционные тесты
│   ├── metrics/                    ← экспорт метрик
│   ├── node/                       ← фасад узла
│   ├── protocol/                   ← формат кадра, типы сообщений
│   ├── record/                     ← подписанные NodeRecord
│   ├── routing/                    ← XOR-метрика, k-buckets
│   ├── rpc/                        ← RPC (PING, FIND_NODE, STORE, FIND_VALUE, handshake, TunnelHandler)
│   ├── store/                      ← локальное хранилище DHT
│   ├── transport/
│   │   ├── transport.go            ← интерфейсы Conn/Listener/Transport
│   │   └── tcp/                    ← TCP-реализация (SetReadTimeout)
│   └── tunnel/                     ← туннели через ретрансляторы
├── scripts/
│   ├── common.sh                   ← общие переменные
│   ├── run_star.sh                 ← star-стенд (локально)
│   ├── run_ring.sh                 ← ring-стенд (локально)
│   ├── run_experiment.sh           ← полный эксперимент (локально)
│   ├── demo_store.sh               ← STORE / FIND_VALUE
│   ├── demo_tunnel.sh              ← туннельная доставка
│   ├── demo_tunnel_recovery.sh     ← восстановление (E5-6)
│   ├── demo_tunnel_pcap.sh         ← E2E-защита через pcap (E5-4)
│   ├── demo_tunnel_pool.sh         ← пул из 3 туннелей (E5-8)
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
│   └── run_experiment_n21.sh       ← N=21
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

## Документация

- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** — слои, модули,
  инженерные решения (TCP, msgpack, Ed25519, Kademlia, AKE, туннели),
  модель угроз, границы.
- **[PROTOCOL.md](docs/PROTOCOL.md)** — формат кадра, типы сообщений,
  payload'ы, алгоритмы lookup / STORE / handshake / туннелей.
- **[TUNNEL.md](docs/TUNNEL.md)** — туннели через ретрансляторы.
- **[DEPLOY.md](docs/DEPLOY.md)** — Docker-развёртывание: 4
  bootstrap-схемы, скрипты управления, диагностика.
- **[VISUALIZATION.md](docs/VISUALIZATION.md)** — Graphviz-графы,
  HTML-отчёт с D3.js.
