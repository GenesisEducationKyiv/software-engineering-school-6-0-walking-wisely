# 1. Вимоги системи

## Current vs Target scope

Цей документ описує цільову production-архітектуру системи. Поточна реалізація є навчальним/ітераційним зрізом, у якому частина production-гарантій свідомо відкладена через обмеження часу.

Найважливіша різниця:

- **Current:** Scanner передає release notification email-повідомлення Sender-у через in-memory channel. Confirmation email надсилається API Server-ом напряму через Email Provider Integration. Обидва шляхи є best-effort і можуть втратити повідомлення при падінні процесу або довгому outage email-провайдера.
- **Target:** delivery pipeline для release notification emails має використовувати persistent email outbox або transactional outbox у PostgreSQL. Це потрібно для at-least-once delivery, durable retry, backoff і dead-letter статусів. Confirmation emails можуть пізніше перейти на той самий outbox, але це не є обов'язковою умовою першого production-oriented кроку.

C4 component diagram відображає **target-state** архітектуру з PostgreSQL email outbox для release notification emails. Детальна outbox-специфікація описана окремо в [email-outbox-spec.md](email-outbox-spec.md).

## Функціональні вимоги

1. Користувач може підписатися на сповіщення про нові релізи GitHub-репозиторію, вказавши email та репозиторій у форматі `owner/repo`.
2. Підписка активується лише після підтвердження email через одноразове посилання (double opt-in).
3. Користувач може відписатися від конкретного репозиторію через посилання в кожному сповіщенні — без логіну.
4. Користувач може переглянути перелік своїх активних підписок за email.
5. При появі нового релізу на GitHub усі підписники цього репозиторію отримують email-сповіщення.

## Бізнес-вимоги до API contract

Система має дотримуватися зовнішньо заданої структури публічних HTTP endpoint-ів:

- `POST /api/subscribe` — створення підписки.
- `GET /api/confirm/{token}` — підтвердження підписки через одноразове посилання з email.
- `GET /api/unsubscribe/{token}` — відписка через одноразове посилання з email.
- `GET /api/subscriptions?email={email}` — перегляд підписок за email.

Confirmation та unsubscribe flow мають працювати як link-based сценарії без логіну, пароля або додаткової взаємодії з боку користувача. Тому confirmation endpoint заданий саме як `GET /api/confirm/{token}`.

## Нефункціональні вимоги

### Надійність доставки

- Кожне сповіщення про реліз має бути доставлене at-least-once. Дублікат допустимий, втрата — ні.
- Тимчасова недоступність email-провайдера не повинна призводити до втрати сповіщень.

Це **target requirement**. Поточна in-memory реалізація не виконує цю вимогу повністю; вона прийнята як тимчасовий компроміс через обмеження часу та простоту реалізації.

### Затримка сповіщень

- Від появи релізу на GitHub до виявлення системою і початку delivery pipeline — **не більше одного polling interval, тобто близько 10 хвилин** за нормальних умов.
- Фактичне отримання email користувачем додатково залежить від Sender-а та email-провайдера; у normal path воно очікується одразу після виявлення, але не є строго гарантованим 10-хвилинним bound-ом у current-state архітектурі.

### Масштабованість

- Система має підтримувати >=10 000 активних підписок без деградації продуктивності.

### Доступність

- HTTP API: **99.9% uptime**.
- Недоступність GitHub або email-провайдера не повинна впливати на API підписок/відписок — система деградує gracefully.

### Ідемпотентність

- Повторний запит на підписку з тим самим email і репозиторієм не створює дублікат.
- Повторне підтвердження через той самий токен є безпечною операцією.

### Безпека

- Токени підтвердження та відписки мають бути криптографічно стійкими та не передбачуваними.
- Перегляд підписок виконується через поточний бізнес-флоу `GET /api/subscriptions?email={email}` без логіну.
- Публічні endpoints мають бути захищені від зловживань.

# 2. Оцінка навантаження

### Користувачі та трафік

- Активних підписок: 10K; середнє 2–3 підписки на користувача → ~4K користувачів
- Унікальних репозиторіїв під спостереженням: ~500–700 (при середньому 14–20 підписок/репо)
- API запити (підписка / підтвердження / відписка): ~20 RPS у пік
- Опитування GitHub Releases API (інтервал 10 хв): ~0.8–1.2 RPS → ~3K–4.2K req/год
- Ліміт authenticated GitHub API: 5K req/год на один token; повний scan кожні 10 хвилин теоретично дозволяє до ~833 repo, але цільові 500–700 repo залишають operational margin для валідації репозиторіїв, retry та secondary rate limits.
- Email-сповіщень: залежить від частоти релізів — важко передбачити

### Дані

- Запис користувача: ~200 bytes (email + timestamps)
- Запис підписки: ~400 bytes (email + repo + 2 токени × 32 bytes + timestamps)
- Стан репозиторію (last seen release): ~100 bytes
- Загальний обсяг при 10K підписок: ~5MB — тривіально

### Bandwidth

- Incoming: < 0.1 Mbps (текстові API-запити)
- Outgoing до GitHub API: ~5KB × 1.2 RPS ≈ 0.05 Mbps
- Outgoing emails: через SMTP-провайдера — не наш bottleneck

**Висновок:** система не є storage- чи bandwidth-інтенсивною. Основне архітектурне обмеження — ліміт GitHub API (5K req/год на один токен). При ~500–700 унікальних репозиторіях і 10-хвилинному інтервалі polling система потребує ~3K–4.2K req/год, що вкладається в стандартний authenticated limit із помірним запасом. Для цього Scanner має виконувати polling per repository, а не per subscription, і бути rate-budget aware, як описано в ADR-001.

# 3. C4 Component Diagram

![Component Diagram](github-releases.arch.drawio.svg)

# 4. Детальний дизайн компонентів

## 4.1 API Server (Go / HTTP + gRPC)

**Відповідальність:**

- Обробка REST API запитів через gRPC-Gateway.
- Обробка gRPC запитів сервісу підписок.
- Валідація email, GitHub repository path та токенів.
- Створення, підтвердження, перегляд і скасування підписок.
- Відправлення confirmation email після створення підписки.

**Endpoints:**

Публічні endpoint-и мають відповідати бізнес-вимозі до API contract:

```text
POST /api/subscribe
GET  /api/confirm/{token}
GET  /api/unsubscribe/{token}
GET  /api/subscriptions?email={email}
GET  / - вебсторінка з формою підписки (опціонально)
```

**Масштабування:** горизонтальне масштабування HTTP/gRPC інстансів можливе за умови спільної PostgreSQL бази та стабільного `EMAIL_SECRET_KEY`. Якщо додається кеш для валідації репозиторіїв, він має бути спільним між інстансами або мати короткий TTL, але Scanner не має покладатися на stale response cache для release detection.

## 4.2 Scanner Worker

**Відповідальність:**

- Виявлення нових релізів у репозиторіях, на які є підтверджені підписки.
- Ізоляція інтеграції з GitHub від API Server.
- Durable створення подій про нові релізи в email outbox перед оновленням стану репозиторію.
- Підтримка стану перевірки репозиторіїв, щоб не надсилати повторні сповіщення про вже оброблені релізи.

**Взаємодії:**

- Читає перелік активних репозиторіїв з PostgreSQL.
- Отримує інформацію про релізи через GitHub API Client.
- Створює notification events в email outbox перед оновленням стану останнього обробленого релізу.
- Оновлює стан останнього обробленого релізу в PostgreSQL.

**Масштабування:** за горизонтального масштабування scanner-компонента потрібна координація обробки репозиторіїв, щоб один репозиторій не перевірявся кількома інстансами одночасно.

## 4.3 Sender Worker

**Відповідальність:**

- Доставка email-сповіщень, сформованих після виявлення нового GitHub release.
- Ізоляція email-провайдера від Scanner Worker.
- Контроль пропускної здатності доставки, щоб не перевищувати ліміти провайдера.
- Експорт операційних метрик для моніторингу delivery pipeline.

**Взаємодії:**

- Отримує pending notification events з email outbox.
- Надсилає email через Email Provider Integration.
- Публікує метрики стану черги та доставки.

**Надійність:** у target-state Sender отримує події з persistent queue або transactional outbox, щоб release notification events не втрачалися при рестарті процесу. Цільова outbox-специфікація описана в [email-outbox-spec.md](email-outbox-spec.md).

## 4.4 GitHub API Client

**Відповідальність:**

- Валідація існування репозиторію при підписці.
- Отримання latest release для scanner worker.
- Обробка GitHub `404`, `403` та `429` відповідей.
- Повернення `Retry-After` при rate-limit сценаріях.

**Caching strategy:**

- **Release polling:** Scanner має використовувати conditional requests через `ETag` / `If-Modified-Since`, а cache metadata зберігати разом зі станом репозиторію в PostgreSQL.
- **Repository validation:** для subscribe flow може використовуватися короткий in-process або Redis cache за ключем `owner/repo`, але він не має підміняти release polling.
- **Fallback:** якщо cache metadata відсутня, виконується звичайний запит до GitHub API.

**Rate limiting:**

```text
unauthenticated: 60 requests/hour
authenticated:   5 000 requests/hour per token
```

## 4.5 Email Provider Integration

**Відповідальність:**

- Відправлення confirmation email після створення підписки.
- Відправлення release notification emails батчами.
- Формування unsubscribe link у кожному notification email.

**Delivery constraints:**

- Компонент має враховувати rate limits та batch limits email-провайдера.
- Confirmation emails і release notification emails мають різні бізнес-сценарії, але використовують спільну Email Provider Integration для формування листів, rate-limit handling і виклику провайдера.
- У target-state release notification emails проходять через Scanner -> PostgreSQL email outbox -> Sender -> Email Provider Integration.
- Confirmation email надсилається API Server-ом напряму через Email Provider Integration. Confirmation emails можуть бути додані до outbox окремим рішенням, якщо потрібні такі самі durable retry гарантії.
- Кожен release notification email має містити unsubscribe link.

**Fallback:** при тимчасовій недоступності email-провайдера потрібен retry mechanism на рівні delivery pipeline; без persistent queue гарантія at-least-once не виконується повністю.
