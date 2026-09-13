# p2pnode — узел защищённой оверлейной P2P-сети

Прототип узла децентрализованной телекоммуникационной сети, работающей
поверх TCP/IP. Узел:

- обнаруживает других участников через **распределённую хэш-таблицу (DHT)**
  на основе **Kademlia**;
- обменивается данными (сообщениями и файлами) с адресацией по псевдониму
  через DHT;
- защищает канал связи **аутентифицированным шифрованием** (AEAD) с защитой
  от replay-атак;
- устойчив к отказу отдельного узла (репликация данных на K ближайших
  узлов, итеративный поиск по нескольким путям).

## Содержание

- [Требования](#требования)
- [Быстрый старт](#быстрый-старт)
- [Архитектура](#архитектура)
- [Структура репозитория](#структура-репозитория)
- [Как это работает](#как-это-работает)
  - [Транспорт и кадрирование](#транспорт-и-кадрирование)
  - [DHT: идентификаторы и метрика](#dht-идентификаторы-и-метрика)
  - [DHT: таблица маршрутизации](#dht-таблица-маршрутизации)
  - [DHT: обнаружение узлов](#dht-обнаружение-узлов)
  - [DHT: хранение значений](#dht-хранение-значений)
- [Ручные сценарии](#ручные-сценарии)
- [Тесты](#тесты)
- [Roadmap](#roadmap)
- [Ограничения и упрощения](#ограничения-и-упрощения)

## Требования

- **Go** 1.22 или новее (`go version`).
- **GNU Make** (опционально, для удобных команд).
- **Docker** и **Docker Compose** — только для этапа развёртывания тестовой
  сети из 5–7 узлов.
- **tcpdump** / **Wireshark** — только для демонстрации шифрования канала.

Внешние Go-зависимости:

- `github.com/vmihailenco/msgpack/v5` — сериализация DHT-сообщений.

Установка зависимостей:

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

Полный вывод с подробностями:

```bash
go test ./... -v
```

### Запуск одного узла

```bash
go run ./cmd/node -listen 127.0.0.1:9001 -log debug
```

Узел сгенерирует случайный 256-битный ID, поднимет TCP-слушатель на
указанном адресе и будет ждать входящих соединений. В логе появятся
строки:

```
level=INFO msg=listening addr=127.0.0.1:9001 id=<hex>
```

### Запуск второго узла с bootstrap

В отдельном терминале:

```bash
go run ./cmd/node -listen 127.0.0.1:9002 \
    -bootstrap 127.0.0.1:9001 \
    -log debug
```

Второй узел:

1. Отправит `PING` первому узлу, чтобы узнать его ID.
2. Запросит `FIND_NODE(свой_ID)` — получит ближайших известных узлов.
3. Пинганёт их, чтобы они добавили его в свои таблицы.
4. Выполнит self-lookup — итеративный поиск по собственному ID.

В логе появятся строки:

```
level=INFO msg="bootstrap: seed identified" id=<hex> addr=127.0.0.1:9001
level=INFO msg="bootstrap: initial nodes" count=1
level=INFO msg="bootstrap: self-lookup done" found=1 table_size=1
level=INFO msg="bootstrap ok" table_size=1
level=INFO msg="known peer" id=<hex> addr=127.0.0.1:9001
```

### Остановка

`Ctrl+C`. Узел корректно завершится:

```
level=INFO msg="shutting down"
```

## Архитектура

```
┌─────────────────────────────────────────────────────────────┐
│  Прикладной слой                                            │
│  ├── TextService   — текстовые сообщения                    │
│  └── FileService   — передача файлов чанками                │
├─────────────────────────────────────────────────────────────┤
│  DHT                                                        │
│  ├── RoutingTable  — k-buckets, XOR-метрика                 │
│  ├── PING / FIND_NODE                                       │
│  └── STORE / FIND_VALUE                                     │
├─────────────────────────────────────────────────────────────┤
│  Crypto                                                     │
│  ├── Handshake     — X25519 + Ed25519                       │
│  └── AEAD          — ChaCha20-Poly1305 + anti-replay        │
├─────────────────────────────────────────────────────────────┤
│  Transport                                                  │
│  ├── Frame         — length + type + payload                │
│  └── Conn (TCP)    — абстракция для будущего UDP            │
└─────────────────────────────────────────────────────────────┘
```

## Структура репозитория

```
p2pnode/
├── go.mod
├── go.sum
├── README.md
├── cmd/
│   └── node/
│       └── main.go                 # точка входа: флаги, запуск, graceful shutdown
└── internal/
    ├── transport/
    │   ├── transport.go            # интерфейсы Conn, Listener, Transport, Frame
    │   └── tcp/
    │       ├── frame.go            # кадрирование: [magic][type][len][payload]
    │       ├── frame_test.go
    │       ├── conn.go             # TCP Conn
    │       ├── listener.go         # TCP Listener
    │       ├── transport.go        # TCP Transport (Dial/Listen)
    │       └── transport_test.go
    ├── dispatch/
    │   ├── dispatcher.go           # реестр обработчиков по типу сообщения
    │   └── dispatcher_test.go
    └── dht/
        ├── id.go                   # ID, XOR-метрика
        ├── id_test.go
        ├── node.go                 # структура Node
        ├── routing.go              # k-buckets, RoutingTable
        ├── routing_test.go
        ├── messages.go             # типы DHT-сообщений (msgpack)
        ├── messages_test.go
        ├── handlers.go             # HandlePing, HandleFindNode, RegisterAll
        ├── dht.go                  # DHT: Ping, FindNode, Bootstrap
        ├── dht_test.go
        └── testcluster_test.go     # хелперы для интеграционных тестов
```

## Как это работает

### Транспорт и кадрирование

**Кадрирование:**
Формат кадра (7 байт заголовка + payload):

```
+--------+--------+--------+--------+----------------+
|  magic |  type  |       length    |    payload     |
| 2 байта| 1 байт |     4 байта     |  length байт   |
+--------+--------+--------+--------+----------------+
  0x50 0x32
```

- `magic = "P2"` — защита от мусора в потоке.
- `type` — тип сообщения (PING, FIND_NODE, STORE, …).
- `length` — big-endian, максимум 16 МБ (защита от вредоносного length).
- `payload` — ровно `length` байт.

Чтение кадра: `io.ReadFull(r, header)`, проверка magic, чтение ровно
`length` байт payload.

Запись кадра: один `Write` на заголовок + один на payload, flush буфера.

### DHT: идентификаторы и метрика

- **ID узла — 256 бит** (`[32]byte`).
- На этапе 2 генерируется случайно через `crypto/rand`.
- На этапе 4 будет = `SHA-256(Ed25519 public key)`, что связывает
  идентификатор с криптографическим ключом.

**XOR-метрика:** расстояние между `a` и `b` = `a XOR b`. Свойства:

- симметрична (`d(a,b) = d(b,a)`);
- рефлексивна (`d(a,a) = 0`);
- triangle inequality (`d(a,c) ≤ d(a,b) + d(b,c)`);
- если `d(a,b) < 2^i`, то первые `256-i` бит `a` и `b` совпадают.

Последнее свойство — ключ к k-buckets: узлы с общим префиксом ID
группируются в один бакет.

### DHT: таблица маршрутизации

**k-buckets:** 256 бакетов (по числу бит ID). Бакет `i` содержит узлы,
у которых:

- общий префикс с `self.ID` длиной `i` бит;
- бит `i` отличается.

Каждый бакет хранит до `K = 8` узлов, упорядоченных от oldest (head)
к newest (tail).

Операции:

- `Add(node)` — добавить/обновить; если бакет полон, вытесняется oldest.
- `Remove(id)` — удалить.
- `Get(id)` — найти.
- `Closest(target, n)` — вернуть `n` узлов, ближайших к `target`.
- `Size()` — общее число узлов.
- `Snapshot()` — копия всех узлов (для отладки).

Защита от фантомов: `Add` отклоняет узлы с нулевым ID или пустым адресом.

### DHT: обнаружение узлов

**PING(node)** — проверка доступности. Возвращает `{FromID, FromAddr}`.
Полезен при bootstrap, когда мы знаем только адрес, но не ID.

**FIND_NODE(target)** — «дай K узлов, ближайших к target». Один RPC —
это один шаг; полный поиск — итеративный:

```
shortlist = table.Closest(target, K)
queried   = {}
while есть неопрошенные в shortlist:
    toQuery = первые Alpha непрошенных из shortlist
    параллельно шлём FIND_NODE(target) каждому
    собираем ответы → newNodes
    shortlist = uniqueSorted(shortlist + newNodes, target, K)
    if все K ближайших опрошены: break
return первые K из shortlist
```

Параметры: `K = 8`, `Alpha = 3`. Гарантирует сходимость за O(log N)
шагов.

**Bootstrap(seedAddr):**

1. `PING(seedAddr)` — узнать ID seed'а.
2. `FIND_NODE(self.ID)` — забрать ближайших к себе.
3. Пингануть найденных (асинхронно) — чтобы они узнали о нас.
4. `FindNode(self.ID)` — self-lookup, заполняет бакеты вокруг своего ID.

### DHT: хранение значений

- `STORE(key, value, ttl)` — сохранить пару на K ближайших к `key` узлах.
- `FIND_VALUE(key)` — итеративный поиск: либо значение, либо список
  ближайших узлов.
- TTL: запись автоматически удаляется через `expires`.
- Репликация: на K ближайших к `key` узлов (включая себя).
- Re-publish: издатель раз в `TTL/2` переопубликовывает свои записи.

## Ручные сценарии

### Сценарий 1: два узла, простой обмен

Терминал 1 (узел A):

```bash
go run ./cmd/node -listen 127.0.0.1:9001 -log debug
```

Терминал 2 (узел B, подключается к A и шлёт текстовое сообщение):

```bash
go run ./cmd/node -listen 127.0.0.1:9002 \
    -connect 127.0.0.1:9001 \
    -msg "привет из узла B" \
    -log debug
```

В терминале 1 появится:

```
level=INFO msg="incoming connection" remote=127.0.0.1:xxxxx
level=INFO msg="text received" remote=... body="привет из узла B"
```

> Примечание: этот сценарий проверяет **только транспорт** (этап 1).
> Он не использует DHT. Для полноценного обмена нужен этап 5.

### Сценарий 2: три узла, bootstrap

Терминал 1 (seed, не закрывать):

```bash
go run ./cmd/node -listen 127.0.0.1:9001 -log debug
```

Терминал 2:

```bash
go run ./cmd/node -listen 127.0.0.1:9002 \
    -bootstrap 127.0.0.1:9001 -log debug
```

Терминал 3:

```bash
go run ./cmd/node -listen 127.0.0.1:9003 \
    -bootstrap 127.0.0.1:9001 -log debug
```

Ожидаемое поведение:

- Node 2 знает seed (1 peer).
- Node 3 знает seed и node 2 (2 peers).
- Через несколько секунд node 2 тоже узнаёт о node 3 (когда node 3
  пинганул его на шаге 3 bootstrap).

В логе каждого узла — `known peer` со списком реальных ID и адресов.

### Сценарий 3: пять узлов, проверка сходимости

Запускаем пять узлов с одним seed'ом:

```bash
# терминал 1
go run ./cmd/node -listen 127.0.0.1:9001 -log debug

# терминалы 2–5
for port in 9002 9003 9004 9005; do
  go run ./cmd/node -listen 127.0.0.1:$port \
      -bootstrap 127.0.0.1:9001 -log debug &
done
```

К концу запуска каждый узел должен знать хотя бы 3 других.

## Тесты

```bash
go test ./... -v
```

Покрытие по пакетам:

### `internal/transport/tcp`
- `TestFrameRoundTrip` — кадры разной длины (0, 5, 1 КБ, 64 КБ, 1 МБ).
- `TestReadFrame_BadMagic`, `TestReadFrame_TooLarge`, `TestReadFrame_EOF`,
  `TestReadFrame_TruncatedPayload` — обработка ошибок.
- `TestTwoNodesExchange` — два узла на localhost, обмен.
- `TestManyFrames` — 1000 кадров разной длины подряд.
- `TestDialTimeout` — недоступный адрес.

### `internal/dispatch`
- `TestRegisterAndDispatch`, `TestDispatchUnknownType`,
  `TestRegisterDuplicatePanics`, `TestServeReadsUntilEOF`,
  `TestServeContinuesAfterHandlerError`.

### `internal/dht`
- `TestDistanceSymmetric`, `TestDistanceSelfZero`,
  `TestTriangleInequality`, `TestCommonPrefixLen`, `TestCloserTo`,
  `TestIDHexRoundTrip` — метрика.
- `TestBucketIndex`, `TestAddAndGet`, `TestAddDuplicateUpdates`,
  `TestBucketEviction`, `TestClosest`, `TestRemove` — таблица
  маршрутизации.
- `TestPingRoundTrip`, `TestFindNodeResponseRoundTrip`,
  `TestDecodeGarbage` — сериализация.
- `TestBootstrap3Nodes`, `TestBootstrap5Nodes`, `TestPingDeadNode`,
  `TestUniqueSorted` — интеграционные.

## Roadmap

| Этап | Что | Статус |
|------|-----|--------|
| 1 | Транспорт: кадры, TCP, диспетчер | готово |
| 2 | DHT: ID, XOR, k-buckets, PING, FIND_NODE, bootstrap | готово |
| 3 | DHT: STORE, FIND_VALUE, TTL, репликация | в работе |
| 4 | Крипто: X25519, Ed25519, AEAD, anti-replay | soon |
| 5 | Прикладной сервис: сообщения + файлы | soon |
| 6 | Docker Compose, 5–7 узлов, 2 топологии, отказ | soon |
| 7 | Метрики: сходимость, RTT, время отказа | soon |
| 8 | Опционально: netem, NAT, Sybil | soon |
| 9 | Записка, USERGUIDE, презентация | soon |

## Ограничения и упрощения

1. **Транспорт только TCP.** UDP не реализован, но интерфейс `Conn`
   абстрагирован — добавление UDP не потребует переписывания DHT.
2. **Вытеснение из бакета без PING oldest.** Классическая Kademlia
   сначала проверяет, жив ли самый старый узел, и только потом вытесняет.
   У нас — просто вытесняем. Для сети 5–7 узлов переполнение бакета
   невозможно (K=8), так что это чисто теоретическое упрощение.
3. **Нет периодического refresh бакетов.** Узел не пингует соседей
   регулярно, чтобы обнаружить отказ. Будет добавлено на этапе 6
   (измерение времени обнаружения отказа).
4. **Нет re-publish и expire** (этап 3, в работе).
5. **Нет шифрования канала** (этап 4, в работе).
6. **Хранилище in-memory.** При перезапуске узла данные теряются; re-publish
   от издателя восстановит записи.
7. **ID генерируется случайно**, а не из публичного ключа. На этапе 4
   будет `SHA-256(pubkey)`.
8. **Bootstrap не гарантирует сходимость всей сети** за один проход —
   только регистрацию нового узла у seed и его окружения. Для полной
   сходимости нужен либо периодический refresh, либо повторный bootstrap.
