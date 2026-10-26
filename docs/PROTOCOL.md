# Протокол p2pnode

Документ описывает формат кадра, типы сообщений, payload'ы и алгоритмы
протокола узла p2pnode, включая туннели через ретрансляторы,
file-transfer поверх туннелей и multi-path lookup для защиты от
Eclipse/Sybil.

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

**Кадрирование:** `io.ReadFull` — гарантирует ровно `len(buf)` байт,
даже если `read()` возвращает данные **произвольными порциями**
(1 байт, разрыв внутри заголовка, несколько кадров в одном потоке).
**Тесты** (`internal/protocol/frame_test.go`):

- `TestReadFrame_RandomChunkBoundaries` — 2000 итераций, случайные
  границы.
- `TestReadFrame_ChunkedSizes` — фиксированные порции: 1, 5, 23, 24,
  25, 124, `MaxFramePayload/7`.
- `TestReadFrame_TwoFramesChunked` — два кадра в потоке, `maxChunk=1..1000`.
- `TestReadFrame_EOFWithData` — `read() → (n>0, io.EOF)`.

**Максимальный размер payload:** `MAX_FRAME_PAYLOAD = 65536`.

**Ответ:** содержит **тот же `request_id`**, что и запрос.

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

### Туннельный кадр с InnerEnvelope

С версии E7-4 payload внутри `TUNNEL_DATA.Ciphertext` содержит
**InnerEnvelope** — обёртку для мультиплексирования прикладных
сообщений:

```
TunnelDataPayload {
    tunnel_id
    message_id
    ciphertext = AEAD.Seal(InnerEnvelope)
}

InnerEnvelope = msgpack {
    inner_type: uint8
    inner_data: []byte
}
```

**InnerType:**

| Код | Имя | Назначение |
|-----|-----|------------|
| `0x01` | `APP_TEXT` | Обычный текст (`SendMessage`) |
| `0x10` | `FILE_HEADER` | Метаданные файла (E7-4) |
| `0x11` | `FILE_CHUNK` | Чанк файла (E7-4) |
| `0x12` | `FILE_ACK` | Подтверждение чанка (E7-4) |
| `0x13` | `FILE_DONE` | Финал передачи (E7-4) |

**Совместимость:** старые узлы, не знающие `InnerEnvelope`, получают
raw text как `APP_TEXT` (через fallback в `handleIncomingData`).

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

**Замечание:** отдельные `MsgType` для `FILE_*` **не используются** —
они идут как `InnerType` внутри `TUNNEL_DATA`.

## 3. Сериализация

Выбран **MessagePack** (`github.com/vmihailenco/msgpack/v5`).

**Обоснование:** компактнее JSON в 1.7×, не требует кодогенерации
(protobuf), языко-независим (gob). Измерения —
`scripts/compare_serialization.sh`.

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
по сети.

### 3.2. Payload'ы RPC

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
    Value  []byte   `msgpack:"value"`
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

**ERROR:**

```go
type ErrorPayload struct {
    Code    string `msgpack:"code"`
    Message string `msgpack:"message"`
}
```

### 3.3. Payload'ы туннелей

**TUNNEL_BUILD / ACK / OK / FAIL:**

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
```

**TUNNEL_DATA / ACK / CLOSE:**

```go
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

### 3.4. Payload'ы file-transfer

Идут **внутри** `TUNNEL_DATA.Ciphertext` как `InnerEnvelope` с типом
`FILE_HEADER` / `FILE_CHUNK` / `FILE_ACK` / `FILE_DONE`.

```go
type FileHeaderPayload struct {
    FileID      [16]byte `msgpack:"file_id"`
    Name        string   `msgpack:"name"`        
    TotalSize   uint64   `msgpack:"total_size"`
    TotalChunks uint32   `msgpack:"total_chunks"`
    SHA256      []byte   `msgpack:"sha256"`
    ChunkSize   uint32   `msgpack:"chunk_size"`
}

type FileChunkPayload struct {
    FileID      [16]byte `msgpack:"file_id"`
    ChunkIndex  uint32   `msgpack:"chunk_index"`
    ChunkSHA256 []byte   `msgpack:"chunk_sha256"`
    Data        []byte   `msgpack:"data"`
}

type FileAckPayload struct {
    FileID     [16]byte `msgpack:"file_id"`
    ChunkIndex uint32   `msgpack:"chunk_index"`
    OK         bool     `msgpack:"ok"`
    Error      string   `msgpack:"error,omitempty"`
}

type FileDonePayload struct {
    FileID  [16]byte `msgpack:"file_id"`
    SHA256  []byte   `msgpack:"sha256"`
    Success bool     `msgpack:"success"`
    Error   string   `msgpack:"error,omitempty"`
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

**Валидация (7 проверок):**
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

**Состояния кандидатов** (`states map[ID]candidateState`):
- `stateUnqueried` — ещё не опрашивался;
- `stateAlive` — успешно опрошен (или жив после проверки);
- `stateDead` — RPC failed + `RemoveIfDead == true` (удалён из таблицы).

**`inFlight map[ID]bool`** — кандидаты, которые сейчас опрашиваются
(исключает повторный выбор в той же итерации).

```
shortlist = table.Closest(target, K)
states    = {id: stateUnqueried}
inFlight  = {}
found_target = false

for iter in 0..IDBits:
    toQuery = []
    for ct in shortlist:
        if states[ct.ID] != stateUnqueried: continue
        if inFlight[ct.ID]: continue
        if ct.ID == self.ID: continue
        toQuery.append(ct)
        if len(toQuery) >= ALPHA: break
    if len(toQuery) == 0: break

    for ct in toQuery: inFlight[ct.ID] = true

    parallel:
        for ct in toQuery:
            resp = FindNodeRPC(ct, target)
            delete(inFlight, ct.ID)
            if resp.error:
                # PING перед удалением
                if RemoveIfDead(ctx, ct.ID):
                    states[ct.ID] = stateDead
                    shortlist.remove(ct)
                else:
                    states[ct.ID] = stateAlive
            else:
                states[ct.ID] = stateAlive
                for c in resp.contacts:
                    if c.ID == target: found_target = true
                    shortlist.append(c)
                    table.Add(c)

    shortlist = unique_sorted(shortlist, target, K)
    if found_target: break
    if all(shortlist) in stateAlive/stateUnqueried == queried: break

# Финальный result.Contacts — только не-stateDead.
return [ct for ct in shortlist if states[ct.ID] != stateDead][0:K]
```

**Ключевое:**
- **Не `break`** на первом `found` — ждём **все** ответы.
- При таймауте RPC — `RemoveIfDead(ctx, id)` (PING под `ctx`).
  Мёртвый → `stateDead` + удаление из `shortlist`; живой →
  `stateAlive`.
- **Финальный** `result.Contacts` — **только** `stateAlive` +
  `stateUnqueried`.
- `LookupIteration.Dead []ContactLog` — новое поле (для отчёта).

**Условие остановки:** target найден ИЛИ все `stateUnqueried`
опрошены.

### 5.2. Multi-path lookup

**Цель:** защита от Eclipse/Sybil-атаки через **несколько
независимых** lookup-путей + мажоритарное голосование.

**Входные данные:** `target_id`, `seeds[]` (не менее `paths`), `paths`.

```
results = []
for i in 0..paths:
    seed = seeds[i]
    tbl = pingFn(seed)             // routing-table с seed'ом
    r = LookupNode(tbl, target)
    winner = ExtractTargetAddr(r)  // addr target'а или ""
    results.append(winner)

votes = map[addr]int{}
for w in results:
    if w != "": votes[w]++

// Мажоритет: > paths/2
best = argmax(votes)
if votes[best] > paths/2:
    return {Winner: best, Agreement: votes[best], Disagree: false}
else:
    return {Disagree: true}        // все разные — отказ
```

**Гарантия:** если `> paths/2` seed'ов **честные**, winner —
**правильный адрес**. Атакующий должен скомпрометировать
**большинство** seeds.

**CLI:** `-lookup-paths N` (default 1 — обычный lookup).

### 5.3. Bootstrap

**Входные данные:** `seed_addr`.

```
seed = Ping(ctx, seed_addr)        // handshake + PING
table.Add(seed)
result = LookupNode(ctx, self.ID)  // обычный режим
// или для -skip-self-lookup:
nodes = FindNodeRPC(ctx, seed, self.ID)
for c in nodes: table.Add(c)

# Если после всех seed'ов table.Size() == 0:
return error("bootstrap failed: routing table is empty")
```

**`Bootstrap` возвращает error,** если таблица пустая — явная
сигнализация «не удалось присоединиться к сети».

### 5.4. STORE

```
key = NodeKeyForID(rec.NodeID)
value = rec.Encode()

candidates = LookupNode(ctx, key)
R = 3
minReplicas = R/2 + 1  // = 2

parallel:
    for c in candidates[0:R]:
        ok = StoreRPC(ctx, c, key, value, ttl)
        if ok: replicas++

store.Put(key, rec, now, ttl)

if replicas < minReplicas:
    return ErrNotEnoughReplicas
if candidates == 0:
    return ErrNotEnoughReplicas  // нет узлов для репликации
```

**Если alias задан:** повторить для `AliasKeyForName(alias)`.

**`Publish` возвращает `ErrNotEnoughReplicas`,** если `Replicas < 2`
или `candidates == 0`. `StoreResult` **возвращается всегда** (даже
при error) — клиент видит `Replicas`, `LocalStored`, `SelectedNodes`.

### 5.5. FIND_VALUE

**Цель:** вернуть запись с **максимальным `sequence_number`**
среди всех реплик — устаревшая реплика не перекрывает актуальную.

```
if store.Get(key) found: return value

shortlist = table.Closest(key, K)
queried = {}
bestRec = nil
bestSeq = 0

for iter in 0..IDBits:
    toQuery = первые ALPHA непрошенных
    if empty: break

    parallel:
        for c in toQuery:
            resp = FindValueRPC(c, key)
            # Собрать ВСЕ ответы (не break на первом found)
            if resp.found:
                rec = record.Decode(resp.value)
                if rec.Validate(now) fails: skip (Debug-лог)
                if bestRec == nil || rec.SequenceNumber > bestSeq:
                    bestRec = rec
                    bestSeq = rec.SequenceNumber
            else:
                for n in resp.nodes:
                    shortlist.append(n); table.Add(n)

    if bestRec != nil:
        store.Put(key, bestRec, now, TTL)  # anti-rollback
        return bestRec

    shortlist = unique_sorted(shortlist, key, K)

return ErrValueNotFound
```

**Ключевое:**
- **Не `break`** на первом `found` — ждём **все** ответы.
- `bestRec` — **max(sequence_number)** среди декодированных и
  провалидированных.
- Невалидные реплики — skip + Debug-лог.
- `store.Put(bestRec)` — anti-rollback (если в store уже свежее —
  `Put` вернёт `false`, store не испортится; клиенту отдаём `bestRec`).
- `events.jsonl`: `findvalue_found` с `replicas`, `best_seq`.

**Тест:** `TestFindValuePicksHighestSeq` — две реплики (`seq=5`,
`seq=10`), `FindValue` → `seq=10` (5/5 PASS, не flaky).

### 5.6. Handshake (AKE)

**Дедлайн:** `Handshake(ctx, addr)` — `deadline = ctx.Deadline()`,
`raw.SetDeadline(deadline)` **после** `Dial`. Покрывает Hello → Reply →
Confirm. **Никакого** `HandshakeTimeout=10s`.

**Инициатор:**

```
1. Dial(addr) → conn
2. if ctx has no deadline: error
3. conn.SetDeadline(ctx.Deadline())
4. hs = NewHandshakeState(id_pub, id_priv, isInitiator=true)
5. conn.WriteFrame(HANDSHAKE_HELLO, hs.BuildHello())
6. replyFrame = conn.ReadFrame()
7. if replyMsg.ResponderID != expected: fail
8. hs.ProcessReply(replyFrame, initID, respID)
9. conn.WriteFrame(HANDSHAKE_CONFIRM, hs.BuildConfirm(...))
10. keys = DeriveKeys(...)
11. secureConn = NewSecureConn(conn, keys...)
```

**Responder:** `Server.handleConn` ставит
`conn.SetReadDeadline(now + HandshakeTimeout)` на первый кадр; тот же
дедлайн — на reply+confirm; после handshake —
`secure.SetDeadline(time.Time{})`.

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

### 5.7. AEAD

- **Алгоритм:** ChaCha20-Poly1305.
- **Nonce:** 12 байт = `session_id[0:4] || counter(8, big-endian)`.
- **AAD:** 20 байт = `version || type || flags || request_id`.
- **Anti-replay:** монотонный counter. `counter < recvCounter` →
  `ErrReplayDetected`. Пропуск → ошибка.

## 6. Алгоритмы туннелей

### 6.1. Построение туннеля

**Инициатор** выбирает `FullPath = [init, relay1, ..., relayN, dest]`.

```
1. BuildCoordinator.Build(ctx, destID, path):
   - ctx, cancel = context.WithTimeout(ctx, bc.BuildTimeout)
   - deadline = ctx.Deadline()
   - conn = Dial(relay1)
   - conn.SetDeadline(deadline)  ← единый на write+read
2. Сгенерировать tunnel_id, e2e_eph_priv/pub, e2e_random.
3. Отправить TUNNEL_BUILD с HopIndex=1.
4. Ждать ACK'и от каждого ретранслятора + BUILD_OK от dest.
5. conn.SetDeadline(time.Time{})  ← снять (сессия долгоживущая)
```

**Ретранслятор** на `HopIndex = i`:

```
1. nextAddr = FullPath[i+1]
2. Dial(nextAddr) → nextConn
3. Отправить TUNNEL_BUILD_ACK в PrevConn.
4. Переслать TUNNEL_BUILD с HopIndex=i+1, PathSoFar += LocalID.
5. Запустить relayLoop (prev ↔ next).
```

**Dest:**

```
1. Сгенерировать e2e_eph_priv/pub, e2e_random.
2. Подписать transcript Ed25519.
3. Ответить TUNNEL_BUILD_OK.
4. Сохранить DestSession.
5. Запустить destDataLoop.
```

### 6.2. Передача данных

```
Alice:
  counter = e2e.NextSendNonce()
  ct = Seal(k_i→d, counter, plaintext, aad=tunnel_id)
  payload = nonce(8) || ct
  TUNNEL_DATA {tunnel_id, message_id, ciphertext=payload}

Ретрансляторы: пересылают как есть (не расшифровывают).

Bob:
  CheckRecvNonce(counter)
  plaintext = Open(k_d→i, nonce, ct, aad=tunnel_id)
  TUNNEL_ACK {tunnel_id, message_id}
```

### 6.3. TTL и expire

`TunnelBuildPayload.TTLSec` передаётся всем хопам. Каждый хоп:

```
ExpiresAt = now + TTLSec
```

Фоновые expire loop (раз в 30 сек): удаляют истёкшие сессии и
`RelayState` / `DestSession`.

## 7. Алгоритмы file-transfer

### 7.1. SendFile

```
1. SplitFile(path, chunkSize):
   - file_id ← random(16)
   - total_chunks ← ceil(size / chunkSize)
   - SHA256 ← hash(file)
   - канал chunks (idx, sha256, data)

2. Отправить InnerEnvelope{FILE_HEADER, header}
   через TunnelManager.SendBytesWithID.

3. Для каждого chunk:
   a. InnerEnvelope{FILE_CHUNK, chunk}
   b. SendBytesWithID (ждёт ACK на уровне туннеля)
   c. Retry до 3 раз при ошибке.

4. Логировать прогресс каждые 10%.
```

### 7.2. ReceiveFile

```
1. OnFileHeader(header):
   a. safeName = SanitizeFileName(header.Name)
   b. if error: Warn + return (передача отклонена)
   c. header.Name = safeName
   d. fileStore.Init(header)
2. OnFileChunk(chunk):
   - AddChunk(chunk) → true/false (дедупликация)
   - если все chunks собраны → Complete(file_id)
3. finalizeFile(transfer):
   a. safeName = SanitizeFileName(transfer.Header.Name)  ← повторно
   b. outPath = UniqueDestPath(outDir, safeName)  ← суффикс при коллизии
   c. Reassemble(header, chunks, outPath):
      - проверить SHA256 каждого chunk
      - собрать в порядке chunk_idx
      - проверить SHA256 всего файла
      - записать на диск
```

### 7.3. Дедупликация

`Store.AddChunk(c)`:
- если `(file_id, chunk_idx)` уже существует → `false`;
- иначе → `true` и добавляет.

**Best practice:** защита от повторов по сети (не от атакующего).

### 7.4. `SanitizeFileName` — безопасность имени

`internal/filetransfer/sanitize.go`:

**`SanitizeFileName(name) (string, error)`** — отклоняет:

- пустое имя;
- absolute paths (`/etc/passwd`, `C:\Windows\...`, `\\server\...`);
- path traversal (`../`, `..\`, `..`) — разделители пути запрещены;
- NUL-байты (`\x00`);
- control chars (< 0x20, 0x7f);
- имена `.` и `..`;
- длину > 255 байт;
- Windows reserved (`CON`, `PRN`, `AUX`, `NUL`, `COM1..9`, `LPT1..9`).

**Разрешены:** `payload.bin`, `my-file_v2.tar.gz`, `.hidden`,
`файл.txt`, `file with spaces.txt`.

**`UniqueDestPath(outDir, name) (string, error)`:**

- проверяет, что итоговый путь лежит **внутри `outDir`** (защита от
  обхода `SanitizeFileName`);
- если файл существует — добавляет суффикс `.1`, `.2`, … до 100;
- если > 100 коллизий — ошибка.

**Где вызывается:**
- `TunnelManager.OnFileHeader` — `SanitizeFileName` **до** `Init`
  (отклоняет невалидные имена на входе);
- `TunnelManager.finalizeFile` — повторная `SanitizeFileName` +
  `UniqueDestPath` (defense in depth).

## 8. Subnet diversity в routing

**Цель:** защита от Sybil-flooding — атакующий создаёт много
узлов в одной /24 и заполняет k-bucket.

**Правило:** в одном k-bucket — **не более `SubnetDiversityLimit`**
контактов из одной /24. `SubnetDiversityLimit` — **параметр конфига**
(`-subnet-diversity-limit`, env `SUBNET_DIVERSITY_LIMIT`, YAML
`subnet_diversity_limit`, default `2`, `0` = отключено, `>=K` =
фактически отключено).

**Логика `RoutingTable.Add`:**

```
if c.NodeID не существующий:
    if rt.subnetLimit > 0:
        newSubnet = subnetOfContact(c)
        sameSubnet = count(existing с newSubnet)
        if sameSubnet >= rt.subnetLimit:
            reject (subnet_diversity)
```

**`subnetOfContact(c)`:**

- **IP** → /24 (IPv4) или /64 (IPv6);
- **loopback** (`127.0.0.1`, `localhost`, `::1`) → "" (уникально);
- **`node-XX`** (Docker) → `zone-(XX-1)/3+1 /24` (эмуляция);
- **`sybil-XX`** → `zone-sybil/24` (все в одной зоне);
- иначе → "".

**Исключение:** обновление существующего контакта — не блокируется.

**Тесты:** `TestBucketFillsToK_SubnetDiversityDisabled` (0 → K),
`TestBucketFillsToK_SubnetLimitEqualsK` (K → K),
`TestBucketSubnetDiversityRejectsThird` (2 → 3-й отклонён),
`TestSubnetDiversityLimitAccessors`.

## 9. Ключи DHT

### 9.1. По NodeID

```
key = SHA-256("node:" || node_id)
```

### 9.2. По псевдониму

```
key = SHA-256("alias:" || normalize(alias))
```

**Нормализация:** lowercase, trim, схлопывание пробелов, удаление
непечатаемых.

## 10. TTL записей и re-publish

**TTL:** 180 секунд (диапазон по ТЗ 120–300).

**Re-publish:** раз в `TTL/2 = 90` секунд.

**Expire:** раз в 30 секунд.

**Refresh:** раз в `RefreshInterval` (default 60s, `-refresh-interval-ms`,
env `REFRESH_INTERVAL_MS`, YAML `refresh_interval_ms`) — `LookupNode(self)`
(стандартный Kademlia refresh).

**Anti-rollback:** если у узла есть запись с `sequence_number=N`,
приходит с `M < N` — отклоняется. `FindValue` — max(seq) среди реплик.

## 11. Bootstrap-схемы

### 11.1. Star

Все узлы стартуют с `BOOTSTRAP_PEERS=["node-01:9001"]`.

### 11.2. Ring

Узел `i` стартует с `BOOTSTRAP_PEERS=["node-(i-1):port"]`.

### 11.3. Tree

Узел `i` стартует с `BOOTSTRAP_PEERS=["node-(i/2):port"]`.

### 11.4. Multiseed

Первые 3 узла — seeds. Узлы 4+ стартуют с
`BOOTSTRAP_PEERS=["node-01:9001","node-02:9002","node-03:9003"]`.

### 11.5. Ошибка bootstrap

`Bootstrap` возвращает error, если после попыток присоединения к
seed'ам routing-таблица **пустая** — явная сигнализация «не удалось
присоединиться к сети».

## 12. Обработка ошибок

| Код | Ситуация | Действие |
|-----|----------|----------|
| `ErrBadVersion` | version ≠ 1 | Закрыть соединение |
| `ErrBadType` | неизвестный type | Отправить ERROR, закрыть |
| `ErrPayloadTooBig` | length > 64 KiB | Закрыть до выделения буфера |
| `ErrShortHeader` | обрыв в заголовке | Закрыть |
| `ErrShortPayload` | обрыв в payload | Закрыть |
| `ErrRequestIDMismatch` | ответ с чужим request_id | Игнорировать |
| `ErrReplayDetected` | повтор nonce | Отклонить кадр |
| `ErrSessionMismatch` | чужой session_id | Отклонить кадр |
| `ErrHandshakeBadSignature` | невалидная подпись | Прервать handshake |
| `ErrTooOld` (store) | `sequence_number` меньше | Отклонить запись |
| `ErrValueNotFound` | `FIND_VALUE` не нашёл | Вернуть ошибку |
| `ErrNotEnoughReplicas` | `Publish` < 2 реплик или нет кандидатов | Вернуть ошибку |
| `ErrTunnelClosed` | туннель закрыт | Прекратить отправку |
| `ErrTunnelNotActive` | туннель не ACTIVE | Отказать |
| `ErrBuildFailed` | не удалось построить | Диагностика |
| `ErrNoCandidates` | нет кандидатов | Диагностика |
| `ErrTunnelExpired` | TTL истёк | Закрыть |
| `ErrBootstrapEmpty` | Bootstrap: таблица пустая | Явная ошибка присоединения |
| `ErrBadFileName` | `SanitizeFileName` отклоняет | Отклонить передачу файла |
| `ErrPathEscapesOutDir` | `UniqueDestPath` — путь вне `outDir` | Отклонить передачу |

## 13. Коды ошибок (ERROR payload)

| Code | Значение |
|------|----------|
| `UNKNOWN_TYPE` | Неизвестный тип сообщения |
| `EXPECTED_HANDSHAKE` | Первый кадр не HANDSHAKE_HELLO |
| `BAD_REQUEST` | Некорректный payload |
| `BAD_RECORD` | Невалидная NodeRecord |
| `TUNNEL_UNSUPPORTED` | TunnelHandler не установлен |
| `INTERNAL` | Внутренняя ошибка |

## 14. Sybil-режим

**CLI:** `-sybil-mode -sybil-victim <hex64>`.

**Поведение:**

1. **Обычно** — узел ведёт себя как честный.
2. **При `FIND_NODE(target)`**, если `target == sybil_victim` —
   возвращает **только свой Contact** (вместо реальных ближайших).

**Зачем:** атакующий с **близким** к victim'у NodeID'ом (совпадает
префикс) попадает в **тот же k-bucket**, что victim. Клиент при
lookup'е видит Sybil'а **раньше** victim'а и **верит** ему.

**Модель атаки:**

- 5 Sybil'ов с **префиксом** victim'а (брутфорс через
  `gen_sybil_identity.sh`).
- Sybil'ы **подключаются** к seed'у, попадают в routing.
- `single` lookup — **клиент** попадает на Sybil'а → **0%** успеха.
- `multipath` lookup — **голосование** игнорирует Sybil'ов → **100%** успеха.

## 15. Пример: полный RPC (PING)

```
1. TCP connect
2. HANDSHAKE_HELLO { id, id_pub, eph_pub, rnd }
3. HANDSHAKE_REPLY { id, id_pub, eph_pub, rnd, session_id, signature }
4. HANDSHAKE_CONFIRM { signature }
5. [SecureConn создан; secure.SetDeadline(deadline)]
6. PING { sender, timestamp_ms } — AEAD
7. PONG { responder, ping_ts, resp_ts } — AEAD
8. TCP close
```

## 16. Пример: file-transfer

```
Alice → R1 → R2 → Bob

Alice:
  BuildCoordinator.Build(ctx, destID, path) → Tunnel ACTIVE

  InnerEnvelope{FILE_HEADER, header}  → TUNNEL_DATA
  InnerEnvelope{FILE_CHUNK, c0}       → TUNNEL_DATA
  InnerEnvelope{FILE_CHUNK, c1}       → TUNNEL_DATA
  ...
  InnerEnvelope{FILE_CHUNK, c31}      → TUNNEL_DATA

Bob (dest):
  OnFileHeader(header):
    safeName = SanitizeFileName(header.Name)  # reject ../../etc/passwd
    Store.Init(header)
  OnFileChunk(c0)      → Store.AddChunk
  OnFileChunk(c1)      → Store.AddChunk
  ...
  OnFileChunk(c31)     → Complete → Reassemble
    outPath = UniqueDestPath(outDir, safeName)  # payload.1.bin при коллизии
    file saved, sha256 verified
```
