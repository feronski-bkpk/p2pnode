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
  (собственный AKE на библиотечных примитивах).

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
│  └── StartBackgroundTasks() — expire + republish            │
├─────────────────────────────────────────────────────────────┤
│  internal/rpc                RPC-слой                       │
│  ├── Client — исходящие RPC с handshake                     │
│  ├── Server — приём входящих, handshake + dispatch          │
│  ├── LookupNode — итеративный поиск (ALPHA=3)               │
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
│  └── MsgType + payload'ы                                    │
├─────────────────────────────────────────────────────────────┤
│  internal/transport          транспорт                      │
│  ├── Conn, Listener, Transport — интерфейсы                 │
│  └── tcp — реализация поверх TCP                            │
├─────────────────────────────────────────────────────────────┤
│  internal/config             конфигурация                   │
│  └── CLI > env > YAML > defaults                            │
├─────────────────────────────────────────────────────────────┤
│  internal/events             event stream (JSONL)           │
├─────────────────────────────────────────────────────────────┤
│  internal/metrics            экспорт метрик                 │
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
          ├── Server.handleHandshake(conn, helloFrame)
          │       ├── crypto.HandshakeState (responder)
          │       ├── ProcessHello → initiator data
          │       ├── BuildReply → SecureConn-req
          │       ├── ProcessConfirm
          │       └── DeriveKeys → SecureConn
          │
          └── loop:
                ├── secure.ReadFrame() → frame (расшифрован)
                ├── Server.dispatch(secure, frame)
                │       └── HandlePing / HandleFindNode / ...
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

## 4. Инженерные решения

### 4.1. Транспорт — TCP

**Решение:** TCP как основной транспорт.

**Ограничение:** NAT traversal усложняется.

**Абстракция:** `transport.Conn` — интерфейс. TCP — реализация.
UDP добавляется без изменения DHT/RPC.

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
- **flags** — зарезервировано под этапы 5+.
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

## 5. Модель угроз

### 5.1. Что защищаем

1. **Конфиденциальность прикладного payload'а** — между двумя узлами.
2. **Целостность кадров** — включая заголовок.
3. **Аутентификацию узлов** — NodeID связан с публичным ключом.
4. **Свежесть сообщений** — anti-replay.
5. **Актуальность записей DHT** — anti-rollback по sequence_number.
6. **Устойчивость к отказу хранителя** — репликация R=3.

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
