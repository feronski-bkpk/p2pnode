# p2pnode — узел защищённой оверлейной P2P-сети

Прототип узла децентрализованной телекоммуникационной сети, работающей поверх TCP/IP.

## Содержание

- [Требования](#требования)
- [Быстрый старт](#быстрый-старт)
- [Архитектура](#архитектура)
- [Формат кадра и протокол](#формат-кадра-и-протокол)
- [Идентичность и NodeID](#идентичность-и-nodeid)
- [Kademlia DHT](#kademlia-dht)
- [Bootstrap и lookup](#bootstrap-и-lookup)
- [Развёртывание тестового стенда](#развёртывание-тестового-стенда)
- [Проверка невырожденности DHT](#проверка-невырожденности-dht)
- [Тесты](#тесты)

## Требования

- **Go** 1.22 или новее.
- **GNU Make** (опционально).
- **Python 3** (для скриптов проверки метрик).

Внешние Go-зависимости:

- `github.com/vmihailenco/msgpack/v5` — сериализация payload'ов.

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

В логе:

```
level=INFO msg=listening addr=127.0.0.1:9001 \
    node_id=<64 hex> state_dir=/tmp/node-01 k=4 alpha=3
level=INFO msg="bootstrap: skipped (no peers)"
level=INFO msg="bootstrap complete" table_size=0
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

Второй узел:

1. Отправит `PING` seed'у, чтобы узнать его `NodeID`.
2. Добавит seed в таблицу.
3. Выполнит итеративный `self-lookup`.

В логе:

```
level=INFO msg="bootstrap: pinging seed" addr=127.0.0.1:9001
level=INFO msg="bootstrap: seed identified" node_id=<short> addr=127.0.0.1:9001
level=INFO msg="bootstrap: self-lookup done" rpc=1 iterations=1 table_size=1
level=INFO msg="bootstrap complete" table_size=1
```

### Полный эксперимент на N=15

```bash
chmod +x scripts/*.sh
./scripts/run_experiment.sh 15 20
```

Скрипт:

1. Останавливает предыдущий стенд.
2. Запускает 15 узлов в star-схеме (порты 9001–9015).
3. Ждёт 20 секунд сходимости.
4. Собирает routing-снапшоты.
5. Делает 30 контрольных lookup'ов.
6. Проверяет невырожденность DHT по 4 критериям ТЗ.

Результаты в `metrics/`:

```
metrics/
├── routing-<nodeid>-<ts>.json          # периодические снапшоты
├── collected/
│   └── routing-<short>.json            # по одному свежему файлу на узел
└── lookups/
    └── lookup-<i>-to-<j>-<short>.json  # 30 контрольных lookup'ов
```

## Архитектура

```
┌────────────────────────────────────────────────────────────┐
│  cmd/node — точка входа                                    │
├────────────────────────────────────────────────────────────┤
│  internal/node — сборка узла, bootstrap                    │
├────────────────────────────────────────────────────────────┤
│  internal/rpc — Client, Server, PING, FIND_NODE, lookup    │
├────────────────────────────────────────────────────────────┤
│  internal/routing — ID, XOR, Contact, k-buckets            │
├────────────────────────────────────────────────────────────┤
│  internal/identity — Ed25519, NodeID                       │
├────────────────────────────────────────────────────────────┤
│  internal/protocol — MsgType, Frame, payload               │
├────────────────────────────────────────────────────────────┤
│  internal/transport — Conn, Listener, Transport (TCP)      │
├────────────────────────────────────────────────────────────┤
│  internal/config — Config, Load (CLI+env+defaults)         │
├────────────────────────────────────────────────────────────┤
│  internal/metrics — экспорт routing/lookup в JSON          │
└────────────────────────────────────────────────────────────┘
```

### Структура репозитория

```
p2pnode/
├── go.mod
├── go.sum
├── README.md
├── cmd/node/main.go
├── internal/
│   ├── config/config.go
│   ├── identity/identity.go + identity_test.go
│   ├── protocol/{msgtype,frame,payload}.go + *_test.go
│   ├── transport/
│   │   ├── transport.go
│   │   └── tcp/{conn,listener,transport}.go + transport_test.go
│   ├── routing/{id,contact,bucket,routing}.go + *_test.go
│   ├── rpc/{client,ping,find_node,server,lookup,checker}.go + *_test.go
│   ├── node/node.go
│   ├── metrics/{export,routing,lookup,network}.go + *_test.go
│   └── integration/bootstrap_test.go
└── scripts/
    ├── common.sh
    ├── run_star.sh
    ├── run_ring.sh
    ├── stop_all.sh
    ├── collect_routing.sh
    ├── lookup_batch.sh
    ├── check_ne_degenerate.sh
    └── run_experiment.sh
```

## Формат кадра и протокол

Все сообщения передаются поверх TCP с **собственным кадрированием**.
Заголовок — 24 байта, все многобайтные числа big-endian:

```
+---------+--------+--------+-------------+----------------+---------+
| version |  type  | flags  | request_id  | payload_length | payload |
| 1 байт  | 1 байт | 2 байта| 16 байт     | 4 байта        | ≤64 KiB |
+---------+--------+--------+-------------+----------------+---------+
```

Поля:

- `version` — версия протокола (сейчас `1`).
- `type` — тип сообщения (см. таблицу ниже).
- `flags` — зарезервировано (0 на этапе 2).
- `request_id` — 128-битный идентификатор запроса, генерируется через
  `crypto/rand`. Ответ обязан содержать **тот же** `request_id`.
- `payload_length` — длина payload, **проверяется до выделения буфера**.
  Максимум — `MAX_FRAME_PAYLOAD = 65536`.
- `payload` — сериализованное сообщение (msgpack).

### Типы сообщений этапа 2

| Код | Имя | Направление |
|-----|-----|-------------|
| `0x01` | `PING` | запрос |
| `0x02` | `PONG` | ответ |
| `0x03` | `FIND_NODE_REQUEST` | запрос |
| `0x04` | `FIND_NODE_RESPONSE` | ответ |
| `0x7F` | `ERROR` | ответ/уведомление |

Коды `0x05–0x3F` зарезервированы под следующие этапы
(`STORE`, `FIND_VALUE`, туннели, приложения).

### Сериализация payload

Выбран **MessagePack** (`github.com/vmihailenco/msgpack/v5`) — компактнее
JSON, не требует кодогенерации (в отличие от protobuf), остаётся
языко-независимым (в отличие от gob).

Примеры payload'ов:

```
PING {
  sender: Contact,
  timestamp_ms: uint64
}

PONG {
  responder: Contact,
  ping_timestamp_ms: uint64,
  responder_timestamp_ms: uint64
}

FIND_NODE_REQUEST {
  sender: Contact,
  target_node_id: bytes[32]
}

FIND_NODE_RESPONSE {
  responder: Contact,
  target_node_id: bytes[32],
  contacts: Contact[]
}

ERROR {
  code: string,
  message: string
}
```

`Contact`:

```
Contact {
  node_id:            bytes[32]            // SHA-256(pubkey)
  identity_algorithm: string               // "ed25519"
  identity_public_key: bytes[32]           // Ed25519 public key
  host: string
  port: uint16
}
```

Поля `last_seen_ms` и `last_verified_ms` **не передаются по сети** — это
локальные метаданные узла-наблюдателя.

## Идентичность и NodeID

Каждый узел при первом запуске создаёт долговременную пару **Ed25519**.
Закрытый ключ хранится только в `-state-dir` и не покидает узел.

```
identity.key  — 64 байта Ed25519 private key
identity.pub  — 32 байта Ed25519 public key
```

`NodeID` вычисляется детерминированно:

```
NodeID = SHA-256(canonical_encode(identity_public_key))
canonical_encode(pubkey) = pubkey_bytes  (для Ed25519 — просто 32 байта)
```

При получении `Contact` узел обязан проверить:

- `len(identity_public_key) == 32`;
- `identity_algorithm == "ed25519"`;
- `SHA-256(identity_public_key) == node_id`;
- `host` и `port` непусты.

Контакт с несовпадающим `NodeID` и `pubkey` отклоняется как попытка
подмены идентичности.

## Kademlia DHT

### XOR-метрика

Расстояние между `NodeID` `a` и `b`:

```
d(a, b) = a XOR b   (256-битное число)
```

Свойства: симметричность, рефлексивность, triangle inequality.

### k-buckets

Таблица маршрутизации — 256 bucket'ов, индекс bucket'а — позиция
старшего установленного бита в `self.ID XOR other.ID`.

Каждый bucket вмещает до `K_BUCKET_SIZE = 4` контактов,
упорядоченных от oldest (head) к newest (tail).

**При добавлении контакта:**

1. Валидация.
2. Если контакт уже есть — обновить и переместить в tail.
3. Если bucket не полон — добавить в tail.
4. Если bucket полон:
   - **PING head**;
   - живой → переместить в tail, новый **не добавлять**;
   - мёртвый → вытеснить, добавить нового.

Это стандартное требование Kademlia: живой LRU-контакт не вытесняется.

## Bootstrap и lookup

### Bootstrap

Новый узел с непустым `-bootstrap`:

1. `PING(seed)` → узнать `NodeID` и `Contact` seed'а.
2. Добавить seed в таблицу.
3. Итеративный `FIND_NODE(self.ID)` — self-lookup.
4. Итоговая таблица содержит 4–13 контактов (зависит от N).

Bootstrap-узел **не является центральным каталогом**: после присоединения
его недоступность не блокирует lookup'ы.

### Итеративный lookup

`LookupNode(target)`:

1. `shortlist = table.Closest(target, K)`.
2. На каждой итерации:
   - выбрать до `ALPHA = 3` неопрошенных ближайших кандидатов;
   - параллельно отправить `FIND_NODE_REQUEST`;
   - собрать ответы, обновить `shortlist`;
   - **early termination**, если target найден.
3. Остановка: все `K` ближайших опрошены, либо target найден.

`HandleFindNode` возвращает `K` контактов, **включая самого отвечающего**,
если он входит в число `K` ближайших к target. Это позволяет инициатору
найти цель, если она и есть отвечающий узел.

### Формат лога lookup

Каждый lookup экспортируется в JSON:

```json
{
  "target": "abc...",
  "initiator": "def...",
  "start_unix_ms": 1712345678000,
  "end_unix_ms":   1712345678100,
  "duration_ms": 100,
  "rpc": 5,
  "iterations": 2,
  "timeouts": 0,
  "target_absent_at_start": true,
  "final_contacts": [...],
  "iterations_log": [...]
}
```

## Развёртывание тестового стенда

### Star-схема

Все узлы подключаются к одному seed'у:

```bash
./scripts/run_star.sh 15
```

- Узел 1 — seed, порт 9001.
- Узлы 2..15 — порты 9002..9015, `-bootstrap 127.0.0.1:9001`.

### Ring-схема

Каждый узел знает только предыдущего:

```bash
./scripts/run_ring.sh 15
```

- Узел 1 — seed.
- Узел i — `-bootstrap 127.0.0.1:<порт i-1>`.

### Остановка

```bash
./scripts/stop_all.sh
```

### Полный эксперимент

```bash
./scripts/run_experiment.sh 15 20
```

Параметры: `N=15`, время сходимости `20` секунд.

## Проверка невырожденности DHT

ТЗ требует формального доказательства, что DHT не выродилась в полный
реестр. Критерии (раздел 6 ТЗ):

1. **≥80% узлов** после сходимости имеют в таблице **< N−1** контактов.
2. **Ни один узел** не имеет полного реестра (`size ≥ N−1`).
3. **≥30 контрольных lookup'ов**, где цель **отсутствует** в таблице
   инициатора до начала поиска.
4. **≥30 lookup'ов**, использующих **≥1 промежуточный узел** (`RPC ≥ 2`).

Проверка:

```bash
./scripts/check_ne_degenerate.sh
```

Ожидаемый вывод:

```
[check] N = 15
[check] table sizes: min=4 max=13 mean=6.73
[check] nodes with < N-1 contacts: 15/15 (100.0%)
[check] criterion 1 (>=80% < N-1):        True
[check] criterion 1b (no full registry):  True
[check] buckets per node: min=2 max=6 mean=4.00
[check] max bucket fill:  min=1 max=4

[check] lookup'ов всего:               30
[check] цель отсутствовала до старта:  30
[check] с промежуточным узлом (RPC≥2): 30
[check] успешных (target в final):     30
[check] criterion 2 (>=30 absent):     True
[check] criterion 3 (>=30 w/ interm):  True

[check] RESULT: NON-DEGENERATE (все критерии выполнены)
```

### Гарантия прекондиции lookup

Скрипт `lookup_batch.sh` запускает каждый lookup в **отдельном процессе**
с временным `state-dir`:

1. Копируется только `identity.{key,pub}` инициатора.
2. Узел стартует с **пустой** таблицей.
3. `-skip-self-lookup` — bootstrap забирает только соседей seed'а
   (seed **не добавляется** в таблицу).
4. Перед lookup `main.go` **удаляет target из таблицы**, если он там
   оказался после bootstrap, — восстанавливая прекондицию ТЗ.

## Тесты

```bash
go test ./... -v
```

Покрытие:

| Пакет | Тестов | Что покрыто |
|-------|--------|-------------|
| `identity` | 9 | Ed25519, детерминизм NodeID, персистентность, ошибки |
| `protocol` | 19 | Frame round-trip, partial read, too big, bad version/type, request_id, payload'ы |
| `transport/tcp` | 5 | Обмен, 1000 кадров, timeout, закрытие |
| `routing` | 22 | XOR, k-buckets, LRU, PING-oldest, дубликаты, SnapshotBuckets |
| `rpc` | 6 | PING round-trip, request_id mismatch, timeout, 3-узловой lookup |
| `metrics` | 8 | Snapshot, экспорт JSON, критерии невырожденности |
| `integration` | 2 | Star 5 узлов, Ring 5 узлов |

**Ключевые тесты:**
- `TestBucketFull_LiveHeadNotEvicted` — живой LRU не вытесняется.
- `TestLookup3Nodes` — цель, отсутствующая у инициатора, найдена через
  промежуточный узел (RPC ≥ 2).
- `TestRequestIDMismatch` — ответ с чужим `request_id` отклоняется.
- `TestStar5Nodes`, `TestRing5Nodes` — сходимость на 5 узлах.
