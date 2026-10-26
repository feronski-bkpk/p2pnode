# План экспериментов

Документ описывает **цель, гипотезу, изменяемый фактор, метрику и
критерий успешности** для каждого расширения.

## 1. Общий принцип

Каждое расширение — это **эксперимент**:
- **Фиксированные условия** — N, K, ALPHA, тайм-ауты, схема bootstrap.
- **Изменяемый фактор** — один параметр, который проверяем.
- **Отклики** — метрики, измеряемые после прогона.
- **Критерий готовности** — что должно быть выполнено до сбора данных.
- **Число повторов** — сколько прогонов для статистики.

Все результаты складываются в `visualization/e7-experiments/` в формате
JSON + сводная таблица. Каждый прогон сопровождается **manifest**-файлом
(версия кода, конфиг, seed, время).

## 2. Передача файлов

### Цель

Реализовать **надёжную** передачу файлов произвольного размера через
**туннель** с E2E-шифрованием. Файл разбивается на **чанки**, каждый
ACK'ается, итог проверяется по **SHA-256**. Имя файла проходит
`SanitizeFileName` (защита от path traversal).

### Гипотеза

E2E-шифрование + ACK на каждый чанк + проверка SHA-256 всего файла
обеспечивают **целостную** передачу **без потерь** даже при отказе
ретранслятора (используется rebuild туннеля). `SanitizeFileName` +
`UniqueDestPath` защищают от записи за пределы `outDir`.

### Изменяемый фактор

**Размер чанка:** 16 КиБ / 32 КиБ / 64 КиБ.

Фиксируем: N=7, 3 ретранслятора, размер файла 1 МБ.

### Отклики (метрики)

| Метрика | Единица | Формула |
|---------|---------|---------|
| `throughput` | КиБ/с | `file_size / total_time` |
| `chunks_total` | шт | `ceil(file_size / chunk_size)` |
| `chunks_acked` | шт | Успешно доставленные |
| `retries` | шт | Повторные отправки чанков |
| `duration_ms` | мс | Полное время передачи |
| `sha256_match` | bool | `recv_sha256 == send_sha256` |
| `name_sanitized` | bool | `SanitizeFileName` не отклонил имя |
| `collision_suffix` | bool | `UniqueDestPath` добавил `.1`/`.2` |

### Критерий успешности

1. **`sha256_match == true`** для **100%** прогонов.
2. **`chunks_acked == chunks_total`** для **100%** прогонов.
3. **`throughput ≥ 100 КиБ/с`** при 3 ретрансляторах (loopback).
4. **`retries ≤ chunks_total / 10`** (не более 10% повторов).
5. **`name_sanitized == true`** для валидных имён (`payload.bin`,
   `файл.txt`).
6. **Злонамеренные имена отклоняются** (`../../etc/passwd`, `/tmp/x`,
   `CON`, `NUL`, `a/b`, `..`, `\x00`).
7. **Повторный `payload.bin`** → `payload.1.bin` (`collision_suffix`).

### Число повторов

**5 прогонов** на каждый размер чанка (итого 15 прогонов).

### Артефакт

`visualization/e7-experiments/E7-4/`:
- `run-01-chunk16k.json`, `run-02-chunk16k.json`, …;
- `summary.json` со средними/медианами по каждому размеру;
- `manifest.json` с конфигурацией;
- `malicious-name.log` — попытки с невалидными именами (dev/null).

### Демонстрация

```bash
./scripts/demo_file_transfer.sh 5 1 2
```

**Ожидаемо:** `OK: размер и SHA-256 совпадают`.

## 3. Ограниченный underlay

### Цель

Показать, что **IP-достижимость** узлов действительно **ограничена**
**nftables**, а не только bootstrap-конфигурацией. То есть узел **физически**
не может подключиться к узлу, с которым не должен.

### Гипотеза

Блокировка на уровне nftables **не ломает** работу DHT, если разрешены
**только** рёбра bootstrap-схемы. Узлы **находят** друг друга через
**разрешённые** связи.

### Изменяемый фактор

**Набор разрешённых пар** — из bootstrap-схемы (star / ring / tree).

Фиксируем: N=7, размер файла не важен (только connectivity).

### Отклики

| Метрика | Единица |
|---------|---------|
| `allowed_pairs` | шт |
| `blocked_pairs` | шт |
| `dht_convergence_time` | сек |
| `lookup_success_rate` | % |

### Критерий успешности

1. **`blocked_pairs`** — действительно **недоступны** (nc → timeout).
2. **`lookup_success_rate ≥ 80%`** через **разрешённые** связи.
3. **DHT сходится** за ≤ 30 сек.

### Артефакт

`visualization/e7-experiments/E7-5/`:
- `nftables-rules.txt` — сгенерированные правила;
- `connectivity-matrix.json` — что доступно, что нет;
- `lookup-batch.json`.

## 4. Управляемая деградация

### Цель

Показать **влияние** задержки и потерь на **lookup**, **туннель** и
**goodput файлов**.

### Гипотеза

- **Delay 50 мс** — увеличивает lookup latency **пропорционально**.
- **Loss 5%** — увеличивает retry в lookup, но успех сохраняется.
- **Delay 100 мс + loss 10%** — **значительно** снижает goodput
  файловой передачи.

**Дополнительно:** единый дедлайн RPC (`HandshakeTimeout` + `RPCTimeout`
на весь вызов) означает, что `Ping(timeout=5s)` реально укладывается
в ~5s, а не «10s handshake + 5s read». Это **наблюдаемо** в метрике
`dur_mean`.

### Изменяемый фактор

**Набор netem-параметров:**

| Режим | delay | loss |
|-------|-------|------|
| `baseline` | 0 | 0% |
| `delay-50` | 50 мс | 0% |
| `loss-5` | 0 | 5% |
| `stress` | 100 мс | 10% |

Фиксируем: N=7, 3 ретранслятора, файл 1 МБ, 30 lookup.

### Отклики

| Метрика | Единица |
|---------|---------|
| `lookup_rpc_mean` | шт |
| `lookup_duration_ms` | мс |
| `lookup_success_rate` | % |
| `file_throughput_kbps` | КиБ/с |
| `file_retries` | шт |

### Критерий успешности

1. **`baseline`** — throughput ≥ 100 КиБ/с, lookup success ≥ 95%.
2. **`delay-50`** — lookup latency **выросла** (не упала), success ≥ 80%.
3. **`loss-5`** — success ≥ 70%.
4. **`stress`** — система **не падает**, success ≥ 30%.

### Артефакт

`visualization/e7-experiments/E7-6/`:
- `baseline/`, `delay-50/`, `loss-5/`, `stress/` — по одному прогону;
- `summary.json` — сводная таблица.

### Реальные результаты (прогон N=5)

| Режим | `found` | `rpc_mean` | `dur_mean` | `dur_p95` | `file_match` |
|---|---|---|---|---|---|
| `baseline` | 11/12 | 2.75 | **3.2 ms** | 5.0 | **True** |
| `delay-50` | 11/12 | 2.58 | **154.1 ms** | 205.0 | **True** |
| `loss-5` | 12/12 | 3.00 | **124.4 ms** | 205.0 | **True** |
| `stress` | 12/12 | 3.00 | **724.2 ms** | 1343.0 | **False** |

## 5. Профиль ретранслятора

### Цель

Сравнить **ranked** (EMA) выбор ретранслятора со **случайным**.
Добавить **подсеть** в профиль и **исключение одной подсети**.

### Гипотеза

- **Ranked** даёт **лучший success rate** при **неоднородной** сети
  (часть ретрансляторов **медленные**).
- **Subnet diversity** **снижает** риск компрометации.

### Изменяемый фактор

**Режим выбора:** `ranked` vs `random` vs `subnet-aware`.

Фиксируем: N=15, 5 ретрансляторов в туннеле, искусственно «сломать»
**3** ретранслятора (задержка 500 мс).

### Отклики

| Метрика | Единица |
|---------|---------|
| `tunnel_build_success` | % |
| `tunnel_build_time_ms` | мс |
| `subnet_diversity` | уникальные /24 |
| `message_delivery_rate` | % |

### Критерий успешности

1. **Ranked** — success **выше** random **на 20%**.
2. **Subnet-aware** — diversity **выше** random **на 30%**.
3. Все три режима **не ломают** сеть.

### Артефакт

`visualization/e7-experiments/E7-7/`:
- `ranked/`, `random/`, `subnet-aware/`;
- `summary.json`.

### Реальные результаты (N=9, iterations=10)

| Режим | `routes` | `acked` | `accepted` | `unique_subnets` | `sub_rej` |
|---|---|---|---|---|---|
| `ranked` | 30 | 10/10 | 60 | **3** | 0 |
| `random` | 30 | 10/10 | 60 | **3** | 0 |
| `subnet` | 30 | 10/10 | 60 | **3** | **9** |

**Ключевой вывод:** в режиме `subnet` — `subnet_diversity_rejects=9`
(2-й кандидат из той же /24 отклоняется). Это **доказывает**, что
subnet diversity работает.

## 6. Исследовательская защита

### Цель

Проверить **контрмеры против Eclipse/Sybil** через:
- **multi-path lookup** (несколько независимых путей);
- **subnet diversity в routing** (контакты из разных /24).

### Гипотеза

При **Eclipse-атаке** (5 Sybil-узлов в одной /24) **multi-path
lookup + subnet diversity** **сохраняют** корректный ответ, тогда как
**без защиты** lookup **возвращает** результат атакующего.

### Изменяемый фактор

**Режим защиты:** `none` vs `multipath` vs `diversity` vs `both`.

**Модель атаки:**
1. Запустить N=11 честных узлов (5 Sybil'ов в одной /24).
2. Sybil'ы **заявляют** target по **фальшивому** адресу.
3. Инициатор делает 30 lookup'ов.
4. Считаем: **сколько** вернули **правильный** адрес.

### Отклики

| Метрика | Единица |
|---------|---------|
| `correct_lookup_pct` | % |
| `diversity_ratio` | уникальные /24 / всего |
| `attack_cost` | узлов-атакующих |

### Критерий успешности

1. **none** — correct ≤ 50%.
2. **multipath** — correct ≥ 80%.
3. **diversity** — correct ≥ 70%.
4. **both** — correct ≥ 90%.

### Артефакт

`visualization/e7-experiments/E7-8/`:
- модель атаки (docker-compose.attack.yml);
- 4 прогона (none / multipath / diversity / both);
- `summary.json`.

### Реальные результаты (N=11, 5 Sybil, 30 iter)

| Режим | `paths` | `correct` | `correct_pct` |
|---|---|---|---|
| `single` | 1 | **0/30** | **0.0%** |
| `multipath` | 3 | **30/30** | **100.0%** |

### Демонстрация

```bash
./scripts/demo_eclipse.sh 11 3 30 5
```

## 7. Регрессия

### Цель

Подтвердить, что **после всех расширений** базовые сценарии этапов
1–6 **работают**.

### Изменяемый фактор

**Флаги расширений:** все **выключены** (по умолчанию).

### Отклики

| Метрика | Единица |
|---------|---------|
| `go_test_pass` | bool |
| `e6_full_check_pass` | bool |
| `demo_tunnel_ok` | bool |
| `demo_store_ok` | bool |
| `demo_failures_ok` | bool |

### Критерий успешности

**Все** метрики **true**.

### Артефакт

`visualization/regression/`:
- `go-test.log`;
- `e6-full-check.log`;
- `demo-tunnel.log`, `demo-store.log`, `demo-failures.log`;
- `summary.json`.

### Реальные результаты

`go test -race ./...` — **все пакеты `ok`**, включая:

- `protocol` — frame chunked tests.
- `routing` — `TestBucketFillsToK_*`, `TestRemoveIfDead`,
  `TestAddUsesTableLivenessChecker`.
- `rpc` — `TestRPC_SilentPeer_HandshakeDeadline`.
- `tunnel` — `TestTunnelIntegration_*`.
- `filetransfer` — `TestSanitizeFileName_*`, `TestUniqueDestPath_*`.
- `integration` — `TestLookupAfterSeedDown`,
  `TestBootstrapFailsOnEmptyTable`, `TestFindValuePicksHighestSeq`,
  `TestPublishFailsOnNoCandidates`, `TestAntiRollback`.

## 8. Порядок выполнения

```
E7-2 (документ) ────┐
                    ├──▶ E7-4 (file-transfer) ──▶ E7-1 (регрессия)
E7-3 (флаги) ───────┘                          ──▶ E7-6 (netem)

E7-5 (underlay) ──────▶ E7-7 (профиль) ──────▶ E7-8 (Eclipse)
```

## 9. Воспроизводимость

Каждый прогон **фиксирует**:
- версию кода (`git rev-parse HEAD`);
- конфиг (CLI-флаги + env);
- seed (если применимо);
- временную базу (Unix ms);
- N, k, ALPHA, R, тайм-ауты;
- схему bootstrap;
- `SubnetDiversityLimit`, `RefreshInterval`.

**Manifest** сохраняется рядом с данными в формате JSON.

## 10. Дополнительные юнит-тесты

Помимо демо-скриптов, **регрессия** покрыта unit-тестами:

- **`TestLookupAfterSeedDown`** (`internal/integration/lookup_seed_down_test.go`) —
  lookup после kill seed'а (E6-5 unit).
- **`TestBootstrapFailsOnEmptyTable`** — `Bootstrap` → error при
  недоступных seed'ах.
- **`TestFindValuePicksHighestSeq`** — `FindValue` max(seq) среди
  реплик (E3-4 unit).
- **`TestPublishFailsOnNoCandidates`** — `Publish` → `ErrNotEnoughReplicas`
  при N=1.
- **`TestAntiRollback`** — `Publish(seq=5)` после `seq=10` →
  `ErrNotEnoughReplicas`.
- **`TestSanitizeFileName_OK`/`Reject`**, **`TestUniqueDestPath_*`** —
  безопасность имени файла (E7-4 unit).
- **`TestBucketFillsToK_*`**, **`TestBucketSubnetDiversityRejectsThird`**,
  **`TestSubnetDiversityLimitAccessors`** — subnet diversity (E7-8 unit).
- **`TestRPC_SilentPeer_HandshakeDeadline`** — единый дедлайн RPC.
- **`TestConn_WriteDeadline_PeerNotReading`**,
  **`TestConn_ReadDeadline_SilentPeer`** — таймауты транспортного уровня.
- **`TestReadFrame_RandomChunkBoundaries`**, **`ChunkedSizes`**,
  **`TwoFramesChunked`**, **`EOFWithData`** — кадрирование при
  случайных границах.
