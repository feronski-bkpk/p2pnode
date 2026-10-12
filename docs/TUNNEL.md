# Туннели через ретрансляторы

Документ описывает туннельную подсистему `p2pnode`: модель, протокол,
криптографию, состояния, восстановление, пул, file-transfer поверх
туннелей и способ проверки.

## 1. Что это и зачем

Туннель — цепочка ретрансляторов, через которую передаётся
прикладной payload от инициатора к получателю:

```
Alice ──▶ R1 ──▶ R2 ──▶ R3 ──▶ Bob
```

- **Alice** — инициатор (initiator), строит туннель и отправляет.
- **R1, R2, R3** — ретрансляторы (relays), пересылают кадры.
- **Bob** — конечный получатель (dest), расшифровывает payload.

**Ключевое свойство:** ретрансляторы видят **только ciphertext**
и служебные поля (`tunnel_id`, `message_id`), но **не могут прочитать
прикладные данные**. Это end-to-end шифрование поверх hop-by-hop
передачи.

## 2. Схема построения

### 2.1. Маршрут и FullPath

Инициатор сам выбирает **всю цепочку** ретрансляторов. Каждый
ретранслятор знает **следующий адрес** из `FullPath`.

```
FullPath = [initiator_addr, relay1_addr, ..., relayN_addr, dest_addr]
индексы:    0              1                  N              N+1
```

Поле **`HopIndex`** = **индекс текущего получателя**:

- Alice отправляет R1 с `HopIndex = 1`.
- R1 пересылает R2 с `HopIndex = 2`.
- ...
- R_N пересылает dest'у с `HopIndex = N+1`.
- Dest проверяет: `HopIndex == len(FullPath) - 1`.

### 2.2. Диаграмма построения

```
Alice                  R1                  R2                  R3                  Bob
  │                     │                   │                   │                   │
  │──TUNNEL_BUILD──────▶│                   │                   │                   │
  │◀──TUNNEL_BUILD_ACK──│                   │                   │                   │
  │                     │──TUNNEL_BUILD────▶│                   │                   │
  │◀──TUNNEL_BUILD_ACK──────────────────────│                   │                   │
  │                     │                   │──TUNNEL_BUILD────▶│                   │
  │◀──TUNNEL_BUILD_ACK──────────────────────────────────────────│                   │
  │                     │                   │                   │──TUNNEL_BUILD────▶│
  │◀──TUNNEL_BUILD_OK───────────────────────────────────────────│◀──TUNNEL_BUILD_OK─│
  │                     │                   │                   │                   │
  │  DeriveKeys →       │ RelayState        │ RelayState        │ RelayState        │  DestSession
  │  E2ESession         │ {prev,next,ttl}   │ {prev,next,ttl}   │ {prev,next,ttl}   │  {e2e,ttl}
```

### 2.3. Состояния туннеля

```
                  build OK
BUILDING ─────────────────────▶ ACTIVE
   │                              │
   │ build fail                   │ relay unreachable / ACK timeout
   ▼                              ▼
  DEAD                        DEGRADED
                                 │
                                 │ rebuild OK
                                 ▼
                              ACTIVE

ACTIVE ──── close ────▶ CLOSING ────▶ DEAD
```

- **BUILDING** — TUNNEL_BUILD отправлен, ждём ACK'и и BUILD_OK.
- **ACTIVE** — можно передавать данные.
- **DEGRADED** — сессия деградировала, идёт rebuild.
- **CLOSING** — TUNNEL_CLOSE отправлен.
- **DEAD** — терминальное состояние.

## 3. Передача данных

```
Alice                  R1                  R2                  R3                  Bob
  │                     │                   │                   │                   │
  │──TUNNEL_DATA───────▶│──TUNNEL_DATA─────▶│──TUNNEL_DATA─────▶│──TUNNEL_DATA─────▶│
  │  {message_id,       │  (не расшифр.)    │  (не расшифр.)    │  (не расшифр.)    │
  │   e2e_ciphertext}   │                   │                   │                   │
  │◀──TUNNEL_ACK────────│◀──TUNNEL_ACK──────│◀──TUNNEL_ACK──────│◀──TUNNEL_ACK──────│
```

**Формат `e2e_ciphertext`:** `nonce(8) || AEAD.Seal(k, nonce, plaintext, aad=tunnel_id)`.

**AAD для AEAD:** `tunnel_id` — ciphertext нельзя перенести в другой
туннель.

## 4. Два уровня шифрования

### 4.1. Что видит каждый узел

| Узел | Видит | Не видит |
|------|-------|----------|
| Alice | plaintext, весь FullPath | — |
| R1 | `tunnel_id`, `message_id`, `next_hop`, ciphertext | plaintext, ключи E2E |
| R2 | то же | то же |
| R3 | то же | то же |
| Bob | plaintext, весь FullPath | — |

### 4.2. E2E vs hop-by-hop

**Hop-by-hop** — шифрование между **соседями**. В нашем протоколе
**используется только для RPC**, не для туннеля.

**End-to-end** — шифрование между **Alice и Bob**. **Всегда.**
E2E-сессия создаётся **в момент построения** через
`TUNNEL_BUILD`/`TUNNEL_BUILD_OK`.

## 5. E2E-сессия

### 5.1. Обмен ключами

```
Alice:                                       Bob:
  eph_i_priv, eph_i_pub ← X25519               eph_d_priv, eph_d_pub ← X25519
  random_i ← 32 bytes                          random_d ← 32 bytes

transcript = msgpack{protocol, tunnel_id, init_id, dest_id,
                     init_eph_pub, dest_eph_pub, init_random, dest_random}

dh  = X25519(eph_i_priv, eph_d_pub) = X25519(eph_d_priv, eph_i_pub)
salt = SHA-256(transcript)
k_i→d = HKDF-Expand(HKDF-Extract(salt, dh), "initiator→dest", 32)
k_d→i = HKDF-Expand(HKDF-Extract(salt, dh), "dest→initiator", 32)
```

### 5.2. Подпись dest'а

Bob подписывает transcript своим долговременным Ed25519-ключом:

```
sig = Ed25519.Sign(dest_priv, transcript)
```

Alice проверяет:

```
pubkey_dest = (найден через DHT)
ok = Ed25519.Verify(pubkey_dest, transcript, sig)
```

### 5.3. AEAD

- **Алгоритм:** ChaCha20-Poly1305.
- **Nonce:** 12 байт = `0x00000000 || counter(8, big-endian)`.
- **AAD:** `tunnel_id` (16 байт).
- **Формат ciphertext:** `nonce(8) || AEAD.Seal(...)`.

**Anti-replay:** монотонный counter.

## 6. File-transfer поверх туннеля

### 6.1. Общая схема

Файл разбивается на **чанки** (default 32 КиБ). Каждый чанк идёт
**внутри** `TUNNEL_DATA` как `InnerEnvelope` с типом `FILE_CHUNK`.
E2E-шифрование **сохраняется** — ретрансляторы видят только ciphertext.

```
Alice → [InnerEnvelope{FILE_HEADER, header}] → TUNNEL_DATA → R1 → R2 → Bob
Alice → [InnerEnvelope{FILE_CHUNK, c0}]      → TUNNEL_DATA → R1 → R2 → Bob
Alice → [InnerEnvelope{FILE_CHUNK, c1}]      → TUNNEL_DATA → R1 → R2 → Bob
...
Alice → [InnerEnvelope{FILE_CHUNK, cN}]      → TUNNEL_DATA → R1 → R2 → Bob
```

### 6.2. Последовательность

1. **Alice** вызывает `SplitFile(path, chunkSize)`:
   - генерирует `FileID`;
   - вычисляет `TotalChunks`, общий `SHA256`;
   - возвращает канал чанков.
2. Alice отправляет `FILE_HEADER` через `Session.SendBytesWithID`.
3. Для каждого чанка:
   - сериализует в `InnerEnvelope{FILE_CHUNK}`;
   - отправляет через `SendBytesWithID`;
   - ждёт `TUNNEL_ACK` (на уровне туннеля);
   - при таймауте — retry (до 3 раз).
4. **Bob** получает `TUNNEL_DATA`:
   - `session.handleIncomingData` **или** `HandlerConfig.dispatchInner`;
   - по `InnerType` вызывает `OnFileHeader` / `OnFileChunk`.
5. **Bob** хранит чанки в `filetransfer.Store` с TTL.
6. При получении **всех** чанков:
   - `Store.Complete(fileID)` → transfer;
   - `Reassemble` собирает файл;
   - проверка SHA256 каждого чанка и всего файла;
   - запись на диск.
7. **Bob** может отправить `FILE_DONE` (опционально).

### 6.3. Дедупликация

`Store.AddChunk(c)` возвращает:
- `true` — чанк новый, добавлен;
- `false` — `(file_id, chunk_idx)` уже существует.

**Best practice:** защита от повторов (например, если ACK
потерялся и Alice отправил чанк повторно).

### 6.4. Формат

См. `docs/PROTOCOL.md` раздел 3.4.

### 6.5. CLI

| Флаг | Env | Default | Назначение |
|------|-----|---------|------------|
| `-send-file` | `SEND_FILE` | — | Путь к файлу для отправки |
| `-recv-file-dir` | `RECV_FILE_DIR` | — | Каталог для приёма |
| `-file-chunk-size` | `FILE_CHUNK_SIZE` | `32768` | Размер чанка |
| `-file-max-size` | `FILE_MAX_SIZE` | `104857600` | Макс. размер (100 МиБ) |

### 6.6. Демонстрация

```bash
# N=5 узлов, max-hops=2, файл 1 МБ
./scripts/demo_file_transfer.sh 5 1 2

# N=7 узлов, max-hops=3, файл 10 МБ
./scripts/demo_file_transfer.sh 7 10 3
```

**Артефакт:** `visualization/file-transfer/` (`src.bin`, `send.log`,
`check-dht.log`, `summary.json`).

**Что проверяется:**

- ✅ разбиение на чанки;
- ✅ ACK на каждый чанк;
- ✅ SHA-256 совпадает (побайтовое сравнение);
- ✅ дедупликация (`TestStore_Dedup`);
- ✅ **E2E сохраняется** (`relay logs` не содержат plaintext).

## 7. Алгоритм BuildPool с exclude

`TunnelManager.BuildPool(destID)` строит `PoolSize` (default 3)
туннелей к одному dest'у:

```
exclude = []
for i in 0..PoolSize:
    sess = buildInternal(destID, exclude)
    if err:
        sess = buildInternal(destID, nil)   // fallback без exclude
    sessions.append(sess)
    for h in sess.Tunnel.Path:
        if h.Type == HopRelay:
            exclude.append(h.NodeID)

pools[destID] = sessions
lastSession[destID] = sessions[0]
```

## 8. Выбор сессии и failover

```go
func (m *TunnelManager) pickSession(destID, attempt) (*Session, error) {
    pool := m.pools[destID]
    if len(pool) > 0 {
        alive := [s for s in pool if s.CanSend() and not s.IsClosed()]
        if attempt < len(alive): return alive[attempt]
        return m.Build(destID)
    }
    if attempt == 0 and lastSession[destID].CanSend():
        return lastSession[destID]
    return m.Build(destID)
}
```

**Сценарий failover:**

1. `BuildPool` → `pools = [s1, s2, s3]`, `builds_ok = 3`.
2. `SendMessage #1`: `pickSession(0)` = s1 → ACK.
3. **Kill relay1 s1** → `readLoop` EOF → `removeSession(s1)` →
   `pools = [s2, s3]`.
4. `SendMessage #2`: `pickSession(0)` = s2 → **без нового Build** → ACK.

## 9. Восстановление

Если пул не используется (`PoolSize=1`):

```
SendMessage #1: pickSession(0) = lastSession → ACK
kill relay1
SendMessage #2: lastSession (ещё ACTIVE) → ACK timeout
             → sess.Close() → removeSession
             → attempt=1: pickSession(1) = Build → новый туннель
             → SendMessageWithID(msgID, ...) → ACK
             → rebuildsOK++
```

## 10. Профилирование ретрансляторов

`ProfileStore` ведёт для каждого кандидата:

```go
type RelayProfile struct {
    NodeID              [32]byte
    Subnet              string        
    Successes           uint64
    Failures            uint64
    SuccessEMA          float64
    LatencyEMA          time.Duration
    LastSeen            time.Time
    ConsecutiveFailures uint8
}
```

**EMA:** α=0.2, нейтральный старт `SuccessEMA=0.5`, `LatencyEMA=0`.

**Score:** `SuccessEMA - LatencyEMA/1s - 0.1*ConsecutiveFailures`.

**Режимы выбора:**

- **`ranked`** — сортировка по score + Fisher-Yates среди top-2×MaxHops.
- **`random`** — случайный порядок.
- **`subnet`** — ranked + subnet-diversity (не более 1 кандидата из
  одной /24).

**CLI:** `-relay-select-mode ranked|random|subnet`.

**Логирование:**

- `route: candidate ranked rank=R node_id=... subnet=... success_ema=...`
- `route: candidate accepted hop_index=H ...`
- `route: candidate rejected reason=...`
- `route: subnet-diversity reject node_id=... subnet=...`

**Демонстрация:**

```bash
./scripts/demo_relay_selection.sh 9 10
```

**Артефакт:** `visualization/relay-selection/`.

## 11. TTL и expire

**TTL туннеля:** `Config.TunnelTTLSec` (default **300 секунд**).

**Поля:**

- `TunnelBuildPayload.TTLSec` — передаётся всем хопам.
- `Tunnel.expiresAt`, `RelayState.ExpiresAt`, `DestSession.ExpiresAt`.

**Фоновые expire loop:**

- `TunnelManager.StartExpireLoop(ctx)` — раз в **30 секунд**.
- `StartStoreExpireLoop(ctx, relayStore, destStore, log)` — раз в
  **30 секунд**.

## 12. Отдельный транспорт для туннелей

**Проблема:** RPC-транспорт использует `ReadTimeout=5s`. Туннельные
соединения **живут долго** (до TTL).

**Решение:** `Node.trTunnel` — отдельный `tcp.Transport` с
`ReadTimeout=0`.

**Динамическое переключение:** `rpc.Server.handleConn` при получении
туннельного кадра делает `SetReadTimeout(0)` через type assertion.

## 13. CLI-параметры

| Флаг | Env | Default | Назначение |
|------|-----|---------|------------|
| `-max-hops` | `MAX_HOPS` | `3` | Макс. ретрансляторов |
| `-tunnel-pool-size` | `TUNNEL_POOL_SIZE` | `3` | Размер пула |
| `-tunnel-ack-timeout-ms` | `TUNNEL_ACK_TIMEOUT_MS` | `5000` | Таймаут ACK |
| `-tunnel-ttl-sec` | `TUNNEL_TTL_SEC` | `300` | TTL туннеля |
| `-no-serve` | `NO_SERVE` | `false` | Клиент без listener'а |
| `-send-to` | `SEND_TO` | — | hex NodeID получателя |
| `-send-text` | `SEND_TEXT` | — | Текст сообщения |
| `-send-file` | `SEND_FILE` | — | Файл для отправки (E7-4) |
| `-recv-file-dir` | `RECV_FILE_DIR` | — | Каталог приёма (E7-4) |
| `-file-chunk-size` | `FILE_CHUNK_SIZE` | `32768` | Размер чанка (E7-4) |
| `-relay-select-mode` | `RELAY_SELECT_MODE` | `ranked` | Режим выбора (E7-7) |
| `-exit-after-ms` | `EXIT_AFTER_MS` | `5000` | Force-exit |

## 14. Демонстрационные сценарии

### 14.1. Базовая доставка — `demo_tunnel.sh`

```bash
./scripts/demo_tunnel.sh
```

**Проверки:**

- `OK: 3 relays participated`
- `tunnel: build complete ... acks_received=3`
- `OK: plaintext not found in relay logs (E5-4)`
- `OK: node-2 received tunnel message`

### 14.2. Восстановление — `demo_tunnel_recovery.sh`

```bash
./scripts/demo_tunnel_recovery.sh
```

**Проверки:**

- `send: acked × 2`
- `tunnel stats iteration=2 builds_ok=2 messages=2`

### 14.3. pcap — `demo_tunnel_pcap.sh`

```bash
sudo setcap cap_net_raw,cap_net_admin+eip $(which tcpdump)
./scripts/demo_tunnel_pcap.sh
```

**Проверки:**

- `OK: plaintext not found in pcap (E5-4 satisfied)`
- `OK: plaintext not found in relay logs`

### 14.4. Пул — `demo_tunnel_pool.sh`

```bash
./scripts/demo_tunnel_pool.sh
```

**Проверки:**

- `send: pool built size=3`
- `iteration=1 sessions=3 pools=1 builds_ok=3`
- `iteration=2 sessions=2 pools=1 builds_ok=3` (без нового Build)

### 14.5. File-transfer — `demo_file_transfer.sh`

```bash
./scripts/demo_file_transfer.sh 5 1 2
```

**Проверки:**

- `OK: node-03 найден в DHT`
- `filetransfer: done ... sha256=...`
- `OK: размер и SHA-256 совпадают — E7-4 satisfied`

## 15. Интеграционные тесты

`internal/tunnel/integration_test.go`:

- **`TestTunnelIntegration_EndToEnd`** — 5 in-process узлов, полный
  цикл Build → ACK × 3 → DATA → ACK.
- **`TestTunnelIntegration_NoPlaintextOnRelay`** — R2 не имеет
  E2E-ключей.
- **`TestTunnelIntegration_TTLExpires`** — TTL истекает.
- **`TestTunnelIntegration_LoopDetection`** — петля отклоняется.

**Unit-тесты:** `tunnel_test.go`, `manager_test.go`, `session_test.go`,
`message_test.go`, `profile_test.go`, `route_test.go`.
