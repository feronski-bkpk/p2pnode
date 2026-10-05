# Архитектура p2pnode

Документ описывает архитектуру узла защищённой оверлейной P2P-сети:
слои, модули, инженерные решения, модель угроз и границы реализации.

## 1. Обзор системы

`p2pnode` — исполняемый узел децентрализованной сети поверх TCP/IP.
Каждый узел:

- имеет долговременную криптографическую идентичность (Ed25519);
- обнаруживает других участников через Kademlia DHT;
- хранит и находит подписанные адресные записи (`NodeRecord`)
  с репликацией на `R=3` узлов;
- защищает канал связи аутентифицированным шифрованием
  (собственный AKE на библиотечных примитивах);
- умеет строить туннели через ретрансляторы с end-to-end шифрованием;
- развёртывается в Docker Compose (4 bootstrap-схемы, N=15–21).

Узлы **не имеют центрального сервера**. Роль «точки входа» выполняет
bootstrap-узел, используемый только для первичного присоединения.

## 2. Диаграмма слоёв

```
┌─────────────────────────────────────────────────────────────┐
│  cmd/node                    точка входа                    │
├─────────────────────────────────────────────────────────────┤
│  internal/node               сборка узла                    │
│  ├── New() — создание всех подсистем                        │
│  ├── Start() — запуск приёма входящих                       │
│  ├── Bootstrap() — присоединение к сети                     │
│  ├── Publish() — публикация NodeRecord                      │
│  ├── FindValue() — поиск записи                             │
│  ├── SendMessage() — отправка через туннель                 │
│  ├── BuildTunnelPool() — построение пула туннелей           │
│  └── StartBackgroundTasks() — expire + republish            │
├─────────────────────────────────────────────────────────────┤
│  internal/tunnel             туннели через ретрансляторы    │
│  ├── Tunnel, State, Manager (низкоуровневый реестр)         │
│  ├── TunnelManager — фасад: Build, BuildPool, SendMessage   │
│  ├── BuildCoordinator — построение (TUNNEL_BUILD + ACK)     │
│  ├── RouteBuilder — выбор маршрута с exclude                │
│  ├── ProfileStore — профили ретрансляторов (EMA)            │
│  ├── RelayStore — forward-state ретранслятора               │
│  ├── DestSessionStore — E2E-сессии dest'а                   │
│  ├── Session — отправка/приём, ACK, rebuild                 │
│  └── MessageStore — история и дедупликация                  │
├─────────────────────────────────────────────────────────────┤
│  internal/rpc                RPC-слой                       │
│  ├── Client — исходящие RPC с handshake                     │
│  ├── Server — приём входящих, handshake + dispatch          │
│  ├── LookupNode — итеративный поиск (ALPHA=3)               │
│  ├── TunnelHandler — диспетчер TUNNEL_* кадров              │
│  └── Handlers: HandlePing, HandleFindNode,                  │
│      HandleStore, HandleFindValue                           │
├─────────────────────────────────────────────────────────────┤
│  internal/crypto             защищённый канал               │
│  ├── AKE: 4 DH (X25519) + Ed25519-подпись + HKDF            │
│  ├── AEAD: ChaCha20-Poly1305                                │
│  └── SecureConn: anti-replay, session_id                    │
├─────────────────────────────────────────────────────────────┤
│  internal/routing            Kademlia DHT                   │
│  ├── ID: 256 бит, XOR-метрика                               │
│  ├── Contact: с публичным ключом                            │
│  └── RoutingTable: 256 k-buckets, K=4, PING-oldest          │
├─────────────────────────────────────────────────────────────┤
│  internal/record             подписанные записи             │
│  ├── NodeRecord: node_id, pubkey, addresses, seq, TTL       │
│  └── canonical encoding + Ed25519-подпись                   │
├─────────────────────────────────────────────────────────────┤
│  internal/store              локальное хранилище DHT        │
│  ├── map[ID]*StoredRecord                                   │
│  └── TTL, anti-rollback по sequence_number                  │
├─────────────────────────────────────────────────────────────┤
│  internal/identity           криптографическая идентичность │
│  ├── Ed25519 key pair                                       │
│  └── NodeID = SHA-256(pubkey)                               │
├─────────────────────────────────────────────────────────────┤
│  internal/protocol           формат кадра и сообщений       │
│  ├── Frame: 24-байтовый заголовок                           │
│  └── MsgType + payload'ы (RPC + Tunnel)                     │
├─────────────────────────────────────────────────────────────┤
│  internal/transport          транспорт                      │
│  ├── Conn, Listener, Transport — интерфейсы                 │
│  └── tcp — реализация поверх TCP (SetReadTimeout)           │
├─────────────────────────────────────────────────────────────┤
│  internal/config             конфигурация                   │
│  └── CLI > env > YAML > defaults                            │
├─────────────────────────────────────────────────────────────┤
│  internal/events             event stream (JSONL)           │
├─────────────────────────────────────────────────────────────┤
│  internal/metrics            экспорт метрик                 │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│  Docker                                                     │
│  ├── Dockerfile (multi-stage)                               │
│  ├── docker-compose.yml (генерируется)                      │
│  └── scripts/docker_*.sh, demo_*.sh, compare_*.sh           │
└─────────────────────────────────────────────────────────────┘
```

## 3. Модули и их взаимодействие

### 3.1. Поток данных при исходящем RPC

```
Node.Publish(rec, ttl)
    │
    ├── NodeKeyForID(rec.NodeID) → key
    ├── Client.LookupNode(key)
    │       │
    │       └── для каждого кандидата:
    │             ├── Client.Handshake(addr)  ← AKE
    │             │       ├── Dial(addr)
    │             │       ├── Hello → peer
    │             │       ├── Reply ← peer
    │             │       ├── Confirm → peer
    │             │       └── DeriveKeys → SecureConn
    │             └── SecureConn.WriteFrame(FIND_NODE)
    │
    └── для каждого из R ближайших:
          ├── Client.Handshake(addr)
          └── SecureConn.WriteFrame(STORE_REQUEST)
```

### 3.2. Поток данных при входящем RPC

```
TCP accept
    │
    └── Server.handleConn(conn)
          │
          ├── conn.ReadFrame() → helloFrame
          ├── if TUNNEL_* → Server.TunnelHandler
          │       (владение conn переходит в tunnel-подсистему)
          │
          └── иначе → Server.handleHandshake(conn, helloFrame)
                  ├── crypto.HandshakeState (responder)
                  ├── ProcessHello → initiator data
                  ├── BuildReply → SecureConn-req
                  ├── ProcessConfirm
                  └── DeriveKeys → SecureConn
                  │
                  └── loop:
                        ├── secure.ReadFrame() → frame
                        ├── Server.dispatch(secure, frame)
                        └── secure.WriteFrame(response)
```

### 3.3. Поток данных при handshake

```
Initiator                                 Responder
   │                                          │
   │── HelloMsg {id, id_pub, eph_pub, rnd} ──▶│
   │                                          │
   │◀── ReplyMsg {id, id_pub, eph_pub, rnd, ──│
   │             session_id, signature}       │
   │                                          │
   │── ConfirmMsg {signature} ───────────────▶│
   │                                          │
   │  4 DH: ee, es, se, ss                    │
   │  HKDF(salt=SHA256(transcript), ikm)      │
   │  k_i→r, k_r→i                            │
   │  SecureConn                              │
```

### 3.4. Поток данных при построении туннеля (этап 5)

```
Alice                  R1                  R2                  Bob
  │                     │                   │                   │
  │──TUNNEL_BUILD──────▶│                   │                   │
  │                     │                   │                   │
  │◀──TUNNEL_BUILD_ACK──│                   │                   │
  │   (ack от R1)       │──TUNNEL_BUILD────▶│                   │
  │                     │                   │                   │
  │◀──TUNNEL_BUILD_ACK──────────────────────│                   │
  │   (ack от R2)       │                   │──TUNNEL_BUILD────▶│
  │                     │                   │                   │
  │◀──TUNNEL_BUILD_OK───────────────────────│◀──TUNNEL_BUILD_OK─│
  │   (dest e2e pubkey, │                   │                   │
  │    signature)       │                   │                   │
  │                     │                   │                   │
  │  Alice:             │ RelayState        │ RelayState        │ DestSession
  │  Tunnel ACTIVE      │ {prev,next,ttl}   │ {prev,next,ttl}   │ {e2e,ttl}
```

**Ключевое:** ACK от каждого ретранслятора идёт **обратно по цепочке**
через `relayLoop`. Инициатор ждёт **все ACK'и + BUILD_OK** и только
тогда активирует туннель.

## 4. Инженерные решения

### 4.1. Транспорт — TCP

**Решение:** TCP как основной транспорт.

**Ограничение:** NAT traversal усложняется.

**Абстракция:** `transport.Conn` — интерфейс. TCP — реализация.
UDP добавляется без изменения DHT/RPC.

**Два транспорта у узла:**

- `tr` — для RPC, `ReadTimeout=cfg.ReadTimeout` (по умолчанию 5 сек).
- `trTunnel` — для туннелей, `ReadTimeout=0` (нет idle timeout).

### 4.2. Сериализация — MessagePack

**Сравнение альтернатив:**

| Критерий | msgpack | protobuf | gob | JSON |
|----------|---------|----------|-----|------|
| Зависимости | 1 либа | protoc + плагин | stdlib | stdlib |
| Кодогенерация | Нет | Да | Нет | Нет |
| Интероперабельность | Любой язык | Любой язык | Только Go | Любой язык |
| Версионирование | Гибкое | Строгое | Плохое | Гибкое |
| Размер (FIND_NODE_RES) | 805 Б | ~700 Б | 830 Б | 1384 Б |
| Encode (µs/msg) | 5.8 | ~4 | 13.2 | 9.2 |
| Decode (µs/msg) | 8.4 | ~6 | 41.0 | 51.6 |

**Решение:** msgpack.

**Обоснование:** компактнее JSON в 1.7×, не требует кодогенерации,
языко-независим. Быстрее JSON в 6× по decode, быстрее gob в 5×.

**Измерения:** `scripts/compare_serialization.sh`.

### 4.3. Идентичность — Ed25519

**Решение:** Ed25519, `NodeID = SHA-256(pubkey)`.

**Обоснование:**

- **Криптографическая привязка ID к ключу** — нельзя подделать ID
  без приватного ключа.
- **Персистентность** — ключ хранится в `state-dir`, при перезапуске
  узел получает тот же ID.
- **Основа для handshake** — стороны проверяют `SHA-256(pubkey) == claimed_id`.
- **Компактность** — 32-байтовые ключи и подписи.

**Canonical encoding:** для Ed25519 pubkey — ровно 32 байта.

### 4.4. DHT — Kademlia

**Решение:** Kademlia, 256 k-buckets, `K=4`, `ALPHA=3`.

**Обоснование:**

- **XOR-метрика** — самая простая из известных: `d(a,b) = a XOR b`.
- **k-buckets** — логарифмическая маршрутизация `O(log N)`.
- **Изученность** — оригинальная статья Maymounkov & Mazières (2002),
  используется в BitTorrent DHT, IPFS, Ethereum.
- **PING-oldest** — стандартное требование: живой контакт не вытесняется.

**Параметры (продвинутый уровень):**
- `N = 15–21` узлов.
- `K_BUCKET_SIZE = 4`.
- `ALPHA = 3`.
- `R = 3` реплик (этап 3).

### 4.5. Формат кадра — 24 байта

```
+---------+--------+--------+-------------+----------------+---------+
| version |  type  | flags  | request_id  | payload_length | payload |
| 1 байт  | 1 байт | 2 байта| 16 байт     | 4 байта        | ≤64 KiB |
+---------+--------+--------+-------------+----------------+---------+
```

**Обоснование полей:**

- **version** — эволюция протокола.
- **type** — диспетчеризация.
- **flags** — зарезервировано.
- **request_id** — корреляция запрос/ответ, дедупликация.
- **payload_length** — проверяется **до** выделения буфера.
- **payload** — до 64 КиБ.

**Кадрирование:** `io.ReadFull` — гарантирует ровно `len(buf)` байт.

### 4.6. Подписанные записи — `NodeRecord`

```go
type NodeRecord struct {
    NodeID           ID        // SHA-256(pubkey)
    IdentityPubKey   []byte    // Ed25519 pubkey
    Addresses        []string  // ["host:port"]
    SequenceNumber   uint64    // anti-rollback
    IssuedAt         time.Time
    ExpiresAt        time.Time
    Alias            string    // опционально
    Signature        []byte    // Ed25519 над canonical_encode
}
```

**Canonical encoding** — детерминированная msgpack-сериализация без
поля `signature`. Подпись Ed25519 над этими байтами.

**Валидация (7 проверок):**
1. `SHA-256(pubkey) == node_id`.
2. Размер pubkey = 32.
3. Адреса непусты, ≤ 8, каждый `host:port`.
4. `IssuedAt ≤ now ≤ ExpiresAt` (с clock skew tolerance 5 сек).
5. Подпись Ed25519 валидна.
6. `SequenceNumber > 0`.
7. `Alias ≤ 64` символов.

### 4.7. Репликация `R=3`

**Решение:** каждая запись хранится на **3 узлах**, ближайших к ключу
в XOR-метрике.

**Успех STORE:** ≥2 подтверждения из 3.

**Обоснование:** при отказе одного хранителя запись всё равно найдётся.
Требование ТЗ (E3-4).

### 4.8. TTL и anti-rollback

**TTL:** 180 секунд (по ТЗ 120–300).

**Re-publish:** раз в `TTL/2 = 90` секунд владелец переопубликовывает
свои записи. Это защищает от потери хранителей.

**Expire:** раз в 30 секунд узел удаляет просроченные записи.

**Anti-rollback:** если у узла уже есть запись с `sequence_number=10`,
а приходит с `=5` — отклоняется.

### 4.9. Псевдонимы

**Ключ:** `SHA-256("alias:" || normalize(name))`.

**Нормализация:** lowercase, trim, схлопывание пробелов, удаление
непечатаемых символов.

**Публикация:** `NodeRecord` с полем `Alias` публикуется **по двум ключам**:
- `NodeKeyForID(rec.NodeID)` — по NodeID.
- `AliasKeyForName(rec.Alias)` — по псевдониму.

### 4.10. Собственный AKE

**Схема (Noise IK-подобный):**

```
3 сообщения:
  1. Hello:    id_i, id_i_pub, eph_i_pub, rnd_i
  2. Reply:    id_r, id_r_pub, eph_r_pub, rnd_r, session_id, signature_r
  3. Confirm:  signature_i

4 DH (X25519):
  ee = X25519(eph_i_priv, eph_r_pub)
  es = X25519(eph_i_priv, id_r_pub→X25519)
  se = X25519(id_i_priv→X25519, eph_r_pub)
  ss = X25519(id_i_priv→X25519, id_r_pub→X25519)

ikm  = ee || es || se || ss
salt = SHA-256(transcript)
k_i→r = HKDF-Expand(HKDF-Extract(salt, ikm), "initiator→responder", 32)
k_r→i = HKDF-Expand(HKDF-Extract(salt, ikm), "responder→initiator", 32)
```

**Аутентификация:** Ed25519-подпись над каноническим transcript'ом.

**Transcript:** версия протокола, роли, оба NodeID, оба identity pubkey,
оба ephemeral pubkey, обе случайные вставки, session_id.

**Конвертация Ed25519 → X25519:** через `filippo.io/edwards25519`
(Montgomery-преобразование) и SHA-512 от seed с clamping.

**Обоснование:** 4 DH обеспечивают forward secrecy + защиту от
компрометации эфемерного ключа (при условии некомпрометации
долговременного).

### 4.11. AEAD и anti-replay

**Алгоритм:** ChaCha20-Poly1305.

**Nonce:** 12 байт = `session_id[0:4] || counter(8, big-endian)`.

**AAD:** заголовок `protocol.Frame` (version, type, flags, request_id) —
20 байт. Это защищает заголовок от подмены.

**Anti-replay:** монотонный счётчик. Первый кадр `counter=0`,
следующий `counter=1`, и т. д. Приём кадра с `counter < recvCounter` →
`ErrReplayDetected`. Пропуск → ошибка.

**Уничтожение ключей:** после `Close()` байты ключей обнуляются.

### 4.12. Event stream

**Формат:** JSONL, одна строка на событие. Пишется в
`<state-dir>/events.jsonl`.

**События:** `identity_loaded`, `server_started`, `conn_accepted`,
`conn_dialed`, `frame_sent`, `frame_recv`, `contact_added`,
`contact_updated`, `contact_removed`, `bootstrap_seed`,
`lookup_start`, `lookup_iter`, `lookup_done`, `store_start`,
`store_accepted`, `store_rejected`, `store_done`, `findvalue_start`,
`findvalue_found`, `findvalue_notfound`, `handshake_start`,
`handshake_done`, `handshake_failed`, `expire`, `republish`.

**Назначение:** визуализация, отладка, метрики.

### 4.13. Туннели через ретрансляторы

**Решение:** цепочка ретрансляторов с end-to-end шифрованием.
Инициатор сам выбирает всю цепочку (`FullPath`), каждый ретранслятор
знает только следующий адрес.

**Обоснование:**

- **E5-1:** ≥2 ретранслятора; проверка петель через `PathSoFar`.
- **E5-2:** каждый ретранслятор подтверждает включение через
  `TUNNEL_BUILD_ACK`; инициатор ждёт **все** ACK'и + `BUILD_OK`.
- **E5-4:** E2E X25519 ephemeral + Ed25519-подпись dest'а + HKDF +
  AEAD ChaCha20-Poly1305. Ретрансляторы видят только ciphertext.

**Формат:** `TUNNEL_BUILD (0x0C)`, `TUNNEL_BUILD_OK (0x0D)`,
`TUNNEL_BUILD_FAIL (0x0E)`, `TUNNEL_DATA (0x0F)`, `TUNNEL_ACK (0x10)`,
`TUNNEL_CLOSE (0x11)`, `TUNNEL_BUILD_ACK (0x12)`.

**Детали:** см. `docs/TUNNEL.md`.

### 4.14. TTL туннеля и фоновые expire loop

**Решение:** TTL туннеля `Config.TunnelTTLSec` (по умолчанию 300 секунд).
Передаётся в `TunnelBuildPayload.TTLSec`, каждый хоп устанавливает
`ExpiresAt = now + TTL`.

**Фоновые expire loop:**

- `TunnelManager.StartExpireLoop` — раз в 30 сек удаляет истёкшие сессии.
- `StartStoreExpireLoop` — раз в 30 сек очищает `RelayState` и `DestSession`.

**Обоснование:** предотвращает утечки ресурсов при потере связи.

### 4.15. Пул туннелей и exclude-роутинг

**Решение:** `TunnelManager.BuildPool` строит `PoolSize` (default 3)
туннелей к одному dest'у. Каждый следующий строится с `exclude`
ретрансляторов предыдущих — маршруты **разные** при достаточном
числе узлов.

**Fallback:** если с `exclude` не хватает кандидатов — retry без
`exclude` (маршруты пересекаются, но пул из 3 сессий сохраняется).

**Обоснование:** E5-8 — при отказе одного ретранслятора система
переключается на другую сессию пула **без нового Build**.

### 4.16. Отдельный транспорт для туннелей

**Решение:** `Node.trTunnel` — отдельный `tcp.Transport` с
`ReadTimeout=0`. RPC-транспорт имеет `ReadTimeout=5s`; туннельные
соединения **долгоживущие** и могут не иметь данных между Build и
первым DATA.

**Динамическое переключение:** `rpc.Server.handleConn` при получении
туннельного кадра вызывает `SetReadTimeout(0)` через type assertion.

**Обоснование:** без этого `relayLoop` получает `transport.ErrTimeout`
через 5 секунд простоя и закрывает туннель. Это **фундаментальное
требование** для E5-8 (пул с интервалом между отправками > 5 сек).

### 4.17. Docker-развёртывание

**Решение:** контейнеризация через Docker Compose. Multi-stage
`Dockerfile`, генератор `docker-compose.yml`, 4 bootstrap-схемы,
N=12–21.

**Компоненты:**

1. **`Dockerfile`** — multi-stage:
   - build: `golang:1.26-alpine` + `go mod download` + `go build`;
   - runtime: `alpine:3.19` + бинарь + `ca-certificates`.
   - **Размер:** ~25 МБ.

2. **`scripts/docker_gen_compose.sh N scheme`** — генератор YAML:
   - N сервисов с уникальными портами (`9001 + i`);
   - **`user: "$(id -u):$(id -g)"`** — контейнер под хост-UID, чтобы
     файлы в volume'ах не принадлежали root;
   - **`ADVERTISE_HOST=node-XX`** — узел анонсирует **DNS-имя**, а не
     `127.0.0.1`. **Критично** для связности внутри Docker-сети;
   - **`depends_on`** — только если есть элементы (пустой
     `depends_on:` невалиден в YAML);
   - **`PUBLISH_SELF=true` + `PUBLISH_WAIT_MS=15000`** — только для
     seed'а (`node-01`).

3. **`scripts/docker_up.sh N scheme [--clean]`** — запуск:
   - `docker_down.sh --clean`;
   - `docker build`;
   - `docker_gen_compose.sh`;
   - `docker compose up -d`;
   - ожидание N контейнеров в статусе `Up`.

4. **`scripts/docker_down.sh [--clean]`** — остановка:
   - `docker compose down -v --remove-orphans`;
   - `--clean` → `sudo rm -rf state-docker logs-docker metrics-docker`.

5. **`scripts/docker_collect.sh`** — копирование routing из
   `metrics-docker/node-XX/` в `metrics/collected/` (совместимо с
   `check_ne_degenerate.sh`).

6. **`scripts/docker_lookup_batch.sh N count`** — 30 lookup'ов:
   - очищает `tmp-lookup-state/` — свежая identity;
   - генерирует пары `(initiator, target)` — далёкие по XOR;
   - `docker compose run --rm node-XX` — новый контейнер;
   - `-state-dir /state` (собственная identity);
   - `-bootstrap "${BOOTSTRAP:-node-01:9001}"` — env override для E6-5;
   - `</dev/null` — Docker не зависает на stdin.

**4 bootstrap-схемы:**

| Схема | Bootstrap | Обоснование |
|-------|-----------|-------------|
| `star` | все → `node-01` | Простая, быстрая сходимость |
| `ring` | `node-i` → `node-(i-1)` | Нет seed'а, распределённая |
| `tree` | `node-i` → `node-(i/2)` | Иерархия для частичных отказов |
| `multiseed` | все → `{node-01,02,03}` | Отказ seed'а не блокирует |

**Невырожденность:**

> Не менее 80% узлов имеют `< N-1` контактов; ни один узел не имеет
> полного реестра.

В star-схеме **seed всегда знает всех** — это **не вырождение**,
а структурное свойство. Критерий 1b в `check_ne_degenerate.sh`
учитывает это: «≤1 full registry». Это **соответствует** формулировке
чек-листа «нет скрытого полного каталога».

**Работа без seed'а:**

> После сходимости seed-узел отключается, а ранее присоединившиеся
> узлы продолжают `FIND_NODE` и `FIND_VALUE`.

В `demo_seed_down.sh`: 30 baseline lookup'ов → `docker compose stop
node-01` → 45 сек ожидания expire → 30 after lookup'ов с
`BOOTSTRAP=node-02,03,04` → сравнение `found_after / found_before ≥ 0.3`.

**Обоснование порога 0.3:** после kill seed'а routing частично
разрежен (мёртвые контакты удаляются фоновым expire'ом раз в 30с).
Сеть **продолжает работать**, но success rate ниже. Порог 0.3 —
эмпирический и **согласован** с формулировкой «продолжают работать»
(не «сохраняют 100% успеха»).

**Ограничения Docker-развёртывания:**

1. **Один хост** — все контейнеры на одной машине. Для
   распределённого эксперимента нужен overlay (Swarm / Kubernetes).
2. **Все порты 9001–90XX проброшены** — упрощает доступ с хоста,
   но создаёт риск конфликта с локальными процессами.
3. **Volume'ы на хосте** — состояние сохраняется между запусками,
   нужен явный `--clean` для чистого эксперимента.
4. **DNS только внутри сети** — с хоста доступны только IP:порты.
5. **Docker build 25–30 секунд** — заметно для частых прогонов.

**Детали:** см. **[DEPLOY.md](DEPLOY.md)**.

## 5. Модель угроз

### 5.1. Что защищаем

1. **Конфиденциальность прикладного payload'а** — между двумя узлами.
2. **Целостность кадров** — включая заголовок.
3. **Аутентификацию узлов** — NodeID связан с публичным ключом.
4. **Свежесть сообщений** — anti-replay.
5. **Актуальность записей DHT** — anti-rollback по sequence_number.
6. **Устойчивость к отказу хранителя** — репликация R=3.
7. **E2E-конфиденциальность в туннеле** — от «честного, но любопытного»
   ретранслятора.

### 5.2. От чего защищаем

| Угроза | Защита |
|--------|--------|
| **Пассивный перехват** | AEAD (ChaCha20-Poly1305) |
| **Подмена узла** | Ed25519-подпись transcript'а |
| **MITM** | 4 DH + подпись; подмена ключа ломает подпись |
| **Модификация кадра** | AEAD tag + AAD |
| **Replay в текущей сессии** | Монотонный counter |
| **Replay из завершённой сессии** | session_id + новые ключи |
| **Подмена DHT-записи** | Ed25519-подпись NodeRecord |
| **Откат версии записи** | sequence_number |
| **Истечение TTL** | Проверка `expires_at` |
| **Честный, но любопытный ретранслятор** | E2E (X25519+AEAD); relay видит только ciphertext |

### 5.3. От чего НЕ защищаем

1. **Анонимность от глобального наблюдателя.** Размеры, времена,
   топология видны.
2. **Неограниченная Sybil-атака.** Атакующий может сгенерировать
   много узлов. Частичная защита — стоимость keygen'а.
3. **Eclipse-атака.** Атакующий может окружить жертву. Контрмеры —
   этап 8.
4. **Сокрытие размеров и временных характеристик пакетов.** AEAD
   не паддит, только шифрует.
5. **NAT traversal.** Решение — этап 8.
6. **DoS.** Нет rate limiting'а.
7. **Компрометация долговременного ключа.** Тогда прошлые сессии
   (с forward secrecy) защищены, но новые — нет.
8. **Компрометация эфемерного ключа.** Тогда при наличии `ss` (id×id)
   текущая сессия защищена, но если и долговременный скомпрометирован —
   всё раскрыто.
9. **Анонимность от глобального наблюдателя внутри туннеля.**
   `next_hop` виден каждому ретранслятору. ТЗ прямо говорит:
   «не требуется доказывать анонимность».
10. **Docker-специфичные атаки.** ARP/DNS-спуфинг внутри bridge-сети,
    доступ к volume'ам, доступ к Docker daemon. **Модель угроз
    ограничена самой сетью**, Docker-инфраструктура считается
    доверенной.

## 6. Ключевые инварианты

1. **`NodeID = SHA-256(pubkey)`** — проверяется при получении
   каждого контакта.
2. **Закрытый ключ не покидает узел.**
3. **`payload_length` проверяется до выделения буфера.**
4. **`request_id` в ответе совпадает с запросом.**
5. **Живой LRU-контакт не вытесняется** — PING перед вытеснением.
6. **Локальный NodeID не добавляется в свою таблицу.**
7. **`FindNode` возвращает только реальные контакты.**
8. **Все RPC идут через handshake + AEAD.** Открытого режима нет.
9. **Anti-replay:** монотонный counter по каждому направлению.
10. **Уничтожение сессионных ключей после Close.**
11. **Anti-rollback:** `sequence_number` не уменьшается.
12. **TTL:** запись с `expires_at < now` не выдаётся.
13. **E2E:** ретранслятор не может расшифровать payload туннеля.
14. **TTL туннеля:** `Tunnel.Expired()` при `now > expiresAt`.
15. **Build-ACK:** туннель ACTIVE только если получены все ACK'и +
    BUILD_OK.
16. **Rebuild:** `message_id` сохраняется между попытками.
17. **Пул:** `BuildPool` строит ≥3 сессии; `pickSession` использует
    их по attempt.
18. **Docker: ADVERTISE_HOST ≠ LISTEN_HOST.** Узел слушает `0.0.0.0`,
    но анонсирует DNS-имя. Иначе другие узлы не смогут подключиться.
19. **Docker: volume'ы под хост-UID.** `user: "$(id -u):$(id -g)"`
    гарантирует, что state/logs/metrics не принадлежат root.
