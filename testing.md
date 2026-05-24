# Testing

Проєкт покритий трьома рівнями тестів:

- Unit-тести: перевіряють ізольовану бізнес-логіку, валідацію, retry-логіку, middleware, worker-и та адаптери без запуску зовнішньої інфраструктури.
- Інтеграційні тести: мають префікс `TestIntegration_` і перевіряють HTTP/gRPC Gateway, Redis, PostgreSQL, migrations та repository layer з реальними залежностями через Docker/Testcontainers.
- E2E-тести з Playwright: відкривають сторінку `cmd/server/web/index.html` у браузері та імітують дії користувача через page object.

## Вимоги

На машині достатньо мати:

- `git`
- `docker`
- Go версії з `go.mod`

Згенеровані protobuf/gRPC файли лежать у `gen/`, але ця директорія не комітиться у git. Перед запуском тестів з чистого клонування потрібно згенерувати їх командою:

```bash
go run github.com/bufbuild/buf/cmd/buf@v1.65.0 generate
```

E2E-тести використовують Chromium через Playwright. Перед першим локальним запуском E2E потрібно встановити браузер:

```bash
go run github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 install chromium
```

Інтеграційні тести самі піднімають потрібну інфраструктуру з нуля в Docker через Testcontainers. Локально не потрібно вручну запускати PostgreSQL, Redis чи інші сервіси.

## Чому unit-тести можуть здаватись повільними

Unit-тести в цьому проєкті самі по собі виконуються швидко: більшість test case-ів не чекають на мережу, Docker або зовнішню інфраструктуру. Основний час витрачається не на assert-и, а на запуск Go test binary для різних пакетів і на компіляцію їхніх dependency graph-ів.

Є кілька речей, які сильно впливають на час запуску:

- `-race` інструментує весь test binary і може збільшувати час запуску в кілька разів. Це корисна перевірка для unit-тестів у CI або для окремого debug-запуску, але для щоденного локального feedback loop вона занадто дорога.
- `-count=1` вимикає кеш Go test. Це правильно для CI, щоб кожен run був чистим, але локально краще дозволити Go використовувати кеш.
- `./...` проходить по всіх пакетах. Навіть якщо інтеграційні та E2E тести не запускаються без build tag-ів, Go все одно збирає unit-test binary для пакетів, які імпортують важкі залежності на кшталт gRPC, grpc-gateway, Prometheus/OpenTelemetry, pgx або Redis.

Практичне правило:

- локально для швидкої перевірки використовувати `go test ./...`;
- якщо build tag `integration` увімкнений, unit-тести відсікаються через `-skip '^TestIntegration_'`;
- integration-тести запускаються через `-run '^TestIntegration_'`;
- E2E тести запускаються окремо з build tag `e2e`.

## Naming Convention

Інтеграційні тести повинні одночасно виконувати дві умови:

- файл має назву `*_integration_test.go`;
- exported test function починається з `TestIntegration_`.

Приклад:

```go
func TestIntegration_ReadRepository(t *testing.T) {
	// ...
}
```

Не використовуй назви на кшталт `TestReadRepository_Integration`: CI і локальні команди фільтрують integration-тести по префіксу `TestIntegration_`.

## Запуск усіх тестів

Перед запуском переконайся, що protobuf/gRPC файли згенеровані, а Chromium для Playwright встановлений.

```bash
go test -skip '^TestIntegration_' ./... && go test -count=1 -tags=integration -run '^TestIntegration_' ./... && go test -count=1 -tags=e2e -run '^TestIndexPageSubscriptionFlow$' ./cmd/server
```

## Unit-Тести

Швидкий локальний запуск:

```bash
go test ./...
```

Ця команда запускає тести без build tag `integration`, тому Docker-залежності не стартують.

Якщо запускаєш unit-тести у команді, де build tag `integration` може бути увімкнений, явно відсікай integration-тести:

```bash
go test -skip '^TestIntegration_' ./...
```

Повна CI-перевірка unit-тестів:

```bash
go test -race -count=1 -skip '^TestIntegration_' ./...
```

Локально `-race` краще не використовувати для кожного запуску. Він потрібен для пошуку data race-ів, але значно уповільнює тестування. `-count=1` також не потрібен у звичайному локальному циклі, бо він вимикає кеш Go test.

## Інтеграційні Тести

Build tag `integration` підключає файли `*_integration_test.go`, а `-run '^TestIntegration_'` запускає тільки integration entrypoints. Це дозволяє використовувати простий `./...` без довгого списку пакетів.

Повний запуск integration-тестів:

```bash
go test -count=1 -tags=integration -run '^TestIntegration_' ./...
```

Ці тести перевіряють інтеграцію з HTTP gateway, metrics endpoint, Redis, PostgreSQL, migrations і repository layer. Усе, що потрібно для тестів, піднімається автоматично в Docker.

За замовчуванням integration-тести не запускаються з `-race`. Race detector змінює timing і сильно уповільнює процес, тому Docker/network тести можуть стати більш timeout-sensitive і flaky.

## E2E-Тести Playwright

```bash
go test -count=1 -tags=e2e -run '^TestIndexPageSubscriptionFlow$' ./cmd/server
```

E2E-тест запускає реальний test server, відкриває сторінку в Chromium, заповнює форму підписки, підтверджує підписку через API token і перевіряє результат у UI.

## CI

Для CI потрібно мати окремі пайплайни для кожного виду тестування:

- Unit tests: `go test -race -count=1 -skip '^TestIntegration_' ./...`
- Integration tests: `go test -count=1 -tags=integration -run '^TestIntegration_' ./...`
- Playwright E2E tests: `go run github.com/playwright-community/playwright-go/cmd/playwright@v0.5700.1 install --with-deps chromium && go test -count=1 -tags=e2e -run '^TestIndexPageSubscriptionFlow$' ./cmd/server`

У CI Docker має бути доступний, бо інтеграційні та E2E тести використовують Testcontainers.
