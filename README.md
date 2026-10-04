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
| 6 | Docker Compose, 4 bootstrap-схемы, 20+ узлов |
| 7 | Метрики, статистика, графики |

## Требования

- **Go** 1.22 или новее.
- **Python 3** — для скриптов анализа и генерации отчёта.
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

### Сборка

```bash
go build ./...
```

### Тесты

```bash
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

Второй узел выполнит handshake с seed'ом, `PING`, `FIND_NODE(self.ID)`
и self-lookup — присоединится к сети.

### Полный эксперимент на N=15

```bash
chmod +x scripts/*.sh
./scripts/run_experiment.sh 15 20
```

Скрипт:

1. Останавливает предыдущий стенд.
2. Собирает бинарник.
3. Запускает 15 узлов в star-схеме (порты 9001–9015).
4. Ждёт 20 секунд сходимости.
5. Собирает routing-снапшоты.
6. Делает 30 контрольных lookup'ов.
7. Проверяет невырожденность DHT.

## Демонстрационные сценарии

### 1. Обнаружение узлов

```bash
./scripts/run_star.sh 15
sleep 20
./scripts/collect_routing.sh
./scripts/check_ne_degenerate.sh
./scripts/stop_all.sh
```

**Проверка невырожденности DHT:** не менее 80% узлов имеют
`< N-1` контактов, ни один узел не имеет полного реестра.

### 2. STORE / FIND_VALUE

```bash
./scripts/demo_store.sh 5 15
```

Сценарий:

1. 5 узлов в star-схеме.
2. Узел 1 публикует свою `NodeRecord` в DHT.
3. Узел 3 находит её через `FIND_VALUE`.
4. Останавливается один хранитель.
5. Узел 4 находит ту же запись — репликация работает.
6. Публикация псевдонима `alice`.
7. Поиск по псевдониму.

### 3. Защищённый канал

Все RPC в проекте идут через handshake + AEAD. Отдельная демонстрация:

```bash
# Терминал 1: seed
./bin/node -listen-port 9500 -state-dir /tmp/n1 -log-level DEBUG

# Терминал 2: клиент
./bin/node -listen-port 9501 -state-dir /tmp/n2 \
    -bootstrap 127.0.0.1:9500 -log-level DEBUG
```

В логах — `handshake_done` с `session_id`. Трафик шифрован ChaCha20-Poly1305.

### 4. Туннели через ретрансляторы

**Базовая доставка** (3 ретранслятора, E2E):

```bash
./scripts/demo_tunnel.sh
```

Проверки:

- `OK: 3 relays participated`
- `tunnel: build complete ... acks_received=3`
- `OK: plaintext not found in relay logs (E5-4)`
- `OK: node-2 received tunnel message`

**Восстановление после отказа ретранслятора**:

```bash
./scripts/demo_tunnel_recovery.sh
```

**Доказательство E2E-защиты через pcap**:

```bash
# Один раз: даём tcpdump права без root
sudo setcap cap_net_raw,cap_net_admin+eip $(which tcpdump)

./scripts/demo_tunnel_pcap.sh
```

**Пул из 3 туннелей с переключением**:

```bash
./scripts/demo_tunnel_pool.sh
```

**Детали:** см. **[docs/TUNNEL.md](docs/TUNNEL.md)**.

### 5. HTML-отчёт с графами

```bash
# Запустить эксперимент
./scripts/run_experiment.sh 15 20

# Сгенерировать отчёт
./scripts/generate_report.sh

# Открыть
xdg-open report.html
```

В отчёте:

- **Граф сети** (D3.js): overlay, topk, tree; клик — подсветка связей.
- **Routing-таблицы** всех узлов.
- **Lookup-логи** с RPC/iterations/timeouts.
- **Star vs Ring** — сравнение bootstrap-схем.
- **Сериализация** — msgpack vs JSON vs gob.

**Детали:** см. **[docs/VISUALIZATION.md](docs/VISUALIZATION.md)**.

## Структура репозитория

```
p2pnode/
├── README.md                       ← этот файл
├── docs/
│   ├── ARCHITECTURE.md             ← архитектура, решения
│   ├── PROTOCOL.md                 ← протокол и алгоритмы
│   ├── TUNNEL.md                   ← туннели через ретрансляторы
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
│       ├── tunnel.go               ← Tunnel, State, E2ESession
│       ├── id.go                   ← ID, NewID
│       ├── build.go                ← BuildCoordinator
│       ├── handlers.go             ← HandleTunnelBuild, relayLoop, destDataLoop
│       ├── relay.go                ← RelayState, RelayStore, DestSessionStore
│       ├── session.go              ← Session, SendMessage, readLoop
│       ├── manager.go              ← TunnelManager, BuildPool, pickSession
│       ├── message.go              ← MessageStore
│       ├── route.go                ← RouteBuilder
│       ├── profile.go              ← ProfileStore
│       └── *_test.go               ← unit + integration тесты
├── scripts/
│   ├── common.sh                   ← общие переменные
│   ├── run_star.sh                 ← star-стенд
│   ├── run_ring.sh                 ← ring-стенд
│   ├── run_experiment.sh           ← полный эксперимент
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
│   ├── lookup_batch.sh             ← 30 lookup'ов
│   ├── capture_and_verify.sh       ← pcap + проверка шифрования
│   └── stop_all.sh                 ← остановка
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
| Число отправок | `-send-repeat` | `SEND_REPEAT` | `1` |
| Интервал | `-send-interval-ms` | `SEND_INTERVAL_MS` | `2000` |
| Force-exit | `-exit-after-ms` | `EXIT_AFTER_MS` | `5000` |

### YAML-конфиг

```bash
go run ./cmd/node -config deploy/configs/node-seed.yaml
```

Пример: `deploy/configs/node-seed.yaml`.

## Документация

- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** — слои, модули,
  инженерные решения (TCP, msgpack, Ed25519, Kademlia, AKE, туннели),
  модель угроз, границы.
- **[PROTOCOL.md](docs/PROTOCOL.md)** — формат кадра, типы сообщений,
  payload'ы, алгоритмы lookup / STORE / handshake / туннелей.
- **[TUNNEL.md](docs/TUNNEL.md)** — туннели через ретрансляторы:
  построение, E2E-шифрование, состояния, восстановление, пул, TTL,
  демо-сценарии.
- **[VISUALIZATION.md](docs/VISUALIZATION.md)** — Graphviz-графы,
  HTML-отчёт с D3.js, сравнение bootstrap-схем, event stream.
