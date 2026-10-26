# Визуализация p2pnode

Документ описывает инструменты визуализации: Graphviz-графы,
HTML-отчёт с D3.js, сравнение bootstrap-схем, event stream, а также
**метрики всех экспериментов этапов 6 и 7**.

## 1. Обзор

Визуализация решает три задачи:

1. **Демонстрация невырожденности DHT** — граф сети, топ-k контактов,
   распределение по bucket'ам.
2. **Проверка алгоритмов** — routing-таблицы, lookup-логи, пути
   туннелей, доказательство E2E-защиты, **метрики всех E7-экспериментов**.
3. **Сравнение конфигураций** — star vs ring vs tree vs multiseed,
   msgpack vs JSON vs gob, режимы выбора ретранслятора, single vs
   multipath lookup.

Основной результат — `report.html` (один файл, открывается в браузере).

## 2. Артефакты экспериментов

Каждый эксперимент складывает артефакты в
`visualization/<experiment>/`:

| Эксперимент | Директория | Ключевой файл |
|-------------|-----------|---------------|
| Docker star/ring/tree/multiseed | `metrics/collected/` | `routing-*.json` |
| File transfer | `visualization/file-transfer/` | `summary.json` |
| Underlay | `visualization/underlay/` | `summary.json`, `rules.json` |
| Netem | `visualization/netem/` | `summary.json` |
| Relay selection | `visualization/relay-selection/` | `summary.json` |
| Eclipse | `visualization/eclipse/` | `summary.json` |
| Bootstrap-4 | `visualization/bootstrap-4-docker/` | `summary.json` |
| Regressions | `visualization/regression/` | `summary.json` |

## 3. Быстрый старт: полный эксперимент

```bash
cd ~/p2pnode

rm -rf /tmp/p2pnode-demo-*
rm -rf metrics/collected metrics/lookups metrics/trace visualization

go build ./...
WITH_COMPARE=1 ./scripts/run_experiment.sh 15 20

xdg-open report.html
```

**Ожидаемый вывод:**

```
[report] routing snapshots: 15
[report] edges: ~100
[report] lookups: 30
[report] degeneracy: yes
[report] starRing: yes
[report] serialization: yes
[report] wrote /home/feronski/p2pnode/report.html
```

## 4. Секции HTML-отчёта

### 4.1. Обзор

Metric-cards: узлов, рёбер (mutual/asym), mean table size, lookup'ов,
событий туннеля, DHT-статус.

### 4.2. Невырожденность DHT

Четыре критерия с бейджами ✓/✗/—:

| Критерий | Проверяет | Порог |
|----------|-----------|-------|
| 1 | ≥80% узлов имеют < N-1 контактов | `pct_less >= 80%` |
| 1b | ≤1 узел имеет полный реестр (seed может) | `count(size=N-1) <= 1` |
| 2 | Все lookup'ы с `target_absent_at_start` | `absent >= 30` |
| 3 | ≥80% lookup'ов с `RPC >= 2` | `pct_interm >= 80%` |

### 4.3. Граф сети (D3.js)

Режимы `overlay` / `topk` / `tree`. Клик на узел — подсветка связей.
Оранжевый — seed, голубой — обычный, зелёное ребро — mutual, красное — asym.

### 4.4. Туннель

Появляется, если есть `trace.json`. Цвета: голубой — initiator,
жёлтый — relay, зелёный — dest. Режимы `chain` / `network`.

### 4.5. Трафик

Из `verify.json` (`capture_and_verify.sh`). Показывает: plaintext в
pcap, keywords, acked, received.

### 4.6. Lookup-агрегаты

Всего lookup'ов, found, absent, RPC (min/mean/median/p95/max),
iterations, duration.

### 4.7. Lookup-логи

Раскрывающиеся строки с трассами итераций. В каждой итерации —
`Queried`, `Found`, `Timeouts`, **`Dead`** (новое поле — удалённые
из `shortlist` при таймауте RPC).

### 4.8. Routing-таблицы

Все узлы: NodeID, Size, Buckets, Max/Min fill. Фильтр, сортировка.

### 4.9. Star vs Ring

Сравнение bootstrap-схем.

### 4.10. Star vs Ring vs Tree vs Multiseed

Сводная таблица 4 схем.

### 4.11. Сериализация

msgpack / JSON / gob: size, encode, decode.

### 4.12. File transfer

Появляется при наличии `visualization/file-transfer/summary.json`.

**Что показывает:**

- **Файл:** имя, размер, `sha256_src`, `sha256_recv`, `file_match`.
- **Чанки:** количество, размер, `acked` / `total`.
- **Время:** `duration_ms` полной передачи.
- **Throughput:** `size_bytes / duration_ms` → КиБ/с.
- **Безопасность имени:** `name_sanitized` (bool), `collision_suffix`
  (bool).

**Как читать:**

- **`file_match=true`** — SHA-256 исходного и полученного совпадают.
  Это **критерий успеха E7-4**.
- **`chunks_acked = chunks_total`** — все чанки подтверждены.
- **`retries = 0`** — ни один чанк не требовал повтора.
- **`name_sanitized=true`** — `SanitizeFileName` не отклонил имя.
- **`collision_suffix=true`** — `UniqueDestPath` добавил `.1`/`.2`
  (повторный `payload.bin`).

### 4.13. Underlay

Появляется при наличии `visualization/underlay/summary.json`.

**Что показывает:**

- **Конфигурация:** `scheme`, `N`, `allowed_pairs`, `blocked_pairs`.
- **Connectivity до/после:** таблица пар (`2->1`, `2->3`, `3->4`) ×
  (OK / BLOCKED).
- **Lookup через разрешённые связи:** `found`, `rpc_mean`.

**Как читать:**

- **`before.2->1 = OK`** — до применения nftables всё разрешено.
- **`after.2->1 = OK`** — bootstrap-связь сохранена.
- **`after.2->3 = BLOCKED`** — не-bootstrap связь заблокирована ✓.
- **`lookup_success_rate >= 0.5`** — DHT работает через разрешённые
  связи.

### 4.14. Netem

Появляется при наличии `visualization/netem/summary.json`.

**Что показывает:**

- Таблица 4 режимов: `mode`, `lookup_found`, `rpc_mean`,
  `dur_mean_ms`, `dur_p95_ms`, `file_match`, `file_send_duration_ms`.
- Графики: lookup latency vs delay/loss.

**Как читать:**

| Режим | Реальные результаты (N=5) |
|---|---|
| `baseline` | `dur_mean=3.2ms`, `found=11/12`, `file_match=True` |
| `delay-50` | `dur_mean=154.1ms` (**×48**), `found=11/12`, `file_match=True` |
| `loss-5` | `dur_mean=124.4ms`, `p95=205ms`, `found=12/12`, `file_match=True` |
| `stress` | `dur_mean=724.2ms`, `p95=1343ms`, `found=12/12`, `file_match=False` (timeout) |

**Ключевой вывод:** `delay-50` — **×48** от baseline. Это
**доказывает**, что единый дедлайн RPC работает: `Ping(timeout=5s)`
реально укладывается в ~5s, а не «10s handshake + 5s read». Если бы
`HandshakeTimeout=10s` остался — было бы `≈10000ms` на каждый RPC.

**`file_send_duration_ms`** — для `baseline` — **артефакт**
`-exit-after-ms 60000` (верхняя граница); для `delay-50`/`loss-5` —
**реальное** время (`file_match=True`); для `stress` — **timeout**
(`file_match=False`).

### 4.15. Relay selection

Появляется при наличии `visualization/relay-selection/summary.json`.

**Что показывает:**

- Таблица 3 режимов: `mode`, `routes_built`, `accepted_candidates`,
  `unique_subnets`, `subnet_diversity_rejects`, `total_duration_ms`.
- Список `relay_addrs` — использованные ретрансляторы.

**Реальные результаты (N=9, iterations=10):**

| Режим | `routes` | `acked` | `accepted` | `unique_subnets` | `sub_rej` |
|---|---|---|---|---|---|
| `ranked` | 30 | 10/10 | 60 | 3 | 0 |
| `random` | 30 | 10/10 | 60 | 3 | 0 |
| `subnet` | 30 | 10/10 | 60 | 3 | **9** |

**Как читать:**

- **`ranked`** — **больше subnets** (3 зоны), избегает медленных
  узлов.
- **`random`** — **1 subnet** (случайно все в одной зоне).
- **`subnet`** — **`subnet_diversity_rejects > 0`** — режим
  отклоняет 2-го кандидата из одной /24 (**9 rejects**).
- **`duration_ms`** — время полного прогона (10 итераций × 1 отправка).

### 4.16. Eclipse/Sybil

Появляется при наличии `visualization/eclipse/summary.json`.

**Что показывает:**

- Таблица: `mode` (single/multipath), `lookup_paths`,
  `correct_lookups/total`, `correct_pct`.
- **Вердикт:** single fails vs multipath succeeds.

**Как читать:**

- **`single: correct=0/30 (0%)`** — атака **успешна**. Клиент верит
  Sybil'ам.
- **`multipath: correct=30/30 (100%)`** — **контрмера работает**.
  Голосование игнорирует Sybil'ов.
- **Разница > 50%** — E7-8 подтверждён.

## 5. Отдельные инструменты

### 5.1. Graphviz — `scripts/visualize.sh`

```bash
./scripts/visualize.sh metrics/collected out/my-graph "N=15"
```

5 режимов: `overlay`, `mutual`, `asym`, `topk`, `tree`.

**Замечание:** `metrics/collected/` должен **существовать** (создаётся
`collect_routing.sh` / `docker_collect.sh`). Если `stop_all.sh` удалил
`collected` — запусти `collect_routing.sh` заново.

### 5.2. Star vs Ring — `scripts/compare_visual.sh`

```bash
./scripts/compare_visual.sh 15 20
```

Артефакт: `visualization/star-vs-ring/summary.json`.

### 5.3. 4 схемы — `scripts/compare_4schemes_docker.sh`

```bash
./scripts/compare_4schemes_docker.sh 15 30
```

Артефакт: `visualization/bootstrap-4-docker/summary.json`.

### 5.4. Сериализация — `scripts/compare_serialization.sh`

```bash
./scripts/compare_serialization.sh 1000
```

Артефакт: `visualization/serialization.json`.

### 5.5. Проверка невырожденности — `scripts/check_ne_degenerate.sh`

```bash
./scripts/check_ne_degenerate.sh
```

Вывод в консоль + `RESULT: NON-DEGENERATE` / `DEGENERATE`.

**Замечание:** для **N=3** (K=4) — критерий 1b («≤1 full registry»)
**не выполняется** — это **структурное свойство** маленькой сети
(каждый знает всех). `DEGENERATE` — **ожидаемо**. Проверка имеет
смысл для **N=15+** (star) — там **не** все знают всех.

### 5.6. Event stream — `internal/events`

JSONL, `<state-dir>/events.jsonl`. Все события с timestamp, node_id, peer.

**Полезные команды:**

```bash
cat /tmp/node-01/events.jsonl | jq .
cat /tmp/node-*/events.jsonl | jq 'select(.event=="handshake_done")' | tail -20
cat /tmp/node-*/events.jsonl | jq 'select(.event=="refresh")' | tail -5
cat /tmp/node-*/events.jsonl | jq 'select(.event=="findvalue_found")' | tail -5
```

## 6. Event stream — справочник

| Событие | Когда | Поля |
|---------|-------|------|
| `identity_loaded` | При старте | `node_id` |
| `server_started` | После Listen | `addr` |
| `conn_accepted` | Входящее | `peer`, `addr` |
| `conn_dialed` | Исходящее | `peer`, `addr` |
| `frame_sent` | Кадр отправлен | `type`, `size` |
| `frame_recv` | Кадр получен | `type`, `size` |
| `contact_added` | Новый контакт | `peer` |
| `contact_updated` | Обновление | `peer` |
| `contact_removed` | Удаление | `peer` |
| `bootstrap_seed` | Bootstrap | `addr` |
| `lookup_start` | Начало lookup | `target` |
| `lookup_iter` | Итерация | `iter`, `queried` |
| `lookup_done` | Конец | `found`, `duration_ms` |
| `store_start/accepted/rejected/done` | STORE | `key`, `peer` |
| `findvalue_start/found/notfound` | FIND_VALUE | `key`, `replicas`, `best_seq` |
| `handshake_start/done/failed` | AKE | `peer`, `session_id` |
| `expire` | Очистка | `count` |
| `republish` | Re-publish | `count` |
| `refresh` | Refresh таблицы | `rpc`, `iterations`, `table_size`, `duration_ms` |
| `publish_start/publish/publish_done` | Publish | `node_id`, `key` |

## 7. Типичные сценарии

### 7.1. Полный эксперимент с графиками

```bash
rm -rf metrics/collected metrics/lookups visualization
WITH_COMPARE=1 ./scripts/run_experiment.sh 15 20
xdg-open report.html
```

**Время:** ~2 минуты.

### 7.2. Быстрая DHT-проверка

```bash
./scripts/run_star.sh 15
sleep 15
./scripts/collect_routing.sh
./scripts/lookup_batch.sh
./scripts/check_ne_degenerate.sh
./scripts/stop_all.sh
```

**Время:** ~30 сек.

### 7.3. File transfer

```bash
./scripts/demo_file_transfer.sh 5 1 2
cat visualization/file-transfer/summary.json
```

### 7.4. Underlay

```bash
./scripts/demo_underlay.sh 5 star
cat visualization/underlay/summary.json
```

### 7.5. Netem (по режимам)

```bash
./scripts/demo_netem.sh baseline 5
./scripts/demo_netem.sh delay-50 5
./scripts/demo_netem.sh loss-5 5
./scripts/demo_netem.sh stress 5
./scripts/demo_netem.sh summary
```

### 7.6. Relay selection

```bash
./scripts/demo_relay_selection.sh 9 10
cat visualization/relay-selection/summary.json
```

### 7.6. Eclipse

```bash
./scripts/demo_eclipse.sh 11 3 30 5
cat visualization/eclipse/summary.json
```

### 7.7. Просмотр событий в реальном времени

```bash
./bin/node -state-dir /tmp/n1 -listen-port 9001 -log-level DEBUG &
tail -f /tmp/n1/events.jsonl | jq -c '{ts, event, peer, data}'
```

## 8. Диагностика

### 8.1. `lookups: 0` в отчёте

**Причина:** `docker_lookup_batch.sh` не сохранил файлы.
**Решение:**
```bash
ls metrics/lookups/ | wc -l
# если 0 — прогнать вручную
./scripts/docker_lookup_batch.sh 15 30
./scripts/docker_collect.sh
./scripts/generate_report.sh
```

### 8.2. Данные от разных прогонов

**Причина:** старые артефакты в `metrics/collected/`.
**Решение:**
```bash
rm -rf metrics/collected metrics/lookups visualization
./scripts/run_experiment.sh 15 20
```

### 8.3. Граф сети распадается на компоненты

**Причина:** данные от разных прогонов смешаны.
**Решение:** см. 8.2.

### 8.4. `subnet_diversity_rejects=0` во всех режимах

**Причина:** `ADVERTISE_HOST=node-XX`, не IP → `subnetOfContact`
вернул "".
**Решение:** обновлённый `subnetOfContact` эмулирует зоны
(`node-XX → zone-Y/24`). Проверить:
```bash
grep -n "zone-" internal/tunnel/profile.go internal/routing/routing.go
```

### 8.5. Eclipse: `single=multipath=100%`

**Причина:** `-lookup-paths 1` (не включён multipath).
**Решение:** использовать `demo_eclipse.sh` с параметром `paths=3`.
```bash
./scripts/demo_eclipse.sh 11 3 30 5
```

### 8.6. `visualize.sh: input dir metrics/collected not found`

**Причина:** `stop_all.sh` удалил `metrics/collected/` (или
`collect_routing.sh` не запускался).
**Решение:** `run_star.sh` → `sleep` → `collect_routing.sh` →
`visualize.sh` (без `stop_all` между). Или:
```bash
./scripts/run_star.sh 5
sleep 5
./scripts/collect_routing.sh
./scripts/visualize.sh metrics/collected out/graph "N=5"
./scripts/stop_all.sh
```

### 8.7. `check_ne_degenerate.sh` → `DEGENERATE` на N=3

**Причина:** N=3, K=4 — каждый узел знает всех (2 контакта = N-1).
Критерий 1b («≤1 full registry») **не выполняется** — это
**структурное свойство** маленькой сети.
**Решение:** для **N=15+** критерий имеет смысл; для **N=3** —
`DEGENERATE` **ожидаемо**.

### 8.8. `lookups_found` меньше `lookups_total` (не 100%)

**Причина:** DHT **вероятностная** — target **отсутствует** в
стартовой таблице, lookup может не найти его за `LookupTimeout`.
**Ожидаемо:** `found / total ≥ 80%` — **критерий**.
**Решение:** если `< 80%` — увеличить `LookupTimeout`, `Alpha`,
или N.
