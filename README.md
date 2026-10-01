# p2pnode

Прототип узла **защищённой децентрализованной оверлейной P2P-сети**,
работающей поверх TCP/IP. Узел обнаруживает других участников через
распределённую хэш-таблицу (Kademlia DHT), обменивается подписанными
записями и защищает канал связи аутентифицированным шифрованием
(собственный AKE на библиотечных примитивах).

## Возможности

| Этап | Возможность |
|------|-------------|
| 1 | Транспорт: TCP, 24-байтовый формат кадра, диспетчеризация |
| 2 | Ed25519-идентичность, Kademlia DHT (PING, FIND_NODE), bootstrap, итеративный lookup |
| 3 | Подписанные `NodeRecord` (Ed25519), STORE/FIND_VALUE, репликация R=3, TTL, anti-rollback, псевдонимы |
| 4 | Собственный AKE (Ed25519 + X25519 + HKDF + ChaCha20-Poly1305), anti-replay |
| 5 | Туннели через ретрансляторы, прикладные сообщения и файлы |
| 6 | Docker Compose, 4 bootstrap-схемы, 20+ узлов |
| 7 | Метрики, статистика, графики |

## Требования

- **Go** 1.22 или новее.
- **Python 3** — для скриптов анализа и генерации отчёта.
- **Graphviz** (опционально) — для визуализации графов.
- **bash**, `sha256sum`, `mktemp` — стандартные утилиты Linux.

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

### 4. HTML-отчёт с графами

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

## Структура репозитория

```
p2pnode/
├── README.md                       ← этот файл
├── docs/
│   ├── ARCHITECTURE.md             ← архитектура, решения
│   └── PROTOCOL.md                 ← протокол и алгоритмы
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
│   ├── rpc/                        ← RPC (PING, FIND_NODE, STORE, FIND_VALUE, handshake)
│   ├── store/                      ← локальное хранилище DHT
│   └── transport/
│       ├── transport.go            ← интерфейсы Conn/Listener/Transport
│       └── tcp/                    ← TCP-реализация
├── scripts/
│   ├── common.sh                   ← общие переменные
│   ├── run_star.sh                 ← star-стенд
│   ├── run_ring.sh                 ← ring-стенд
│   ├── run_experiment.sh           ← полный эксперимент
│   ├── demo_store.sh               ← демонстрация STORE/FIND_VALUE
│   ├── check_ne_degenerate.sh      ← проверка невырожденности
│   ├── compare_bootstrap.sh        ← star vs ring
│   ├── compare_serialization.sh    ← msgpack vs JSON vs gob
│   ├── generate_report.sh          ← HTML-отчёт
│   ├── visualize.sh                ← Graphviz-графы
│   ├── collect_routing.sh          ← сбор снапшотов
│   ├── lookup_batch.sh             ← 30 lookup'ов
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

### YAML-конфиг

```bash
go run ./cmd/node -config deploy/configs/node-seed.yaml
```

Пример: `deploy/configs/node-seed.yaml`.

## Документация

- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** — слои, модули,
  инженерные решения (TCP, msgpack, Ed25519, Kademlia, AKE),
  модель угроз, границы.
- **[PROTOCOL.md](docs/PROTOCOL.md)** — формат кадра, типы сообщений,
  payload'ы, алгоритмы lookup / STORE / handshake.
