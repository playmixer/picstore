# Метрики производительности PicStore

Для оценки эффективности навигации, кэширования и пагинации можно собирать следующие метрики.

## Счетчики

### Кэш навигации
- `navigation_cache_hit` – количество попаданий в кэш упорядоченных ID.
- `navigation_cache_miss` – количество промахов кэша (приходится выполнять SQL).
- `navigation_cache_size` – количество записей в кэше (сессий).

### Время ответа
- `navigation_context_duration_ms` – время формирования NavigationContext (от начала до конца).
- `sql_query_duration_ms` – время выполнения SQL-запросов.
- `redis_operation_duration_ms` – время операций с Redis.

### Использование предзагрузки
- `preloaded_images_total` – количество предзагруженных изображений.
- `preloaded_images_used` – количество предзагруженных изображений, которые были фактически использованы при навигации.

### Пагинация
- `pagination_page_requests` – количество запросов каждой страницы.
- `pagination_fallback_count` – количество случаев, когда пришлось использовать fallback-навигацию.

## Сбор метрик

### 1. Логирование
В коде Go уже присутствует логирование с уровнем debug. Включите DEBUG-режим в конфигурации, чтобы видеть детали.

Пример конфигурации в `config.go`:
```go
Debug bool `yaml:"debug"`
```

### 2. Prometheus
Если в будущем потребуется интеграция с Prometheus, можно добавить endpoint `/metrics` и использовать библиотеку `github.com/prometheus/client_golang`.

Пример счетчика:
```go
var navigationCacheHit = prometheus.NewCounter(
    prometheus.CounterOpts{
        Name: "navigation_cache_hit_total",
        Help: "Total number of navigation cache hits.",
    },
)
```

### 3. Redis-счетчики
В методах `GetNavigationContext` и `cacheOrderedImageIDs` можно инкрементировать ключи Redis:

```go
func incrementMetric(metric string) {
    ctx := context.Background()
    rdb := redisClient
    rdb.Incr(ctx, "metrics:"+metric)
}
```

## Визуализация
Собранные метрики можно визуализировать с помощью Grafana, подключившись к Redis или Prometheus.

## Текущая реализация
На данный момент метрики собираются только через логи (уровень debug). Для активации установите `DEBUG=true` в переменных окружения.

Логи включают:
- Время выполнения каждого этапа навигации.
- Размеры массивов prev/next.
- Факт попадания в кэш.

Пример лога:
```
[DEBUG] Navigation cache hit for session: abc123
[DEBUG] SQL query took 12ms
[DEBUG] Navigation context built with 5 prev, 5 next images
```

## Рекомендации по мониторингу
1. **Кэш**: следите за соотношением hit/miss. Если miss-rate высокий, возможно, нужно увеличить TTL или оптимизировать инвалидацию.
2. **Время ответа**: если `navigation_context_duration_ms` превышает 100 мс, проверьте индексы БД и нагрузку на Redis.
3. **Предзагрузка**: отслеживайте, сколько предзагруженных изображений действительно используются. Если мало, можно уменьшить размер окна.

## Дальнейшие улучшения
- Добавить экспорт метрик в формате OpenMetrics.
- Реализовать дашборд в административной панели.
- Настроить алерты на аномалии (например, резкий рост времени ответа).