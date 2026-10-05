# Развёртывание p2pnode в Docker

Документ описывает контейнеризацию узла, 4 bootstrap-схемы, скрипты
управления, эксперименты масштаба N=15…21 и полный цикл воспроизведения
результатов этапа 6.

## 1. Что это и зачем

Узел `p2pnode` — один бинарник. Для экспериментов на N=12–21 узлов нужны
**раздельные** identity, порты, хранилища и журналы. Локальный запуск
(через `bin/node &` в цикле) плохо масштабируется: один процесс на узел,
race conditions, сложно гарантировать чистоту state между прогонами.

**Docker Compose** решает три задачи:

1. **Изоляция** — каждый узел в своём контейнере, свои volume'ы.
2. **Сеть** — DNS-имена `node-01 … node-21` внутри bridge-сети.
3. **Воспроизводимость** — одна команда запускает стенд любого размера
   и любой схемы.

## 2. Требования

- **Docker** ≥ 20.10 (с `docker compose` v2).
- **Go 1.26** — только для локальной сборки (в контейнере — свой Go).
- **bash**, `sha256sum`, `mktemp` — стандартные утилиты Linux.
- **~2 ГБ RAM** на N=21 (21 × ~50 МБ).
- **~500 МБ** диска на state/logs/metrics (N=21).

Проверка:

```bash
docker --version
docker compose version
```

## 3. Быстрый старт

### 3.1. Одна команда — полный эксперимент

```bash
cd ~/p2pnode

# Запустить 15 узлов в star-схеме, подождать 30с сходимости,
# собрать routing, прогнать 30 lookup'ов, проверить невырожденность.
./scripts/run_experiment_docker.sh 15 star 30
```

**Что происходит:**
1. `docker_down.sh --clean` — стоп предыдущего стенда, очистка volume'ов.
2. `docker build -t p2pnode:latest .` — сборка образа.
3. `docker_gen_compose.sh 15 star` — генерация `docker-compose.yml`.
4. `docker compose up -d` — запуск 15 контейнеров.
5. `sleep 30` — сходимость DHT.
6. `docker_collect.sh` — копирование routing-снапшотов.
7. `docker_lookup_batch.sh 15 30` — 30 контрольных lookup'ов.
8. `check_ne_degenerate.sh` — проверка невырожденности.
9. `generate_report.sh` — HTML-отчёт.

**Результат:**
```
metrics/collected/routing-*.json    — 15 routing-снапшотов
metrics/lookups/lookup-*.json       — 30 lookup-логов
report.html                          — интерактивный отчёт
```

### 3.2. Пошаговый запуск

```bash
# 1. Собрать образ
docker build -t p2pnode:latest .

# 2. Сгенерировать compose для N=15 в star-схеме
./scripts/docker_gen_compose.sh 15 star

# 3. Поднять стенд
./scripts/docker_up.sh 15 star

# 4. Ждать сходимости
sleep 30

# 5. Собрать routing
./scripts/docker_collect.sh

# 6. Прогнать 30 lookup'ов
./scripts/docker_lookup_batch.sh 15 30

# 7. Проверить невырожденность
./scripts/check_ne_degenerate.sh

# 8. Остановить
./scripts/docker_down.sh --clean
```

## 4. Bootstrap-схемы

### 4.1. `star` — все на seed

```
       node-02
       node-03
node-01 ─ node-04
       node-05
       ...
```

- `node-01` — **seed** (запускается первым, без bootstrap).
- Все остальные подключаются **к seed'у**: `-bootstrap node-01:9001`.
- **Плюсы:** быстрая сходимость, простой.
- **Минусы:** seed — точка отказа.

```bash
./scripts/docker_up.sh 15 star
```

### 4.2. `ring` — каждый на предыдущего

```
node-01 → node-02 → node-03 → ... → node-15 → node-01
```

- `node-01` — первый, без bootstrap.
- `node-i` подключается к `node-(i-1)`.
- **Плюсы:** нет единой точки отказа.
- **Минусы:** медленная сходимость (нет общего seed'а).

```bash
./scripts/docker_up.sh 15 ring
```

### 4.3. `tree` — бинарное дерево

```
        node-01
        /     \
    node-02   node-03
    /   \      /   \
  n-04 n-05  n-06 n-07
  ...
```

- `node-i` подключается к **родителю** `node-(i/2)`.
- **Плюсы:** иерархическая топология, хорошая для тестов частичного отказа.
- **Минусы:** глубина `log2(N)` — для N=15 это 4.

```bash
./scripts/docker_up.sh 15 tree
```

### 4.4. `multiseed` — 3 seed'а

```
       node-01   node-02   node-03     ← 3 seed'а
            \      |      /
             \     |     /
              все остальные
```

- Первые **3 узла** — seed'ы.
- Остальные подключаются **ко всем трём**:
  `-bootstrap node-01:9001,node-02:9002,node-03:9003`.
- **Плюсы:** отказ одного seed'а не блокирует сеть.
- **Минусы:** 3 точки входа.

```bash
./scripts/docker_up.sh 15 multiseed
```

## 5. Архитектура Docker-стенда

### 5.1. Схема сети

```
┌─────────────────────────────────────────────────────┐
│  Host (Linux)                                       │
│  ┌───────────────────────────────────────────────┐  │
│  │  Docker network: p2pnode_p2pnet (bridge)      │  │
│  │                                               │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐     │  │
│  │  │ node-01  │  │ node-02  │  │ node-15  │     │  │
│  │  │ :9001    │  │ :9002    │  │ :9015    │     │  │
│  │  │ ADVERT:  │  │ ADVERT:  │  │ ADVERT:  │     │  │
│  │  │ node-01  │  │ node-02  │  │ node-15  │     │  │
│  │  └────┬─────┘  └────┬─────┘  └────┬─────┘     │  │
│  │       │             │             │           │  │
│  └───────┼─────────────┼─────────────┼───────────┘  │
│          │             │             │              │
│  ┌───────▼──────┐  ┌───▼──────┐  ┌───▼──────┐      │
│  │ port 9001    │  │ port 9002│  │ port 9015│       │
│  │ (publish)    │  │          │  │          │       │
│  └──────────────┘  └──────────┘  └──────────┘       │
│                                                     │
│  Host ports 9001–9015 → container ports 9001–9015   │
└─────────────────────────────────────────────────────┘
```

**Ключевое:**
- `ADVERTISE_HOST=node-XX` — DNS-имя внутри Docker-сети.
- `LISTEN_HOST=0.0.0.0` — слушаем все интерфейсы.
- `ports: "90XX:90XX"` — проброс для **доступа с хоста** (lookup_batch
  запускает `bin/node` на хосте с bootstrap на `127.0.0.1:9001`).
- Volume'ы: `state-docker/node-XX:/state`,
  `logs-docker/node-XX:/logs`, `metrics-docker/node-XX:/metrics`.

### 5.2. Образ

**Multi-stage `Dockerfile`:**

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/node ./cmd/node

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata bash netcat-openbsd
COPY --from=build /out/node /usr/local/bin/node
ENV NODE_STATE_DIR=/state LISTEN_HOST=0.0.0.0 LISTEN_PORT=9001 LOG_LEVEL=INFO
RUN mkdir -p /state /logs /metrics
WORKDIR /
ENTRYPOINT ["/usr/local/bin/node"]
```

**Размер образа:** ~25 МБ (alpine + статический бинарь).

**ENTRYPOINT:** `/usr/local/bin/node`. CLI-аргументы дописываются **после**
entrypoint — можно передавать флаги через `docker compose run`.

### 5.3. Генератор compose

`scripts/docker_gen_compose.sh <N> <scheme>` создаёт `docker-compose.yml`
с N сервисами.

**Ключевые моменты:**

1. **`user: "$(id -u):$(id -g)"`** — контейнер работает под **хост-UID**,
   чтобы файлы в volume'ах **не принадлежали root**.

2. **`ADVERTISE_HOST=node-XX`** — узел анонсирует **своё** DNS-имя, а
   не `127.0.0.1`. Без этого другие узлы не смогут к нему подключиться.

3. **`depends_on`** — генерируется **только если есть элементы**.
   Пустой `depends_on:` **невалиден** в YAML.

4. **`PUBLISH_SELF=true` + `PUBLISH_WAIT_MS=15000`** — только для
   seed'а (`node-01`). Это для E6-6.1 (отказ хранителя).

5. **Для `multiseed`** — seed'ы (`node-01` … `node-03`) **не зависят**
   ни от кого, остальные зависят от **всех трёх**, но **исключая себя**.

### 5.4. Отдельная сеть

Сеть `p2pnode_p2pnet` — **bridge** с автоматическим DNS. Внутри неё:

```bash
# Из контейнера node-03:
getent hosts node-02
# → 172.19.0.6  node-02
```

**DNS-резолвинг** работает для всех сервисов compose.

## 6. Скрипты управления

| Скрипт | Назначение |
|--------|------------|
| `docker_gen_compose.sh N scheme` | Генерирует `docker-compose.yml` |
| `docker_up.sh N scheme [--clean]` | build + generate + up + wait |
| `docker_down.sh [--clean]` | down + (опционально) rm volume'ов |
| `docker_collect.sh` | Копирует `metrics-docker/node-XX/routing-*.json` в `metrics/collected/` |
| `docker_lookup_batch.sh N count` | 30 lookup'ов через `docker compose run` |
| `docker_check_lookup.sh init tgt` | Один lookup с DEBUG-логами |
| `run_experiment_docker.sh N scheme wait` | Полный эксперимент |
| `demo_seed_down.sh N` | работа без seed'а |
| `demo_failures.sh N` | 3 сценария отказа |
| `compare_4schemes_docker.sh N wait` | сравнение 4 схем |
| `run_experiment_n21.sh N wait` | N=21 |

### 6.1. `docker_up.sh N scheme [--clean]`

```bash
./scripts/docker_up.sh 15 star --clean
```

**Что делает:**
1. Останавливает предыдущий стенд (если есть).
2. `--clean` → удаляет `state-docker/`, `logs-docker/`,
   `metrics-docker/` (через `sudo`, если нужно).
3. Собирает образ `p2pnode:latest`.
4. Генерирует `docker-compose.yml`.
5. Создаёт volume-каталоги.
6. `docker compose up -d`.
7. Ждёт до 60 сек, пока **все N контейнеров** не будут `Up`.

### 6.2. `docker_lookup_batch.sh N count`

```bash
./scripts/docker_lookup_batch.sh 15 30
```

**Что делает:**
1. Очищает `tmp-lookup-state/` — **свежая identity** для каждого lookup'а.
2. Читает `state-docker/node-XX/identity.pub` — вычисляет NodeID.
3. Генерирует пары `(initiator, target)` — **далёкие по XOR**.
4. Для каждой пары:
   - `docker compose run --rm node-XX` — **новый контейнер**.
   - `-state-dir /state` (собственная identity).
   - `-bootstrap node-01:9001` (или env `BOOTSTRAP`).
   - `-lookup-target <hex>`.
   - `-dump-routing` → выход.
5. Переименовывает `lookup-<short>-<ts>.json` в
   `lookup-<i>-to-<j>-<short>.json`.

**Env override:**

```bash
# Для E6-5: после kill seed'а — несколько других узлов
BOOTSTRAP="node-02:9002,node-03:9003,node-04:9004" \
    ./scripts/docker_lookup_batch.sh 15 30
```

### 6.3. `docker_collect.sh`

Копирует **последние** routing-снапшоты из `metrics-docker/node-XX/`
в `metrics/collected/`. Lookup-файлы **не трогает** — они уже в
`metrics/lookups/`.

## 7. Демонстрационные сценарии этапа 6

### 7.1. N=15 одной командой

```bash
./scripts/docker_up.sh 15 star
sleep 30
docker compose ps    # 15 контейнеров Up
```

### 7.2. star и ring

```bash
./scripts/docker_up.sh 15 star
./scripts/docker_down.sh --clean

./scripts/docker_up.sh 15 ring
./scripts/docker_down.sh --clean
```

### 7.3. Невырожденность

```bash
./scripts/docker_up.sh 15 star
sleep 30
./scripts/docker_collect.sh
./scripts/check_ne_degenerate.sh
```

**Ожидаемо:**
```
[check] criterion 1 (>=80% < N-1):        True
[check] criterion 1b (≤1 full registry):   True
[check] RESULT: NON-DEGENERATE
```

### 7.4. 30 lookup'ов

```bash
./scripts/docker_lookup_batch.sh 15 30
./scripts/check_ne_degenerate.sh
```

**Ожидаемо:**
```
[check] lookup'ов всего:               30
[check] с промежуточным узлом (RPC≥2): 30
[check] criterion 3 (≥80% w/ interm):  True
```

### 7.5. Работа без seed'а

```bash
./scripts/demo_seed_down.sh 15
```

**Ожидаемо:**
```
[seed-down] OK: after/before = 0.90 (>=0.3) — E6-5 satisfied
```

**Что делает:**
1. `docker_up 15 star` + 30 сек сходимости.
2. Baseline 30 lookup'ов (все seed'ы живы).
3. `docker compose stop node-01`.
4. Ждёт 45 сек — routing чистится от мёртвого seed'а.
5. After 30 lookup'ов с `BOOTSTRAP=node-02,03,04`.
6. Сравнение: `found_after / found_before ≥ 0.3`.

### 7.6. 3 сценария отказа

```bash
./scripts/demo_failures.sh 7
```

**Что проверяет:**
1. **Отказ хранителя** — STORE, kill хранителя, FIND_VALUE работает.
2. **Отказ кандидата lookup** — kill узла во время серии, lookup продолжает.
3. **Отказ ретранслятора** — kill активного relay, rebuild туннеля.

**Ожидаемо:**
```
[1] OK: FIND_VALUE работает после kill хранителя
[2] OK: lookup продолжает работать после kill кандидата
[3] OK: туннель восстановлен после kill ретранслятора
```

### 7.7. Сравнение 4 схем

```bash
./scripts/compare_4schemes_docker.sh 15 30
```

**Результат:** `visualization/bootstrap-4-docker/summary.json` с
метриками по star, ring, tree, multiseed.

### 7.8. N=21

```bash
./scripts/run_experiment_n21.sh 21 40
```

**Ожидаемо:**
```
[check] N = 21
[check] table sizes: min=3 max=12 mean=7.24
[check] nodes with < N-1 contacts: 21/21 (100.0%)
[check] criterion 1 (>=80% < N-1):        True
[check] criterion 1b (≤1 full registry):   True
[check] RESULT: NON-DEGENERATE
```

## 8. Диагностика

### 8.1. Контейнеры не поднимаются

```bash
docker compose ps
docker compose logs node-01
```

**Частые причины:**
- порт занят (`9001` уже используется);
- volume смонтирован с root-правами (`user:` не применился);
- старые контейнеры не удалены (`docker compose down -v`).

**Решение:**
```bash
docker compose down -v --remove-orphans
sudo rm -rf state-docker logs-docker metrics-docker
./scripts/docker_up.sh 15 star --clean
```

### 8.2. `docker compose exec` зависает

**Симптом:** команда висит после `Container Created`.

**Причина:** `docker compose run/exec` ждёт stdin.

**Решение:** добавить `</dev/null` или `-T` + явный `-listen-port`.

### 8.3. Lookup даёт `rpc=0`

**Симптом:** после kill seed'а — все lookup'ы `rpc=0`.

**Причина:** `node-02` не отвечает на `node-02:9002`.

**Проверка:**
```bash
docker compose ps node-02
timeout 2 bash -c 'cat < /dev/null > /dev/tcp/127.0.0.1/9002' && echo OK || echo FAIL
```

**Решение:** использовать **несколько seed'ов**:
```bash
BOOTSTRAP="node-02:9002,node-03:9003,node-04:9004" \
    ./scripts/docker_lookup_batch.sh 15 30
```

### 8.4. `depends_on must be a array`

**Причина:** пустой `depends_on:` в compose.

**Решение:** в `docker_gen_compose.sh` — обернуть блок в
`if [ -n "$DEPS" ]`.

### 8.5. `lookups: 60` вместо `30`

**Причина:** `docker_collect.sh` **дублирует** lookup-файлы.

**Решение:** в `docker_collect.sh` **убрать** блок копирования
lookup'ов — они уже в `metrics/lookups/`.

## 9. Параметры compose

Все параметры задаются **env-переменными** в `docker-compose.yml`.
CLI-флаги можно **добавить** через `docker compose run`.

| Параметр | Env | Default |
|----------|-----|---------|
| Каталог состояния | `NODE_STATE_DIR` | `/state` |
| Адрес прослушивания | `LISTEN_HOST` | `0.0.0.0` |
| Порт | `LISTEN_PORT` | `9000` |
| Анонсируемое имя | `ADVERTISE_HOST` | — |
| Bootstrap | `BOOTSTRAP_PEERS` | — |
| Размер k-bucket | `K_BUCKET_SIZE` | `4` |
| Параллелизм lookup | `ALPHA` | `3` |
| Уровень логов | `LOG_LEVEL` | `INFO` |
| Каталог метрик | `EXPORT_DIR` | `/metrics` |
| Интервал экспорта | `EXPORT_INTERVAL_MS` | `2000` |
| Публикация записи | `PUBLISH_SELF` | `false` |
| Пауза перед publish | `PUBLISH_WAIT_MS` | `3000` |
| TTL туннеля | `TUNNEL_TTL_SEC` | `300` |
| Размер пула | `TUNNEL_POOL_SIZE` | `3` |

## 10. Ограничения

1. **Один хост** — все контейнеры на одной машине. Для распределённого
   эксперимента нужен overlay-драйвер (Docker Swarm / Kubernetes).
2. **Все порты 9001–90XX проброшены** — упрощает доступ с хоста, но
   создаёт риск конфликта с локальными процессами.
3. **Volume'ы на хосте** — состояние сохраняется между запусками, что
   требует явного `--clean` для чистого эксперимента.
4. **DNS только внутри сети** — с хоста доступны только IP:порты.
5. **Docker build 25–30 секунд** — заметно для частых прогонов.
