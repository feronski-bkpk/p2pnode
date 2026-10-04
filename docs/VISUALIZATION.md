# Визуализация p2pnode

Документ описывает инструменты визуализации, реализованные в проекте:
Graphviz-графы, HTML-отчёт с D3.js, сравнение bootstrap-схем, event
stream. Приведены полные команды запуска, ожидаемый вывод и правила
интерпретации.

## 1. Обзор

Визуализация решает три задачи:

1. **Демонстрация невырожденности DHT** — граф сети, топ-k контактов,
   распределение по bucket'ам.
2. **Проверка алгоритмов** — routing-таблицы, lookup-логи с трассами
   итераций, пути туннелей, доказательство E2E-шифрования.
3. **Сравнение конфигураций** — star vs ring, msgpack vs JSON vs gob.

Все инструменты в `scripts/`. Основной результат — `report.html`
(один файл, открывается в браузере, не требует сети).

## 2. Что уже реализовано

| Инструмент | Файл | Что делает |
|------------|------|------------|
| HTML-отчёт | `scripts/generate_report.sh` | Собирает все артефакты в `report.html` |
| Шаблон отчёта | `scripts/report_template.html` | CSS + D3.js + логика рендера |
| Graphviz-графы | `scripts/visualize.sh` | DOT/PNG/SVG-графы в 5 режимах |
| Star vs Ring | `scripts/compare_visual.sh` | Прогон двух схем, сравнение, `summary.json` |
| Сериализация | `scripts/compare_serialization.sh` | msgpack / JSON / gob — размер + скорость |
| Сбор routing | `scripts/collect_routing.sh` | Экспорт routing-снапшотов с узлов |
| Lookup-батч | `scripts/lookup_batch.sh` | 30 контрольных lookup'ов с трассами |
| Проверка DHT | `scripts/check_ne_degenerate.sh` | Критерии E6-3 в консоль |
| Event stream | `internal/events` | JSONL-лог событий каждого узла |

## 3. Быстрый старт: полный эксперимент

```bash
cd ~/p2pnode

# 1. Очистить старые артефакты (важно! иначе данные смешаются)
rm -rf /tmp/p2pnode-demo-*
rm -rf metrics/collected metrics/lookups metrics/trace visualization

# 2. Собрать бинарник
go build ./...

# 3. Полный прогон N=15, 20 секунд сходимости, со сравнением схем
WITH_COMPARE=1 ./scripts/run_experiment.sh 15 20

# 4. Открыть отчёт
xdg-open report.html
```

**Что происходит по шагам:**

1. `run_experiment.sh` вызывает `stop_all.sh`, чистит `metrics/`, `logs/`, `state/`.
2. `run_star.sh 15` — запускает 15 узлов star-схемой (порты 9001–9015).
3. Ждёт 20 сек сходимости.
4. `collect_routing.sh` — собирает routing-снапшоты в `metrics/collected/`.
5. `lookup_batch.sh` — 30 контрольных lookup'ов в `metrics/lookups/`.
6. `check_ne_degenerate.sh` — проверяет невырожденность, печатает результат.
7. Если `WITH_COMPARE=1`: `compare_visual.sh` (star vs ring) + `compare_serialization.sh`.
8. `generate_report.sh` — собирает `report.html`.

**Ожидаемый вывод в конце:**

```
[report] routing snapshots: 15
[report] edges: ~100
[report] lookups: 30
[report] traces: 0 (или N из текущего прогона)
[report] verify: yes/no
[report] degeneracy: yes
[report] starRing: yes (если WITH_COMPARE=1)
[report] serialization: yes (если WITH_COMPARE=1)
[report] wrote /home/feronski/p2pnode/report.html
```

Если `lookups: 0` или `routing snapshots` не совпадает с N — смотреть раздел 9 «Диагностика».

## 4. HTML-отчёт — что внутри

Единый `report.html`, открывается в браузере. Состоит из секций:

### 4.1. Обзор

Metric-cards: узлов, рёбер (mutual/asym), mean table size (min/max),
lookup'ов (успешных/всего), событий туннеля, DHT-статус.

**Как читать:**
- **DHT-статус OK** — все 4 критерия E6-3 выполнены (см. секцию ниже).
- **DHT-статус FAIL** — хотя бы один критерий не выполнен, детали в
  секции «Невырожденность».
- **Рёбер → mutual** — большинство рёбер в Kademlia взаимные (A знает
  B и B знает A). Если mutual мало — плохой признак, узлы плохо
  сходятся.

### 4.2. Невырожденность DHT

Четыре критерия с бейджами ✓/✗/—:

| Критерий | Что проверяет | Порог |
|----------|---------------|-------|
| 1 | ≥80% узлов имеют < N-1 контактов | `pct_less >= 80%` |
| 1b | Ни один узел не имеет полный реестр | `max_size < N-1` |
| 2 | Все lookup'ы с target, отсутствовавшим до старта | `absent_at_start = true` |
| 3 | Все lookup'ы с RPC ≥ 2 (промежуточный узел) | `rpc >= 2` |

**Как читать:**
- **NON-DEGENERATE** — все 4 критерия ✓. DHT не вырождена в полный
  каталог, lookup'ы действительно многошаговые.
- **DEGENERATE** — хотя бы один ✗. Например, если все узлы знают
  всех (критерий 1b нарушен), DHT бесполезна.
- **— (жёлтый)** — критерий не проверен, потому что нет данных
  (например, `lookups = []`). Это не FAIL, а «нечего проверять».

**Таблица размеров** — каждый узел, его `size`, процент от `N-1`.
Полезна для поиска выбросов: один узел с size=N-1 означает, что он
знает всю сеть (что нормально для seed'а в star-схеме).

### 4.3. Граф сети (D3.js)

Force-directed layout. Управление:

| Элемент | Что делает |
|---------|------------|
| Режим `overlay` | Все рёбра (плотно) |
| Режим `topk` | 3 ближайших по XOR на узел (разреженный) |
| Режим `tree` | Только исходящие рёбра seed'а |
| Чекбокс «Подписи» | Показывать/скрывать ID узлов |
| Чекбокс «Стрелки» | Показывать направление рёбер |
| Чекбокс «Только взаимные» | Оставить только mutual-рёбра |
| Клик на узел | Подсветить связи выбранного узла |
| Drag | Перетаскивать узлы |

**Цвета:**
- **Оранжевый узел** — seed (узел с максимальным in-degree).
- **Голубой узел** — обычный узел.
- **Зелёное ребро** — mutual (A→B и B→A).
- **Красное ребро** — asymmetric (только A→B).

**Как читать:**
- **Одна связная компонента** — сеть корректно сходится. Если
  граф распадается на 2+ компоненты — это либо смешение данных из
  разных прогонов (см. раздел 9), либо узлы не нашли друг друга.
- **Много mutual (зелёных)** — здоровое состояние Kademlia.
- **Много asym (красных)** — некоторые узлы не отвечают на PING или
  добавлены недавно (ещё не подтверждены).
- **Изолированные узлы** — узлы без рёбер. В star-схеме возможны на
  раннем этапе, если bootstrap не завершился.
- **Сильно разный размер узлов** — размер пропорционален `size`
  (числу контактов в routing-таблице). Seed в star-схеме — крупнее.

### 4.4. Туннель

Появляется только если есть `trace.json` хотя бы с одним событием.

Управление:
- Выбор trace'а (из `metrics/trace/` или `/tmp/p2pnode-demo-*/`).
- Режим `chain` — только путь туннеля (initiator → relays → dest).
- Режим `network` — полный граф сети, поверх — путь туннеля.
- Чекбокс «Анимация» — красные точки движутся по tunnel-рёбрам.
- Чекбокс «Временная шкала» — события trace'а внизу.

**Цвета узлов:**
- **Голубой** — initiator (Alice).
- **Жёлтый** — ретранслятор (relay).
- **Зелёный** — dest (Bob).
- **Серый** — не участвует в туннеле.

**Как читать:**
- Должно быть **5 узлов в цепочке**: 1 initiator + 3 relay + 1 dest.
  Если меньше — либо `max-hops` меньше, либо ретрансляторов не хватило.
- **Клик на узел** — tooltip: role, что видит узел (relay видит
  `tunnel_id`, `next_hop`, но не payload).
- **Временная шкала** — события с временем, tunnel_id, message_id,
  hop_index. Полезна для отладки: видно, что ACK'и от ретрансляторов
  пришли раньше, чем BUILD_OK от dest.

### 4.5. Трафик

Появляется, если найден `verify.json` (из `capture_and_verify.sh`).

**Что показывает:**
- Таблица проверок: plaintext в pcap, keywords протокола, acked,
  received.
- Перехваченные кадры: тип, заголовок, payload (msgpack + AEAD-hex).

**Как читать:**
- **Plaintext в pcap → «не найден ✓»**
- **Ключевые слова протокола → «0 ✓»** — msgpack-поля (`tunnel_id`,
  `dest_id`) зашифрованы handshake'ом и не читаются в pcap.
- **AEAD ciphertext** (красным) — реальный шифротекст, без ключей
  не расшифровать.
- **msgpack** (серым) — поля протокола для маршрутизации (видны
  ретранслятору, но не содержат payload).

### 4.6. Handshake AKE — схема

Статическая SVG-схема: 3 сообщения, 4 DH, HKDF, SecureConn.
Не данные, а справочная диаграмма.

### 4.7. Lookup-агрегаты

Появляется, если есть lookup'ы.

**Метрики:**
- Всего lookup'ов, успешных (target в final), с absent_at_start.
- RPC: min / mean / median / p95 / max.
- Iterations: mean / max.
- Duration: mean / median / max (мс).

**Как читать:**
- **found = 30/30** — все lookup'ы успешны.
- **RPC median 3** — типичный lookup делает 3 RPC. Если median = 2 —
  подозрительно мало (lookup может быть вырожденным). Если > 10 —
  сеть плохо сходится.
- **Iterations mean 1.6** — lookup в среднем 1.6 итерации. Хорошо.
- **Duration mean 5 мс** — быстро. На реальной сети будет больше.
- **p95 > max/2** — есть выбросы (медленные lookup'ы). Нормально для
  распределённой сети.

### 4.8. Lookup-логи

Раскрывающиеся строки: `initiator → target` + RPC/iterations/duration
+ бейдж found/not found.

**Клик на строку → раскрыть:**
- target_absent_at_start.
- timeouts.
- Финальные контакты (chip'ы с короткими ID).
- Итерации: `iter=N · queried=X · found=Y · timeouts=Z` + chip'ы
  queried / found / timeouts.

**Как читать:**
- **iter=0** — первый запрос к ближайшим из routing-таблицы.
- **iter=1, 2** — расширение списка кандидатов через найденные
  контакты.
- **found** на iter=0 — target был у первого узла (мало итераций).
- **found** на iter=2+ — target найден через цепочку узлов
  (многошаговый lookup).
- **timeouts > 0** — какой-то узел не ответил. Если timeouts только
  в одном iter и lookup всё равно found — нормально.

### 4.9. Routing-таблицы

Таблица всех узлов: NodeID, Size, Buckets, Max/Min fill.
Фильтр по ID, сортировка по size/buckets/max_fill.

**Как читать:**
- **Size = N-1** — узел знает всех. Плохо (вырождение).
- **Size = 4** — узел знает мало. Типично для star-схемы у
  не-seed-узлов.
- **Buckets** — число непустых k-bucket'ов. Для N=15 обычно 3-5.
- **Max fill** — максимальная заполненность bucket'а. Не должна
  превышать `K=4`.

### 4.10. Star vs Ring

Появляется, если `WITH_COMPARE=1` или вручную запускался
`compare_visual.sh`.

**Метрики:**
| Метрика | Что означает |
|---------|--------------|
| Nodes | Число узлов (должно быть одинаково) |
| Table size min/max/mean | Заполненность routing-таблиц |
| Buckets per node mean | Среднее число непустых bucket'ов |
| Max bucket fill | Максимальная заполненность |
| Edges total | Общее число рёбер |
| Max out-degree | Максимум исходящих связей у одного узла |
| Max in-degree | Максимум входящих (кого знают больше всех) |

**Как читать:**
- **Star: max_in_degree = 14, mean_size = 7** — все знают seed'а
  (in-degree=14 у seed'а). Нормально для star.
- **Ring: max_in_degree ≈ max_out_degree** — нет концентрации.
  Равномернее.
- **Star vs Ring: diff** — (ring - star). Знак показывает, куда
  сдвинулось.
- **Ring: edges > star: edges** — ring даёт больше рёбер, потому
  что узлы знают соседей с обеих сторон.

### 4.11. Сериализация

Появляется, если `WITH_COMPARE=1`.

**Таблица:** format, size (bytes), encode (µs), decode (µs).

**Как читать:**
- **msgpack** должен быть меньше JSON в ~1.7× и быстрее в decode.
- **gob** может быть быстрее msgpack по encode, но медленнее в
  decode и Go-only.
- Если gob вдруг меньше msgpack — подозрительно, возможно разные
  данные.

## 5. Отдельные инструменты

### 5.1. Graphviz-графы — `scripts/visualize.sh`

**Назначение:** строит DOT-файлы и рендерит в PNG/SVG без браузера.

**Использование:**

```bash
# На конкретной директории со снапшотами
./scripts/visualize.sh metrics/collected out/graph-overlay "Star N=15"

# Или через хелпер
./scripts/visualize.sh overlay
```

**Пять режимов:**

| Режим | Что показывает | Когда полезно |
|-------|----------------|---------------|
| `overlay` | Все рёбра | Общая картина (плотно) |
| `mutual` | Только A↔B | Здоровье сети |
| `asym` | Только A→B без B→A | Найти "односторонние" связи |
| `topk` | 3 ближайших по XOR | Kademlia-структура |
| `tree` | Иерархия seed → bucket → contacts | Топология от seed'а |

**Результат:** `out/graph-<mode>.png` и `.svg`.

**Как читать DOT-файлы вручную:**
```bash
dot -Tpng metrics/collected/*.dot -o graph.png  # если DOT есть
# или смотреть содержимое
head -50 out/graph-overlay.dot
```

### 5.2. Star vs Ring — `scripts/compare_visual.sh`

```bash
./scripts/compare_visual.sh 15 20
```

Запускает два прогона (star и ring), сохраняет:
- `visualization/star-vs-ring/star-overlay.png`
- `visualization/star-vs-ring/ring-overlay.png`
- `visualization/star-vs-ring/summary.json`

**Особенность:** `compare_visual.sh` не трогает `metrics/lookups/` —
использует trap для сохранения/восстановления. Так что можно
запускать после `lookup_batch.sh` без потери данных.

### 5.3. Сериализация — `scripts/compare_serialization.sh`

```bash
./scripts/compare_serialization.sh 1000
```

Замеряет 1000 итераций encode/decode для `FIND_NODE_RESPONSE` в трёх
форматах. Результат в `visualization/serialization.json`.

### 5.4. Проверка невырожденности — `scripts/check_ne_degenerate.sh`

```bash
./scripts/check_ne_degenerate.sh
```

Печатает в консоль:
```
[check] N = 15
[check] table sizes: min=3 max=10 mean=6.67
[check] nodes with < N-1 contacts: 15/15 (100.0%)
[check] criterion 1 (>=80% < N-1):        True
[check] criterion 1b (no full registry):  True
[check] buckets per node: min=2 max=6 mean=4.07
[check] max bucket fill:  min=1 max=4

[check] lookup'ов всего:               30
[check] цель отсутствовала до старта:  30
[check] с промежуточным узлом (RPC≥2): 30
[check] успешных (target в final):     29
[check] criterion 2 (>=30 absent):     True
[check] criterion 3 (>=30 w/ interm):  True

[check] RESULT: NON-DEGENERATE (все критерии выполнены)
```

**Как интерпретировать:**
- **RESULT: NON-DEGENERATE**
- **criterion 1 True** — 100% узлов имеют <14 контактов.
- **criterion 3 True** — 30 lookup'ов с RPC≥2, многошаговые.

### 5.5. Event stream — `internal/events`

**Формат:** JSONL, одна строка на событие, в `<state-dir>/events.jsonl`.

**Пример:**
```json
{"ts":1696240123456,"node":"abcd1234","event":"conn_accepted","peer":"ef567890","addr":"127.0.0.1:9001","data":{}}
{"ts":1696240123489,"node":"abcd1234","event":"frame_recv","peer":"ef567890","data":{"type":"PING","size":120}}
{"ts":1696240123501,"node":"abcd1234","event":"handshake_done","peer":"ef567890","data":{"session_id":"..."}}
```

**Полезные команды:**
```bash
# Все события одного узла
cat /tmp/node-01/events.jsonl | jq .

# Все handshake за последнюю минуту
cat /tmp/node-*/events.jsonl | \
    jq 'select(.event=="handshake_done")' | tail -20

# Топ-10 узлов по числу RPC
cat /tmp/node-*/events.jsonl | \
    jq -r 'select(.event=="frame_sent") | .node' | \
    sort | uniq -c | sort -rn | head
```

## 6. Event stream — детальный справочник

**События (список полный):**

| Событие | Когда | Поля |
|---------|-------|------|
| `identity_loaded` | При старте узла | `node_id` |
| `server_started` | После `Listen()` | `addr` |
| `conn_accepted` | Входящее соединение | `peer`, `addr` |
| `conn_dialed` | Исходящее | `peer`, `addr` |
| `frame_sent` | Кадр отправлен | `type`, `size` |
| `frame_recv` | Кадр получен | `type`, `size` |
| `contact_added` | Новый контакт в routing | `peer` |
| `contact_updated` | Обновление контакта | `peer` |
| `contact_removed` | Удаление (expire) | `peer` |
| `bootstrap_seed` | Bootstrap-подключение | `addr` |
| `lookup_start` | Начало lookup | `target` |
| `lookup_iter` | Итерация lookup | `iter`, `queried` |
| `lookup_done` | Конец lookup | `found`, `duration_ms` |
| `store_start` | Начало STORE | `key` |
| `store_accepted` | Хранитель принял | `peer` |
| `store_rejected` | Хранитель отказал | `peer`, `reason` |
| `store_done` | STORE завершён | `replicas` |
| `findvalue_start` | Начало FIND_VALUE | `key` |
| `findvalue_found` | Нашли значение | `peer` |
| `findvalue_notfound` | Не нашли | — |
| `handshake_start` | Начало AKE | `peer` |
| `handshake_done` | AKE успешен | `session_id` |
| `handshake_failed` | AKE провален | `reason` |
| `expire` | Фоновая очистка | `count` |
| `republish` | Re-publish записей | `count` |
| `publish_start` | Начало publish | `node_id` |
| `publish` | Запись опубликована | `key` |
| `publish_done` | Publish завершён | `replicas` |

## 7. Типичные сценарии

### 7.1. Полный эксперимент с графиками (обычный прогон)

```bash
cd ~/p2pnode
rm -rf /tmp/p2pnode-demo-* metrics/collected metrics/lookups visualization
WITH_COMPARE=1 ./scripts/run_experiment.sh 15 20
xdg-open report.html
```

**Время:** ~2 минуты. **Что смотреть:** секции «Обзор»,
«Невырожденность», «Граф сети», «Lookup-агрегаты», «Star vs Ring».

### 7.2. Только DHT-проверка (быстро)

```bash
./scripts/run_star.sh 15
sleep 15
./scripts/collect_routing.sh
./scripts/lookup_batch.sh
./scripts/check_ne_degenerate.sh
./scripts/stop_all.sh
```

**Время:** ~30 сек. **Что смотреть:** вывод в консоль.

### 7.3. Проверка E2E-шифрования (pcap)

```bash
# Один раз: даём tcpdump права
sudo setcap cap_net_raw,cap_net_admin+eip $(which tcpdump)

./scripts/capture_and_verify.sh
cat /tmp/p2pnode-demo-capture/verify.json
```

**Что смотреть:** `plaintext_in_pcap: false`, `protocol_keywords_in_pcap: 0`.

### 7.4. Демонстрация туннеля с отказами

```bash
./scripts/demo_tunnel.sh
./scripts/demo_tunnel_recovery.sh
./scripts/demo_tunnel_pool.sh
./scripts/demo_tunnel_pcap.sh
```

### 7.5. Сравнение bootstrap-схем

```bash
./scripts/compare_visual.sh 15 20
# Смотреть: visualization/star-vs-ring/summary.json
```

### 7.6. Сравнение форматов

```bash
./scripts/compare_serialization.sh 1000
# Смотреть: visualization/serialization.json
```

### 7.7. Просмотр событий в реальном времени

```bash
# Терминал 1: запустить узел
./bin/node -state-dir /tmp/n1 -listen-port 9001 -log-level DEBUG

# Терминал 2: следить за событиями
tail -f /tmp/n1/events.jsonl | jq -c '{ts, event, peer, data}'
```

### 7.8. Просмотр графа в Graphviz вручную

```bash
./scripts/visualize.sh metrics/collected out/my-graph "N=15"
# Открыть out/my-graph.png (Linux: xdg-open)
```

## 8. Что планируется

### 8.1. Real-time Web «птичий полёт»

**Идея:** WebSocket + D3.js с live-обновлением.

**Компоненты:**
- `cmd/collector` — читает `events.jsonl` всех узлов, объединяет по
  timestamp, раздаёт через WebSocket.
- HTML-страница с D3.js, обновляется в реальном времени.
- Анимация RPC: летящие точки между узлами.
- Клик на узел — routing-таблица.
- Timeline: слайдер для прокрутки истории.

### 8.2. Секция «Отказ ретранслятора»

Показ двух туннелей рядом: сломанный (relay_dead) и восстановленный
(rebuild_ok). Граф + timeline.

**Данные:** из `demo_tunnel_recovery.sh` и `demo_tunnel_pool.sh`.

### 8.3. Секция «Отказ seed'а»

Две таблицы рядом: baseline lookup'ов (до kill) и after (после kill).
Бейдж OK/FAIL: RPC ≥ 2, target найден.

### 8.4. Секция «Отказ хранителя»

Сохранение двух routing-снапшотов (до/после отказа). Показ:
хранители [A,B,C] → A убит → B, C держат запись. FIND_VALUE находит.

### 8.5. Секция «Передача файла»

Таблица блоков: id, size, acked, sha256. Прогресс-бар. Финальный SHA
+ бейдж «verified».

### 8.6. k-buckets heatmap

Матрица «узел × bucket index → число контактов». Компактно.

### 8.7. Lookup path visualization

Для выбранного lookup — граф: initiator → first queried → ... → target.
