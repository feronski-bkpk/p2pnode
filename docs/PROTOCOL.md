# Протокол p2pnode

Документ описывает формат кадра, типы сообщений, payload'ы и алгоритмы протокола узла p2pnode, включая туннели через ретрансляторы.

## 1. Формат кадра

Все сообщения передаются поверх TCP с **собственным кадрированием**.
Заголовок — **24 байта**, все многобайтные числа — big-endian.

```
+---------+--------+--------+-------------+----------------+---------+
| version |  type  | flags  | request_id  | payload_length | payload |
| 1 байт  | 1 байт | 2 байта| 16 байт     | 4 байта        | ≤64 KiB |
+---------+--------+--------+-------------+----------------+---------+
```

| Поле | Размер | Назначение |
|------|--------|-----------|
| `version` | 1 | Версия протокола |
| `type` | 1 | Тип сообщения |
| `flags` | 2 | Зарезервировано |
| `request_id` | 16 | Уникальный ID запроса, `crypto/rand` |
| `payload_length` | 4 | Длина payload, проверяется **до** выделения буфера |
| `payload` | ≤65536 | Сериализованное сообщение (msgpack) |

**Кадрирование:** `io.ReadFull`. Один `read()` может вернуть меньше
байт, чем запрошено.

**Максимальный размер payload:** `MAX_FRAME_PAYLOAD = 65536`.

**Ответ:** содержит **тот же `request_id`**, что и запрос. Иначе
клиент отклоняет ответ (`ErrRequestIDMismatch`).

### Защищённый кадр

После handshake payload исходного кадра шифруется AEAD. Структура
**payload** (24-байтовый заголовок остаётся):

```
+-------------+--------+--------+---------------------+
| session_id  | nonce  |  len   |    ciphertext       |
| 16 байт     | 12 байт| 4 байта|  len = payload+tag  |
+-------------+--------+--------+---------------------+
```

- **session_id** — 16 байт, из handshake.
- **nonce** — 12 байт = `session_id[0:4] || counter(8, big-endian)`.
- **len** — длина ciphertext (payload + 16-байтовый тег AEAD).
- **ciphertext** — AEAD.Seal(nonce, plaintext, aad).
- **AAD** — 20 байт: `version || type || flags || request_id`.

### Туннельный кадр

Кадры `TUNNEL_*` идут **без handshake**. 24-байтовый заголовок
остаётся, payload — открытый msgpack. Внутри `payload` содержится
`ciphertext` (E2E-зашифрованный), который **ретранслятор не читает**.

## 2. Типы сообщений

| Код | Имя | Направление | Этап |
|-----|-----|-------------|------|
| `0x01` | `PING` | запрос | 2 |
| `0x02` | `PONG` | ответ | 2 |
| `0x03` | `FIND_NODE_REQUEST` | запрос | 2 |
| `0x04` | `FIND_NODE_RESPONSE` | ответ | 2 |
| `0x05` | `STORE_REQUEST` | запрос | 3 |
| `0x06` | `STORE_RESPONSE` | ответ | 3 |
| `0x07` | `FIND_VALUE_REQUEST` | запрос | 3 |
| `0x08` | `FIND_VALUE_RESPONSE` | ответ | 3 |
| `0x09` | `HANDSHAKE_HELLO` | запрос | 4 |
| `0x0A` | `HANDSHAKE_REPLY` | ответ | 4 |
| `0x0B` | `HANDSHAKE_CONFIRM` | запрос | 4 |
| `0x0C` | `TUNNEL_BUILD` | запрос | 5 |
| `0x0D` | `TUNNEL_BUILD_OK` | ответ | 5 |
| `0x0E` | `TUNNEL_BUILD_FAIL` | ответ | 5 |
| `0x0F` | `TUNNEL_DATA` | запрос | 5 |
| `0x10` | `TUNNEL_ACK` | ответ | 5 |
| `0x11` | `TUNNEL_CLOSE` | запрос | 5 |
| `0x12` | `TUNNEL_BUILD_ACK` | ответ | 5 |
| `0x7F` | `ERROR` | ответ/уведомление | 1 |

## 3. Сериализация

Выбран **MessagePack** (`github.com/vmihailenco/msgpack/v5`).

**Обоснование:** компактнее JSON в 1.7×, не требует кодогенерации
(protobuf), языко-независим (gob). Измерения — `scripts/compare_serialization.sh`.

### 3.1. Contact

```go
type Contact struct {
    NodeID            [32]byte `msgpack:"node_id"`
    IdentityAlgorithm string   `msgpack:"identity_algorithm"` // "ed25519"
    IdentityPublicKey []byte   `msgpack:"identity_public_key"` // 32 байта
    Host              string   `msgpack:"host"`
    Port              uint16   `msgpack:"port"`
}
```

Локальные метаданные (`LastSeenMs`, `LastVerifiedMs`) **не передаются**
по сети — устанавливаются узлом-наблюдателем.

### 3.2. Payload'ы

**PING / PONG:**

```go
type PingPayload struct {
    Sender      Contact `msgpack:"sender"`
    TimestampMs uint64  `msgpack:"timestamp_ms"`
}

type PongPayload struct {
    Responder            Contact `msgpack:"responder"`
    PingTimestampMs      uint64  `msgpack:"ping_timestamp_ms"`
    ResponderTimestampMs uint64  `msgpack:"responder_timestamp_ms"`
}
```

**FIND_NODE:**

```go
type FindNodeRequestPayload struct {
    Sender       Contact  `msgpack:"sender"`
    TargetNodeID [32]byte `msgpack:"target_node_id"`
}

type FindNodeResponsePayload struct {
    Responder    Contact   `msgpack:"responder"`
    TargetNodeID [32]byte  `msgpack:"target_node_id"`
    Contacts     []Contact `msgpack:"contacts"`
}
```

**STORE:**

```go
type StoreRequestPayload struct {
    Sender Contact  `msgpack:"sender"`
    Key    [32]byte `msgpack:"key"`
    Value  []byte   `msgpack:"value"` // msgpack от record.NodeRecord
    TTLSec int64    `msgpack:"ttl_sec"`
}

type StoreResponsePayload struct {
    OK      bool   `msgpack:"ok"`
    Message string `msgpack:"message,omitempty"`
}
```

**FIND_VALUE:**

```go
type FindValueRequestPayload struct {
    Sender Contact  `msgpack:"sender"`
    Key    [32]byte `msgpack:"key"`
}

type FindValueResponsePayload struct {
    Found bool      `msgpack:"found"`
    Value []byte    `msgpack:"value,omitempty"`
    Nodes []Contact `msgpack:"nodes,omitempty"`
}
```

**HANDSHAKE:**

```go
type HandshakeHelloPayload struct {
    Body []byte `msgpack:"body"` // msgpack от crypto.HelloMsg
}
type HandshakeReplyPayload struct {
    Body []byte `msgpack:"body"`
}
type HandshakeConfirmPayload struct {
    Body []byte `msgpack:"body"`
}
```

**TUNNEL:**

```go
type TunnelBuildPayload struct {
    TunnelID   [16]byte   `msgpack:"tunnel_id"`
    DestID     [32]byte   `msgpack:"dest_id"`
    FullPath   []string   `msgpack:"full_path"`
    HopIndex   uint8      `msgpack:"hop_index"`
    MaxHops    uint8      `msgpack:"max_hops"`
    TTLSec     int64      `msgpack:"ttl_sec"`
    E2EPubKey  []byte     `msgpack:"e2e_pubkey"`
    E2ERandom  []byte     `msgpack:"e2e_random"`
    InitID     [32]byte   `msgpack:"init_id"`
    InitPubKey []byte     `msgpack:"init_pubkey"`
    PathSoFar  [][32]byte `msgpack:"path_so_far"`
}

type TunnelBuildAckPayload struct {
    TunnelID [16]byte `msgpack:"tunnel_id"`
    HopIndex uint8    `msgpack:"hop_index"`
    HopID    [32]byte `msgpack:"hop_id"`
}

type TunnelBuildOKPayload struct {
    TunnelID      [16]byte   `msgpack:"tunnel_id"`
    DestID        [32]byte   `msgpack:"dest_id"`
    DestE2EPubKey []byte     `msgpack:"dest_e2e_pubkey"`
    DestE2ERandom []byte     `msgpack:"dest_e2e_random"`
    DestIDPubKey  []byte     `msgpack:"dest_id_pubkey"`
    Signature     []byte     `msgpack:"signature"`
    Path          [][32]byte `msgpack:"path"`
}

type TunnelBuildFailPayload struct {
    TunnelID  [16]byte `msgpack:"tunnel_id"`
    Reason    string   `msgpack:"reason"`
    FailedHop [32]byte `msgpack:"failed_hop"`
}

type TunnelDataPayload struct {
    TunnelID   [16]byte `msgpack:"tunnel_id"`
    MessageID  [16]byte `msgpack:"message_id"`
    Ciphertext []byte   `msgpack:"ciphertext"`
}

type TunnelAckPayload struct {
    TunnelID  [16]byte `msgpack:"tunnel_id"`
    MessageID [16]byte `msgpack:"message_id"`
}

type TunnelClosePayload struct {
    TunnelID [16]byte `msgpack:"tunnel_id"`
    Reason   string   `msgpack:"reason"`
}
```

**ERROR:**

```go
type ErrorPayload struct {
    Code    string `msgpack:"code"`
    Message string `msgpack:"message"`
}
```

## 4. Идентичность

### 4.1. NodeID

```
NodeID = SHA-256(canonical_encode(identity_public_key))
canonical_encode(pubkey) = pubkey_bytes (32 байта Ed25519)
```

### 4.2. Contact validation

При получении `Contact` узел проверяет:
1. `len(identity_public_key) == 32`.
2. `identity_algorithm == "ed25519"`.
3. `SHA-256(identity_public_key) == node_id`.
4. `host != ""`, `port != 0`.

Невалидный контакт отклоняется.

### 4.3. NodeRecord

```go
type NodeRecord struct {
    NodeID         ID        `msgpack:"node_id"`
    IdentityPubKey []byte    `msgpack:"identity_pubkey"`
    Addresses      []string  `msgpack:"addresses"`
    SequenceNumber uint64    `msgpack:"sequence_number"`
    IssuedAt       time.Time `msgpack:"issued_at"`
    ExpiresAt      time.Time `msgpack:"expires_at"`
    Alias          string    `msgpack:"alias,omitempty"`
    Signature      []byte    `msgpack:"signature"`
}
```

**Canonical encoding:** msgpack-сериализация всех полей **кроме**
`Signature`.

**Signature:** `Ed25519.Sign(identity_priv, canonical_bytes)`.

**Валидация:**
1. `SHA-256(identity_pubkey) == node_id`.
2. Размер `identity_pubkey = 32`.
3. `len(addresses) ∈ [1, 8]`, каждый адрес — `host:port`.
4. `issued_at ≤ now + 5s ≤ expires_at`.
5. Подпись Ed25519 валидна.
6. `sequence_number > 0`.
7. `len(alias) ≤ 64`.

## 5. Алгоритмы RPC

### 5.1. Итеративный lookup

**Входные данные:** `target_id`, `K=4`, `ALPHA=3`.

**Алгоритм:**

```
shortlist = table.Closest(target, K)
queried = {}
result = []
found_target = false

for iter in 0..IDBits:
    toQuery = []
    for ct in shortlist:
        if ct.ID in queried: continue
        if ct.ID == self.ID: continue
        toQuery.append(ct)
        queried.add(ct.ID)
        if len(toQuery) >= ALPHA: break
    if len(toQuery) == 0: break

    parallel:
        for ct in toQuery:
            resp = FindNodeRPC(ct, target)
            if resp.error: timeout
            else:
                for c in resp.contacts:
                    if c.ID == target: found_target = true
                    shortlist.append(c)
                    table.Add(c)

    shortlist = unique_sorted(shortlist, target, K)
    if found_target: break
    if all(shortlist) in queried: break

return shortlist[0:K]
```

**Условие остановки:** target найден ИЛИ все K ближайших опрошены.

### 5.2. Bootstrap

**Входные данные:** `seed_addr`.

```
seed = Ping(seed_addr)   // handshake + PING
table.Add(seed)
result = LookupNode(self.ID)   // обычный режим
// или для -skip-self-lookup:
nodes = FindNodeRPC(seed, self.ID)
for c in nodes: table.Add(c)
```

### 5.3. STORE

**Входные данные:** `rec *NodeRecord`, `ttl`.

```
key = NodeKeyForID(rec.NodeID)
value = rec.Encode()

candidates = LookupNode(key)
R = 3

parallel:
    for c in candidates[0:R]:
        ok = StoreRPC(c, key, value, ttl)
        if ok: replicas++

store.Put(key, rec, now, ttl)   // локально

if replicas >= R/2+1 OR local_stored:
    return success
else:
    return ErrNotEnoughReplicas
```

**Если alias задан:** повторить для `AliasKeyForName(alias)`.

### 5.4. FIND_VALUE

```
if store.Get(key) found: return value

shortlist = table.Closest(key, K)
queried = {}

for iter in 0..IDBits:
    toQuery = первые ALPHA непрошенных из shortlist
    if empty: break

    parallel:
        for c in toQuery:
            resp = FindValueRPC(c, key)
            if resp.found: return resp.value
            for n in resp.nodes:
                shortlist.append(n)
                table.Add(n)

    shortlist = unique_sorted(shortlist, key, K)

return ErrValueNotFound
```

**Early termination:** как только нашли — возвращаем.

### 5.5. Handshake (AKE)

**Инициатор:**

```
1. Dial(addr) → conn
2. hs = NewHandshakeState(id_pub, id_priv, isInitiator=true)
3. hello = hs.BuildHello(self.NodeID)
4. conn.WriteFrame(HANDSHAKE_HELLO, hello)
5. replyFrame = conn.ReadFrame()  // HANDSHAKE_REPLY
6. replyMsg = decode(replyFrame.Payload)
7. if expectedPeerID != nil && replyMsg.ResponderID != expectedPeerID: fail
8. (resp_id, resp_id_pub, resp_eph, resp_rnd) = hs.ProcessReply(replyMsg)
9. confirm = hs.BuildConfirm(...)
10. conn.WriteFrame(HANDSHAKE_CONFIRM, confirm)
11. transcript = TranscriptBytes(...)
12. keys = DeriveKeys(
        hs.ephemeral_priv, resp_eph,
        id_priv, resp_id_pub,
        transcript, hs.session_id,
        isInitiator=true,
    )
13. secureConn = NewSecureConn(conn, keys.k_i→r, keys.k_r→i, keys.session_id)
```

**Responder:** симметрично.

**Транскрипт:**

```
transcript = msgpack {
    protocol: "p2pnode-ake-v1",
    initiator_id, responder_id: [32]byte,
    initiator_id_pub, responder_id_pub: []byte,
    initiator_eph, responder_eph: []byte,
    initiator_rand, responder_rand: []byte,
    session_id: [16]byte,
}
```

**Подписи:**
- Responder подписывает transcript своим Ed25519-ключом.
- Initiator подписывает тот же transcript своим ключом.

**4 DH:**

```
ee = X25519(eph_i_priv, eph_r_pub)
es = X25519(eph_i_priv, id_r_pub→X25519)
se = X25519(id_i_priv→X25519, eph_r_pub)
ss = X25519(id_i_priv→X25519, id_r_pub→X25519)
```

**Вывод ключей:**

```
ikm  = ee || es || se || ss
salt = SHA-256(transcript)
k_i→r = HKDF-Expand(HKDF-Extract(salt, ikm), "initiator→responder", 32)
k_r→i = HKDF-Expand(HKDF-Extract(salt, ikm), "responder→initiator", 32)
```

### 5.6. AEAD

**Алгоритм:** ChaCha20-Poly1305.

**Nonce:** 12 байт = `session_id[0:4] || counter(8, big-endian)`.

**AAD:** 20 байт = `version || type || flags || request_id`.

**Anti-replay:** монотонный counter. `counter < recvCounter` →
`ErrReplayDetected`. Пропуск → ошибка.

**Уничтожение ключей:** после `SecureConn.Close()` байты ключей
обнуляются (`zeroBytes`).

## 6. Алгоритмы туннелей

### 6.1. Построение туннеля

**Инициатор** выбирает `FullPath = [init, relay1, ..., relayN, dest]`.

```
1. Для каждого ретранслятора из FullPath — выбрать адрес.
2. Сгенерировать:
   - tunnel_id (16 байт, crypto/rand)
   - e2e_eph_priv, e2e_eph_pub (X25519)
   - e2e_random (32 байта)
3. Отправить TUNNEL_BUILD с HopIndex=1 первому ретранслятору.
4. Ждать поток ACK'ов и BUILD_OK.
```

**Ретранслятор** на `HopIndex = i` (не последний):

```
1. nextAddr = FullPath[i+1]
2. Dial(nextAddr) → nextConn
3. Отправить TUNNEL_BUILD_ACK {tunnel_id, hop_index=i, hop_id=LocalID}
   в PrevConn (обратно инициатору).
4. Переслать TUNNEL_BUILD с HopIndex=i+1, PathSoFar += LocalID.
5. Запустить relayLoop (prev ↔ next).
6. Сохранить RelayState {tunnel_id, prev, next, expiresAt}.
```

**Dest** на `HopIndex = len(FullPath)-1`:

```
1. Сгенерировать e2e_eph_priv, e2e_eph_pub, e2e_random.
2. Считать transcript.
3. Подписать Ed25519.
4. Ответить TUNNEL_BUILD_OK с {dest_e2e_pubkey, dest_random, signature}.
5. Сохранить DestSession {tunnel_id, e2e, expiresAt}.
6. Запустить destDataLoop.
```

**Инициатор** при получении всех ACK'ов + BUILD_OK:

```
1. Проверить подпись dest'а над transcript.
2. dh = X25519(eph_i_priv, dest_eph_pub)
3. salt = SHA-256(transcript)
4. k_i→d = HKDF-Expand(HKDF-Extract(salt, dh), "initiator→dest", 32)
5. k_d→i = HKDF-Expand(HKDF-Extract(salt, dh), "dest→initiator", 32)
6. Tunnel ACTIVE, E2ESession создана.
```

### 6.2. Передача данных

```
Alice:
  counter = e2e.NextSendNonce()
  ct = Seal(k_i→d, counter, plaintext, aad=tunnel_id)
  payload = nonce(8) || ct
  TUNNEL_DATA {tunnel_id, message_id, ciphertext=payload}

Ретрансляторы: пересылают как есть.

Bob:
  CheckRecvNonce(counter)
  plaintext = Open(k_d→i, nonce, ct, aad=tunnel_id)
  TUNNEL_ACK {tunnel_id, message_id}
```

### 6.3. Восстановление

`TunnelManager.SendMessage`:

```
msgID = NewMessageID()  // один на все попытки
for attempt := 0; attempt < 3; attempt++:
    sess = pickSession(destID, attempt)
    err = sess.SendMessageWithID(msgID, text, AckTimeout)
    if err == nil:
        if attempt > 0: rebuildsOK++
        return msgID, nil
    lastErr = err
    sess.Close()
    removeSession(sess)
return msgID, lastErr (rebuildsFail++)
```

**`pickSession`:**

- E5-8: если `pools[destID]` не пуст → `alive[attempt]`.
- E5-6: если `lastSession[destID]` ACTIVE → использовать.
- Иначе: `Build(destID)`.

### 6.4. TTL и expire

`TunnelBuildPayload.TTLSec` передаётся всем хопам. Каждый хоп:

```
ExpiresAt = now + TTLSec
```

Фоновые expire loop (раз в 30 сек):

```
TunnelManager.expireSessions()   // удаляет истёкшие из sessions/pools/lastSession
RelayStore.ExpireAll()           // удаляет истёкшие RelayState
DestSessionStore.ExpireAll()     // удаляет истёкшие DestSession
```

## 7. Ключи DHT

### 7.1. По NodeID

```
key = SHA-256("node:" || node_id)
```

### 7.2. По псевдониму

```
key = SHA-256("alias:" || normalize(alias))
```

**Нормализация:**
- lowercase;
- trim;
- схлопывание внутренних пробелов;
- удаление непечатаемых символов.

## 8. TTL записей и re-publish

**TTL по умолчанию:** 180 секунд (диапазон по ТЗ 120–300).

**Re-publish:** раз в `TTL/2 = 90` секунд.

**Expire:** раз в 30 секунд.

**Anti-rollback:** если у узла есть запись с `sequence_number=N`,
приходит с `M < N` — отклоняется.

## 9. Bootstrap-схемы

### 9.1. Star

Все узлы стартуют с `BOOTSTRAP_PEERS=["node-01:9001"]`.
Seed — единая точка входа.

### 9.2. Ring

Узел `i` стартует с `BOOTSTRAP_PEERS=["node-(i-1):port"]`.
Каждый знает только предыдущего.

## 10. Обработка ошибок

| Код | Ситуация | Действие |
|-----|----------|----------|
| `ErrBadVersion` | version ≠ 1 | Закрыть соединение |
| `ErrBadType` | неизвестный type | Отправить ERROR, закрыть |
| `ErrPayloadTooBig` | length > 64 KiB | Закрыть до выделения буфера |
| `ErrShortHeader` | обрыв в заголовке | Закрыть |
| `ErrShortPayload` | обрыв в payload | Закрыть |
| `ErrRequestIDMismatch` | ответ с чужим request_id | Игнорировать, ждать таймаута |
| `ErrReplayDetected` | повтор nonce | Отклонить кадр |
| `ErrSessionMismatch` | чужой session_id | Отклонить кадр |
| `ErrHandshakeBadSignature` | невалидная подпись handshake | Прервать handshake |
| `ErrTooOld` (store) | `sequence_number` меньше существующего | Отклонить запись |
| `ErrValueNotFound` | `FIND_VALUE` не нашёл | Вернуть ошибку |
| `ErrTunnelClosed` | туннель закрыт | Прекратить отправку |
| `ErrTunnelNotActive` | туннель не в ACTIVE/DEGRADED | Отказать |
| `ErrBuildFailed` | не удалось построить туннель | Диагностика |
| `ErrNoCandidates` | нет подходящих ретрансляторов | Диагностика |
| `ErrTunnelExpired` | TTL истёк | Закрыть |

## 11. Коды ошибок (ERROR payload)

| Code | Значение |
|------|----------|
| `UNKNOWN_TYPE` | Неизвестный тип сообщения |
| `EXPECTED_HANDSHAKE` | Первый кадр не HANDSHAKE_HELLO |
| `BAD_REQUEST` | Некорректный payload |
| `BAD_RECORD` | Невалидная NodeRecord |
| `TUNNEL_UNSUPPORTED` | TunnelHandler не установлен |
| `INTERNAL` | Внутренняя ошибка |

## 12. Пример: полный RPC

**Клиент → Сервер (PING):**

```
1. TCP connect
2. HANDSHAKE_HELLO {
       body: { initiator_id, id_pub, eph_pub, random }
   }
3. HANDSHAKE_REPLY {
       body: { responder_id, id_pub, eph_pub, random, session_id, signature }
   }
4. HANDSHAKE_CONFIRM {
       body: { signature }
   }
5. [SecureConn создан]

6. PING {
       sender: { node_id, id_alg, id_pub, host, port },
       timestamp_ms: 1234567890
   }
   зашифрован AEAD с nonce = session_id[:4] || 0

7. PONG {
       responder: { ... },
       ping_timestamp_ms: 1234567890,
       responder_timestamp_ms: 1234567891
   }
   зашифрован AEAD с nonce = session_id[:4] || 0 (своё направление)

8. TCP close
```

## 13. Пример: туннельная доставка

```
Alice: TUNNEL_BUILD → R1
R1:    TUNNEL_BUILD_ACK → Alice, TUNNEL_BUILD → R2
R2:    TUNNEL_BUILD_ACK → Alice (через R1), TUNNEL_BUILD → R3
R3:    TUNNEL_BUILD_ACK → Alice (через R2, R1), TUNNEL_BUILD → Bob
Bob:   TUNNEL_BUILD_OK → R3 → R2 → R1 → Alice

Alice: Tunnel ACTIVE, E2ESession готова.

Alice: TUNNEL_DATA {tunnel_id, message_id, ciphertext} → R1
R1:    TUNNEL_DATA → R2 (не расшифровывает)
R2:    TUNNEL_DATA → R3
R3:    TUNNEL_DATA → Bob
Bob:   расшифровывает, проверяет nonce, TUNNEL_ACK → R3 → R2 → R1 → Alice
Alice: получает ACK, помечает message как доставленное.
```
