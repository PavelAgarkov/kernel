# service-pkg

Набор утилит и мини‑фреймворков для сервисов на Go: аккуратный каркас приложения (graceful shutdown, обработка сигналов, рекавери), лидер‑элекция через Redis, планировщик задач, барьер готовности, обёртки для PostgreSQL и ClickHouse, HTTP (chi) и gRPC серверы с полезными мидлварами, логирование на базе zap, простые утилиты.

> Цель — ускорить запуск продуктивных сервисов: «сшить» инфраструктурные куски в согласованный рантайм с безопасной остановкой и понятными контрактами.

---

## Содержание
- [Возможности](#возможности)
- [Установка](#установка)
- [Требования](#требования)
- [Быстрый старт](#быстрый-старт)
- [Архитектура пакетов](#архитектура-пакетов)
    - [application](#application)
    - [watchdog (leader election)](#watchdog-leader-election)
    - [scheduler](#scheduler)
    - [readiness_barrier](#readiness_barrier)
    - [database/postgres](#databasepostgres)
    - [database/clickhouse](#databaseclickhouse)
    - [locker (Redis‑lock)](#locker-redislock)
    - [server/http (chi)](#serverhttp-chi)
    - [server/grpc](#servergrpc)
    - [logger](#logger)
    - [utils](#utils)
- [Рекомендации по эксплуатации](#рекомендации-по-эксплуатации)
- [Дорожная карта](#дорожная-карта)
- [Лицензия](#лицензия)

---

## Возможности
- **Единая точка входа приложения**: управление сигналами ОС (SIGTERM/SIGINT/SIGQUIT), список shutdown‑хуков с приоритетами, рекавери паник.
- **Лидер‑элекция** (Redis): надёжный цикл захвата/продления блокировки, уведомления о потере/получении лидерства.
- **Планировщик задач**: периодические job’ы c rate‑лимитом, дедлайнами и двумя режимами остановки (немедленная/мягкая).
- **Барьер готовности**: переключение ready/not‑ready по сигналам, безопасный жизненный цикл.
- **PostgreSQL (pgxpool)**: обёртка с явной настройкой Min/MaxConns, TTL/idle, health‑check, application_name.
- **ClickHouse**: подключение с LZ4, политика `NeedReconnect/NeedWait` по ошибкам, безопасный reconnect.
- **HTTP (chi)**: мидлвары для логов, X‑Correlation‑ID, recover; аккуратный graceful shutdown.
- **gRPC**: сервер + interceptors (panic → Internal, лимит размера ответа, таймауты), reflection по флагу.
- **Логирование (zap)**: унифицированные `WriteInfoLog/WriteWarnLog/WriteErrorLog/WriteFatalLog`.

## Установка
```bash
go get github.com/PavelAgarkov/service-pkg@latest
```

## Требования
- Go **1.23+** (модуль объявлен как `go 1.23.1`)
- Redis (для лидер‑элекции), PostgreSQL/ClickHouse — при использовании соответствующих пакетов

---

## Быстрый старт
Ниже — минимальный каркас сервиса на базе **application**, **server**, **scheduler** и **watchdog**.

```go
package main

import (
    "context"
    "time"
    "net/http"

    apppkg "github.com/PavelAgarkov/service-pkg/application"
    logtypes "github.com/PavelAgarkov/service-pkg/logger"
    logger "github.com/PavelAgarkov/service-pkg/logger/zap_engine"
    "github.com/PavelAgarkov/service-pkg/scheduler"
    "github.com/PavelAgarkov/service-pkg/server"
    "github.com/PavelAgarkov/service-pkg/watchdog"
    "github.com/PavelAgarkov/service-pkg/locker"

    "github.com/go-redis/redis/v8"
)

func main() {
    // базовый ctx со стопом
    ctx, cancel := context.WithCancel(context.Background())

    // инициализация рантайма приложения (ядро)
    app := apppkg.NewApp(ctx, /*GOMAXPROCS*/ 0, /*GC%*/ 100)
    defer app.FlushLogger()
    defer app.RegisterRecovers()()

    // HTTP сервер на chi (опционально)
    httpStop := server.CreateHTTPChiServer(func(s *server.HTTPServerChi) {
        s.Router.Use(server.RecoverChiMiddleware, server.LoggingChiMiddleware)
        s.Router.Get("/health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
    }, ":8080")
    app.RegisterShutdown("http", httpStop, apppkg.HighPriority)

    // Планировщик с одной задачей
    sch := scheduler.NewJobScheduler(1)
    _ = sch.Add(scheduler.JobConfiguration{
        Name:     "heartbeat",
        Tick:     5 * time.Second,
        Deadline: 2 * time.Second,
        StopMode: scheduler.StopImmediate,
        Func: func(ctx context.Context) error {
            logger.WriteInfoLog(ctx, &logtypes.LogEntry{Msg: "tick"})
            return nil
        },
    })
    app.RegisterShutdown("scheduler", scheduler.NewTaskSupervisor([]scheduler.JobSchedulerInterface{sch}).Stop(), apppkg.MediumPriority)

    // Лидер‑элекция (при необходимости)
    rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
    lck := locker.NewLocker(rdb)
    wd := watchdog.NewRedisWatchdogLeader(ctx, lck)

    app.RegisterWatchdogsLeadership(&apppkg.LeaderSupervisor{
        Watchdog:       wd,
        Watcher:        wd.Elect(watchdog.Config{ElectionName: watchdog.Cron}),
        SupervisorName: "cron-supervisor",
        Start: func() { sch.Start(ctx)() },
        Stop:  func() { sch.Stop()() },
    })

    // обработчик сигналов ОС
    app.Start(cancel)

    // блокировка до остановки
    app.Run()
}
```

---

## Архитектура пакетов

### application
Каркас приложения: обработка сигналов ОС, приоритезированные shutdown‑хуки, рекавери, и «надсмотрщики» над задачами, зависящими от лидер‑элекции.

**Ключевые сущности:**
- `App` — ядро;
    - `RegisterShutdown(name string, fn func(), priority int)` — регистрирует действие на остановку. Чем **меньше** число, тем **выше** приоритет (выполняется раньше).
    - `Start(cancel context.CancelFunc)` — подписка на SIGTERM/SIGINT/SIGQUIT; по сигналу вызывает `cancel()`.
    - `Run()` — ждёт завершения базового контекста.
    - `RegisterWatchdogsLeadership(*LeaderSupervisor)` — связывает лидер‑элекцию с `Start/Stop` функций над подсистемами.
- `LeaderSupervisor` — привязывает `watchdog` к конкретной подсистеме: при `TakenAcquire` вызывает `Start()`, при `LostAcquire` — `Stop()`.

> Используется односвязный список для shutdown‑хуков, упорядоченных по приоритетам.

### watchdog (leader election)
Лидер‑элекция на Redis‑блокировке:
- `RedisWatchdogLeader` периодически пытается захватить/продлить `key`, шлёт события в канал наблюдателю.
- События: `TakenAcquire` (стали лидером), `LostAcquire` (потеряли лидерство).

**Пример:**
```go
wd := watchdog.NewRedisWatchdogLeader(ctx, locker)
ch := wd.Elect(watchdog.Config{ElectionName: "my-job", Expiration: 30*time.Second})
for ev := range ch { /* переключаем подсистемы */ }
```

### scheduler
Планировщик периодических задач с rate‑лимитом и дедлайнами.

- `Add(JobConfiguration)` до `Start`.
- `Start(ctx)()` запускает задачи (каждая — в собственной горутине через Ticker).
- `Stop()()` останавливает: отменяет контексты, гасит тикеры и ждёт `WaitGroup`.
- `StopMode`:
    - `StopImmediate` — задача наследует общий `ctx`; при остановке мгновенно отменяется.
    - `StopGraceful` — задача получает `context.Background()` с таймаутом, чтобы корректно доработать цикл.

### readiness_barrier
Лёгкий флаг готовности сервиса:
- `Start/Stop` — безопасный запуск/остановка фонового слушателя сигналов без гонок.
- `SendSignalCtx(ctx, ReadySignalToggle|NotReadySignalToggle)` — выставить состояние.
- `IsReady()` — атомарное чтение состояния.

### database/postgres
Обёртка над `pgxpool.Pool` с продуманными настройками:
- `MaxConns/MinConns`, `MaxConnIdleTime`, `MaxConnLifetime` (+ `Jitter`), `HealthCheckPeriod`, `ConnectTimeout`.
- `application_name` в `RuntimeParams` для удобной диагностики.

```go
cfg := postgres.Configs{ Host: "db", Port: "5432", Username: "u", Password: "p", Database: "app",
    SSLMode: "disable", MaxOpenedConnections: 20,
    ConnectionMaxIdleTime: 5*time.Minute, ConnectionMaxLifeTime: 1*time.Hour,
    HealthCheckPeriod: 15*time.Second, ConnectTimeout: 1*time.Second,
}
pg := postgres.NewPostgresConnection(ctx, cfg)
defer pg.Stop()
pool := pg.GetPool()
```

### database/clickhouse
Подключение и политика обработки ошибок для CH:
- Настройка пула (`MaxOpen/Idle`, TTL, Lifetime), LZ4, `DialTimeout`.
- `NeedReconnect(error) (bool, *ch.Exception)` — классификация ошибок, при которых разумно пересоздавать соединение.
- `NeedWait(error) (bool, time.Duration, *ch.Exception)` — когда полезна задержка (квоты, «мало живых реплик», перегруз).
- Безопасный `Reconnect(ctx, newCfg)` с обменом `*sql.DB` под мьютексом.

### locker (Redis‑lock)
Простейшая распределённая блокировка на Lua‑скриптах `SET NX PX`/`DEL`/`PEXPIRE`:
- `Lock(ctx, key, value, TTL)`
- `Unlock(ctx, key, value)`
- `ExtendLockTTL(ctx, key, value, TTL)`

### server/http (chi)
Упаковка для быстрого старта HTTP‑сервера:
- `CreateHTTPChiServer(routes, port, ...middleware) func()` возвращает **функцию остановки** (graceful 5s).
- Мидлвары: `RecoverChiMiddleware` (panic → 500), `LoggingChiMiddleware` (X‑Correlation‑ID + лог), `LoggerChiContextMiddleware`.

```go
stop := server.CreateHTTPChiServer(func(s *server.HTTPServerChi){
    s.Router.Use(server.RecoverChiMiddleware, server.LoggingChiMiddleware)
    s.Router.Get("/ping", func(w http.ResponseWriter, r *http.Request){ w.Write([]byte("pong")) })
}, ":8080")
// ...
stop() // при остановке приложения
```

### server/grpc
gRPC сервер с полезными перехватчиками:
- `CreateGRPCServer(ctx, register, Configs{Port, Network, Reflection}, opts...) func()` — возвращает **shutdown**.
- Interceptors:
    - `PanicHandler` → код `Internal` + стек.
    - `EnforceMaxSendSize(maxBytes)` — жёсткий лимит ответа (избегает утечек при гигантских ответах).
    - `TimeoutUnaryInterceptor(d)` — таймаут на запрос.

```go
shutdown := server.CreateGRPCServer(ctx, func(s *grpc.Server){
    // pb.RegisterYourServiceServer(s, impl)
}, server.Configs{Port: ":9090", Network: "tcp", Reflection: true},
   grpc.ChainUnaryInterceptor(
       server.TimeoutUnaryInterceptor(3*time.Second),
   ),
)
// ...
shutdown()
```

### logger
Две части:
- `logger` — типы записей (`LogEntry` и др.).
- `logger/zap_engine` — функции `WriteInfoLog/WriteWarnLog/WriteErrorLog/WriteFatalLog`, `FlushLogs()`.

```go
import (
  logtypes "github.com/PavelAgarkov/service-pkg/logger"
  logger   "github.com/PavelAgarkov/service-pkg/logger/zap_engine"
)
logger.WriteInfoLog(ctx, &logtypes.LogEntry{Msg: "hello", Component: "app", Method: "main"})
```

### utils
Мелкие утилиты: безопасный запуск горутин с recover (`GoRecover`), контексты с тайм‑аутом без дедлайна, хелперы по слайсам и пр.

---

## Рекомендации по эксплуатации
- **Приоритеты shutdown**: сначала закрывайте внешние интерфейсы (HTTP/gRPC), затем фоновые воркеры и планировщики, потом соединения с БД/кэшем.
- **Лидер‑элекция**: готовьтесь к «морганию» сети/Redis — `LeaderSupervisor` уже делает Start/Stop идемпотентно.
- **gRPC ответы**: используйте `EnforceMaxSendSize` чуть ниже максимального размера ответов (например, `0.9 * out_grpc_body_size`).
- **PostgreSQL/CH пулы**: подбирайте `MinConns` ≈ `MaxConns/4`, TTL/idle — исходя из нагрузки и политики сервера.
- **Планировщик**: критичные задачи — `StopImmediate` (чтобы не тянуть останов), долгие — `StopGraceful` с разумным `Deadline`.

---

## Дорожная карта
- [ ] Метрики Prometheus: пулы БД, планировщик, лидеры, HTTP/gRPC.
- [ ] Пробы liveness/readiness HTTP‑эндпоинтами.
- [ ] Интеграция с OpenTelemetry (tracing/logs).
- [ ] Тестовые double’ы и пример сервиса (boilerplate).

---

## Лицензия
См. файл **LICENSE** в репозитории.