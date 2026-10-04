# Туннели через ретрансляторы

Документ описывает туннельную подсистему `p2pnode`: модель, протокол, криптографию, состояния, восстановление и способ проверки.

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
и служебные поля (`tunnel_id`, `message_id`), но **не могут прочитать прикладные данные**. Это end-to-end шифрование поверх
hop-by-hop передачи.

## 2. Схема построения

### 2.1. Маршрут и FullPath

Инициатор сам выбирает **всю цепочку** ретрансляторов. Каждый ретранслятор знает **следующий адрес** из `FullPath`.

```
FullPath = [initiator_addr, relay1_addr, ..., relayN_addr, dest_addr]
индексы:    0              1                  N              N+1
```

Поле **`HopIndex`** в payload = **индекс текущего получателя**:

- Alice отправляет R1 с `HopIndex = 1` (R1 — index 1).
- R1 пересылает R2 с `HopIndex = 2`.
- ...
- R_N пересылает dest'у с `HopIndex = N+1`.
- Dest проверяет: `HopIndex == len(FullPath) - 1` → dest.

### 2.2. Диаграмма построения

```
Alice                  R1                  R2                  R3                  Bob
  │                     │                   │                   │                   │
  │──TUNNEL_BUILD──────▶│                   │                   │                   │
  │                     │                   │                   │                   │
  │◀──TUNNEL_BUILD_ACK──│                   │                   │                   │  E5-2: подтверждение
  │   {hop_index=1,     │──TUNNEL_BUILD────▶│                   │                   │
  │    hop_id=R1}       │                   │                   │                   │
  │                     │                   │                   │                   │
  │◀──TUNNEL_BUILD_ACK──────────────────────│                   │                   │  E5-2
  │   {hop_index=2,     │                   │──TUNNEL_BUILD────▶│                   │
  │    hop_id=R2}       │                   │                   │                   │
  │                     │                   │                   │                   │
  │◀──TUNNEL_BUILD_ACK──────────────────────────────────────────│                   │  E5-2
  │   {hop_index=3,     │                   │                   │──TUNNEL_BUILD────▶│
  │    hop_id=R3}       │                   │                   │                   │
  │                     │                   │                   │                   │
  │◀──TUNNEL_BUILD_OK───────────────────────────────────────────│◀──TUNNEL_BUILD_OK─│  BUILD_OK + подпись
  │   {dest_e2e_pubkey, │                   │                   │                   │
  │    dest_random,     │                   │                   │                   │
  │    signature}       │                   │                   │                   │
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
- **DEGRADED** — сессия деградировала (ошибка отправки, read EOF),
  идёт rebuild.
- **CLOSING** — TUNNEL_CLOSE отправлен.
- **DEAD** — терминальное состояние, ресурсы освобождены.

## 3. Передача данных

```
Alice                  R1                  R2                  R3                  Bob
  │                     │                   │                   │                   │
  │──TUNNEL_DATA───────▶│──TUNNEL_DATA─────▶│──TUNNEL_DATA─────▶│──TUNNEL_DATA─────▶│
  │  {message_id,       │  (не расшифр.)    │  (не расшифр.)    │  (не расшифр.)    │
  │   e2e_ciphertext}   │                   │                   │                   │
  │                     │                   │                   │                   │
  │◀──TUNNEL_ACK────────│◀──TUNNEL_ACK──────│◀──TUNNEL_ACK──────│◀──TUNNEL_ACK──────│
  │  {message_id}       │                   │                   │                   │
```

**Формат `e2e_ciphertext`:** `nonce(8) || AEAD.Seal(k, nonce, plaintext, aad=tunnel_id)`.

**AAD для AEAD:** `tunnel_id` — гарантирует, что ciphertext нельзя
перенести в другой туннель.

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

**Hop-by-hop** — шифрование между **соседями** (Alice↔R1, R1↔R2, ...).
В нашем протоколе **используется только для RPC**, не для туннеля.
**Для туннелей hop-by-hop AKE НЕ применяется** — туннельные кадры
идут **без handshake**, потому что:

- Кадры `TUNNEL_*` не содержат чувствительных метаданных
  (`next_hop` — публичный адрес, `tunnel_id` — случайный, `ciphertext` — E2E).
- Полноценный AKE на каждом хопе даёт **отрицательную** выгоду:
  задержка × 3, сложность × 5.

**End-to-end** — шифрование между **Alice и Bob**. **Всегда.**
E2E-сессия создаётся **в момент построения** через `TUNNEL_BUILD`/
`TUNNEL_BUILD_OK` и защищает прикладной payload.

## 5. E2E-сессия

### 5.1. Обмен ключами

```
Alice:                                       Bob:
  eph_i_priv, eph_i_pub ← X25519               eph_d_priv, eph_d_pub ← X25519
  random_i ← 32 bytes                          random_d ← 32 bytes

transcript = msgpack{
    protocol:  "p2pnode-tunnel-e2e-v1",
    tunnel_id: ID,
    init_id:   [32]byte,
    dest_id:   [32]byte,
    init_eph_pub: []byte,
    dest_eph_pub: []byte,
    init_random:  []byte,
    dest_random:  []byte,
}

dh  = X25519(eph_i_priv, eph_d_pub)
    = X25519(eph_d_priv, eph_i_pub)

salt = SHA-256(transcript)
k_i→d = HKDF-Expand(HKDF-Extract(salt, dh), "initiator→dest", 32)
k_d→i = HKDF-Expand(HKDF-Extract(salt, dh), "dest→initiator", 32)
```

### 5.2. Подпись dest'а

Bob подписывает transcript своим **долговременным Ed25519-ключом**:

```
sig = Ed25519.Sign(dest_priv, transcript)
```

Alice проверяет:

```
pubkey_dest = (найден через DHT по NodeKeyForID(dest_id))
ok = Ed25519.Verify(pubkey_dest, transcript, sig)
```

### 5.3. AEAD

- **Алгоритм:** ChaCha20-Poly1305.
- **Nonce:** 12 байт = `0x00000000 || counter(8, big-endian)`.
- **AAD:** `tunnel_id` (16 байт).
- **Формат ciphertext:** `nonce(8) || AEAD.Seal(...)`.

**Anti-replay:** монотонный `counter`, инкрементируется на каждом
`TUNNEL_DATA`. Повтор nonce → `ErrReplay`. Пропуск → `ErrNonceGap`.

## 6. Алгоритм BuildPool с exclude (E5-8)

`TunnelManager.BuildPool(destID)` строит `PoolSize` (по умолчанию 3)
туннелей к одному dest'у:

```
exclude = []
for i in 0..PoolSize:
    sess = buildInternal(destID, exclude)
    if err:
        # Fallback: без exclude (пересечение маршрутов).
        sess = buildInternal(destID, nil)
    sessions.append(sess)

    for h in sess.Tunnel.Path:
        if h.Type == HopRelay:
            exclude.append(h.NodeID)

pools[destID] = sessions
lastSession[destID] = sessions[0]
```

## 7. Выбор сессии и failover (E5-8)

```go
func (m *TunnelManager) pickSession(destID, attempt) (*Session, error) {
    pool := m.pools[destID]

    if len(pool) > 0 {
        alive := [s for s in pool if s.CanSend() and not s.IsClosed()]
        if attempt < len(alive):
            return alive[attempt]
        return m.Build(destID)
    }

    if attempt == 0 and lastSession[destID].CanSend():
        return lastSession[destID]
    return m.Build(destID)
}
```

**Сценарий failover:**

1. `BuildPool` → `pools = [s1, s2, s3]`, `builds_ok = 3`.
2. `SendMessage` #1: `pickSession(0)` = s1 → ACK.
3. **Kill relay1 s1** → `Session.readLoop` EOF → `onClosed` →
   `removeSession(s1)` → `pools = [s2, s3]`.
4. `SendMessage` #2: `pickSession(0)` = s2 → **без нового Build** → ACK.
5. `SendMessage` #3: `pickSession(0)` = s2 → ACK.

**Ключевое:** `builds_ok` **не растёт** после kill — это
**переключение внутри пула**.

## 8. Восстановление

Если пул не используется (`PoolSize=1`):

```
SendMessage #1: pickSession(0) = lastSession → ACK
kill relay1
SendMessage #2: pickSession(0) = lastSession (ещё ACTIVE) → ACK timeout
             → sess.Close() → removeSession
             → attempt=1: pickSession(1) = Build(destID) → новый туннель
             → SendMessageWithID(msgID, ...) → ACK
             → rebuildsOK++
```

**Альтернативный сценарий (быстрый EOF):**

- kill → `readLoop` EOF → `onClosed` → `removeSession` → `pools=[]`.
- SendMessage #2: `pickSession(0)` → pool пуст → `Build` → новый туннель.
- `builds_ok=2`, `rebuilds_ok=0`, `messages=2`.

## 9. Профилирование ретрансляторов

`ProfileStore` ведёт для каждого кандидата:

```go
type RelayProfile struct {
    NodeID              [32]byte
    Successes           uint64
    Failures            uint64
    SuccessEMA          float64       // 0..1
    LatencyEMA          time.Duration
    LastSeen            time.Time
    ConsecutiveFailures uint8
}
```

**EMA:** α=0.2, нейтральный старт `SuccessEMA=0.5`, `LatencyEMA=0`.

**Score:** `SuccessEMA - LatencyEMA/1s - 0.1*ConsecutiveFailures`.

**Rank** сортирует по убыванию score. `RouteBuilder.Build` берёт
**топ-2×MaxHops** кандидатов, перемешивает (Fisher-Yates),
пингует каждого и выбирает **первых `MaxHops` живых**.

**Логирование:**

- `route: candidates collected total=N excluded=M max_hops=K`
- `route: candidate ranked rank=R node_id=... success_ema=... latency_ema=...`
- `route: candidate rejected node_id=... reason=ping_failed addr=...`
- `route: candidate accepted hop_index=H node_id=... rtt=...`
- `route: built dest=... relays=N path=[...]`

**Reason'ы отклонения:**

- `already_used` — уже в `used` (self, dest, exclude).
- `no_addr` — нет адреса в routing-таблице.
- `ping_failed` — PING не прошёл.
- `enough_relays` — уже набрано `MaxHops`, но кандидат всё равно пингуется для профиля.

## 10. TTL и expire

**TTL туннеля:** `Config.TunnelTTLSec` (по умолчанию **300 секунд**).

**Поля:**

- `TunnelBuildPayload.TTLSec` — передаётся всем хопам.
- `Tunnel.expiresAt` — `now + TTL` (у инициатора).
- `RelayState.ExpiresAt` — `now + TTL` (у ретранслятора).
- `DestSession.ExpiresAt` — `now + TTL` (у dest'а).

**Фоновые expire loop:**

- `TunnelManager.StartExpireLoop(ctx)` — раз в **30 секунд** удаляет
  истёкшие сессии из `sessions`/`pools`/`lastSession`.
- `StartStoreExpireLoop(ctx, relayStore, destStore, log)` — раз в
  **30 секунд** очищает истёкшие `RelayState` и `DestSession`.

**Логирование:**

```
tunnel: expired tunnel_id=... state=...
tunnel: expired relay states removed=2 remaining=0
tunnel: expired dest sessions removed=1 remaining=3
```

## 11. Отдельный транспорт для туннелей

**Проблема:** RPC-транспорт использует `ReadTimeout=5s`. Туннельные
соединения **живут долго** (до TTL) и **могут не иметь данных**
между Build и первым DATA.

**Решение:** `Node.trTunnel` — отдельный `tcp.Transport` с
`ReadTimeout=0`. **Туннельные соединения не имеют idle timeout.**

**Динамическое переключение (на стороне ретранслятора):**
`rpc.Server.handleConn` при получении туннельного кадра делает
**type assertion** `conn.(interface{ SetReadTimeout(time.Duration) })`
и вызывает `SetReadTimeout(0)`.

**Почему это критично:** без этого `relayLoop` ретранслятора
получает `transport.ErrTimeout` через 5 секунд простоя и закрывает
`rs.Close()` → `nextConn.Close()` → **туннель рушится**.

## 12. CLI-параметры

| Флаг | Env | Default | Назначение |
|------|-----|---------|------------|
| `-max-hops` | `MAX_HOPS` | `3` | Макс. ретрансляторов |
| `-tunnel-pool-size` | `TUNNEL_POOL_SIZE` | `3` | Размер пула |
| `-tunnel-ack-timeout-ms` | `TUNNEL_ACK_TIMEOUT_MS` | `5000` | Таймаут ACK |
| `-tunnel-ttl-sec` | `TUNNEL_TTL_SEC` | `300` | TTL туннеля |
| `-no-serve` | `NO_SERVE` | `false` | Клиент без listener'а |
| `-publish-wait-ms` | `PUBLISH_WAIT_MS` | `3000` | Пауза перед publish |
| `-send-to` | `SEND_TO` | — | hex NodeID получателя |
| `-send-text` | `SEND_TEXT` | — | Текст сообщения |
| `-send-repeat` | `SEND_REPEAT` | `1` | Сколько раз отправить |
| `-send-interval-ms` | `SEND_INTERVAL_MS` | `2000` | Пауза между отправками |
| `-exit-after-ms` | `EXIT_AFTER_MS` | `5000` | Force-exit для `-no-serve` |

## 13. Демонстрационные сценарии

### 13.1. Базовая доставка — `demo_tunnel.sh`

```bash
./scripts/demo_tunnel.sh
```

**Проверки:**

- `OK: 3 relays participated`
- `tunnel: build complete ... acks_received=3`
- `OK: plaintext not found in relay logs (E5-4)`
- `OK: node-2 received tunnel message`
- `route: entries = 10`

### 13.2. Восстановление — `demo_tunnel_recovery.sh`

```bash
./scripts/demo_tunnel_recovery.sh
```

**Проверки:**

- `send: acked × 2`
- `tunnel stats iteration=2 builds_ok=2 messages=2`
- `node-2 received 2 message(s)`

### 13.3. pcap — `demo_tunnel_pcap.sh`

```bash
sudo setcap cap_net_raw,cap_net_admin+eip $(which tcpdump)
./scripts/demo_tunnel_pcap.sh
```

**Проверки:**

- `OK: message sent successfully`
- `OK: plaintext not found in pcap (E5-4 satisfied)`
- `OK: plaintext not found in relay logs`
- `OK: relay activity present`

### 13.4. Пул — `demo_tunnel_pool.sh`

```bash
./scripts/demo_tunnel_pool.sh
```

**Проверки:**

- `send: pool built size=3`
- `iteration=1 sessions=3 pools=1 builds_ok=3 messages=1`
- `iteration=2 sessions=2 pools=1 builds_ok=3 messages=2`
- `iteration=3 sessions=2 pools=1 builds_ok=3 messages=3`
- `send: acked × 3`
- `node-2 received 3 message(s)`

## 14. Интеграционные тесты

`internal/tunnel/integration_test.go`:

- **`TestTunnelIntegration_EndToEnd`** — 5 in-process узлов, полный цикл Build → ACK × 3 → DATA → ACK.
- **`TestTunnelIntegration_NoPlaintextOnRelay`** — R2 не имеет E2E-ключей.
- **`TestTunnelIntegration_TTLExpires`** — TTL истекает.
- **`TestTunnelIntegration_LoopDetection`** — петля отклоняется.

**Unit-тесты:** `tunnel_test.go`, `manager_test.go`, `session_test.go`,
`message_test.go`, `profile_test.go`, `route_test.go`.
