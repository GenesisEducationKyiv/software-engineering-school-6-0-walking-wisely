# 1. Вимоги системи

## Функціональні вимоги

1. Користувач може підписатися на сповіщення про нові релізи GitHub-репозиторію, вказавши email та репозиторій у форматі `owner/repo`.
2. Підписка активується після підтвердження email через одноразове посилання.
3. Користувач може відписатися від конкретного репозиторію через посилання без логіну.
4. Користувач може переглянути перелік своїх підписок за email.
5. При появі нового релізу на GitHub Scanner створює email-сповіщення для підтверджених підписників цього репозиторію.

## Бізнес-вимоги до API contract

Публічні HTTP endpoint-и:

```text
POST /api/subscribe
GET  /api/confirm/{token}
GET  /api/unsubscribe/{token}
GET  /api/subscriptions?email={email}
GET  / - вебсторінка з формою підписки
```

Confirmation та unsubscribe flow працюють як link-based сценарії без логіну, пароля або додаткової взаємодії з боку користувача.

## Нефункціональні вимоги

### Надійність доставки

- Email delivery виконується за best-effort моделлю.
- Система має ізолювати API Server і Scanner від прямої залежності на latency email-провайдера через асинхронний delivery pipeline.
- Система має логувати помилки постановки або доставки email-повідомлень без запису email-адрес у logs.
- Durable at-least-once delivery не є вимогою цього дизайну; втрата email допускається при падінні процесу, перевантаженні delivery pipeline або помилці email-провайдера.

### Затримка сповіщень

- Система має періодично перевіряти GitHub repositories, на які є підтверджені підписки.
- Затримка виявлення release визначається інтервалом scanner-а і короткочасним кешуванням GitHub latest release.
- Після виявлення release система має передати notification email у delivery pipeline без синхронного очікування відповіді email-провайдера.
- Фактичне отримання email користувачем залежить від scanner-а, Sender-а та доступності email-провайдера.

### Масштабованість

- Система має підтримувати до 10 000 активних підписок у межах навчального навантаження.
- GitHub polling має масштабуватися за кількістю унікальних repositories, а не за кількістю підписок.
- Fixed-interval polling має працювати в межах доступного GitHub API rate limit; при рості кількості унікальних repositories саме GitHub API budget є основним обмеженням.

### Доступність

- HTTP API має залишатися доступним для read/delete операцій підписок незалежно від стану GitHub polling.
- Створення нової підписки може залежати від доступності GitHub, бо система перевіряє існування repository перед збереженням subscription.
- Недоступність GitHub не має зупиняти весь scanner; помилка одного repository не блокує перевірку інших repositories.
- Недоступність email-провайдера не має блокувати HTTP API після передачі повідомлення в delivery pipeline.

### Ідемпотентність

- Повторний запит на підписку з тим самим email і repository не має створювати дубльовану confirmed subscription.
- Повторний subscribe для unconfirmed subscription має оновлювати confirmation token.
- Повторне підтвердження через той самий token має бути безпечним і не створювати додаткових записів.

### Безпека

- Confirmation та unsubscribe tokens мають бути непередбачуваними і підписаними.
- Confirmation та unsubscribe flows мають працювати без логіну через одноразові link tokens.
- Перегляд підписок виконується через `GET /api/subscriptions?email={email}` відповідно до заданого API contract.
- Logs не мають містити email-адреси в критичних місцях; для діагностики потрібно використовувати repository або subscription id.

# 2. Оцінка навантаження

### Користувачі та трафік

- Активних підписок: орієнтир до 10K.
- Унікальних репозиторіїв під спостереженням: приблизно 500-700 за умови, що багато користувачів підписані на однакові repo.
- API запити підписки, підтвердження, відписки та перегляду: очікувано невеликі порівняно з GitHub polling.
- GitHub API: основне обмеження для обраного polling-дизайну.

### Дані

Поточна storage-модель складається з таблиці `subscriptions`, яка містить:

- `email`;
- `repo`;
- `confirmed`;
- `confirm_token`;
- `unsubscribe_token`;
- `last_seen_tag`;
- timestamps.

Індекси підтримують унікальність `(email, repo)`, lookup за tokens, scanner queries по confirmed repo та lookup за email.

### GitHub API budget

Поточний Scanner робить один latest-release запит на унікальний confirmed repo за scan cycle, якщо відповідь не знайдена в Redis cache.

Для 500-700 унікальних repo і 10-хвилинного scan interval це може створити до 3000-4200 перевірок на годину без урахування cache hits. Це близько до authenticated GitHub API limit у 5000 запитів на годину, тому fixed-interval polling має обмежений запас для росту.

# 3. C4 Component Diagram

![Component Diagram](github-releases.arch.drawio.svg)

# 4. Детальний дизайн компонентів

## 4.1 API Server

**Відповідальність:**

- Обробка REST/gRPC endpoint-ів підписок.
- Валідація email, GitHub repository path та token format.
- GitHub repository validation при створенні підписки.
- Створення, підтвердження, перегляд і скасування підписок.
- Постановка confirmation email в in-memory channel.

**Endpoints:**

```text
POST /api/subscribe
GET  /api/confirm/{token}
GET  /api/unsubscribe/{token}
GET  /api/subscriptions?email={email}
GET  /
```

## 4.2 Scanner Worker

**Відповідальність:**

- Періодично перевіряти GitHub repositories, на які є підтверджені підписки.
- Отримувати інформацію про latest release через GitHub API Client.
- Визначати підписників, яким потрібно надіслати notification email.
- Передавати release notification emails у delivery pipeline.
- Оновлювати стан обробленого release, щоб не надсилати повторні сповіщення про той самий release.

**Обмеження:**

- Немає окремої таблиці release state.
- Немає durable створення notification events.
- Немає координації кількох scanner instances.
- При перевантаженні delivery pipeline notification може бути втрачений.

## 4.3 Sender Worker

**Відповідальність:**

- Читати email messages з delivery pipeline.
- Групувати повідомлення для ефективної відправки.
- Надсилати batch через Email Provider Integration.
- Логувати помилки доставки.

**Обмеження:**

- Немає durable retry.
- Failed messages не повертаються в персистентну queue.
- Graceful shutdown може зменшити ризик втрати buffered messages, але не є durable гарантією.

## 4.4 GitHub API Client

**Відповідальність:**

- Валідація існування репозиторію при підписці.
- Отримання latest release для Scanner-а.
- Короткочасне кешування latest release.
- Обробка GitHub помилок, зокрема repository not found і rate-limit сценаріїв.
- Передача інформації про retry timing, якщо GitHub її надає.

**Rate limits:**

```text
unauthenticated: 60 requests/hour
authenticated:   5000 requests/hour per token
```

## 4.5 Email Provider Integration

**Відповідальність:**

- Формування provider request для email delivery.
- Відправлення email через зовнішній email provider.
- Нормалізація provider errors для Sender-а.
- Логування успішних batch sends.

Confirmation emails і release notification emails використовують спільний Sender та Email Provider Integration.

# 5. Заплановані покращення

Ці покращення не входять до цього дизайну, але є наступними кроками для посилення його нефункціональних вимог.

- **Надійність доставки:** перейти від best-effort email delivery до at-least-once delivery для release notifications. Це посилить гарантію, що тимчасовий збій процесу або email-провайдера не призводить до безповоротної втрати notification.
- **Надійність доставки:** додати контрольовану поведінку для повторних спроб і фінальних помилок доставки. Система має розрізняти тимчасові та невідновні проблеми й давати операційний спосіб побачити, які повідомлення не були доставлені.
- **Затримка сповіщень:** зробити release detection більш передбачуваним при різній активності repositories. Система має чіткіше визначати очікувану затримку між появою release на GitHub і створенням notification.
- **Масштабованість:** зменшити залежність GitHub polling від fixed interval для всіх repositories. Система має краще витримувати ріст кількості унікальних repositories без непропорційного росту GitHub API usage.
- **Доступність:** покращити деградацію при тимчасових збоях GitHub. Недоступність GitHub або rate limit не мають створювати неконтрольований backlog, retry storm або блокування unrelated repositories.
- **Спостережуваність:** додати кращу видимість стану background processing. Оператори мають бачити затримку scanner-а, стан delivery pipeline, кількість помилок, retry і найстаріші pending work items.
- **Масштабованість інтеграції з GitHub:** розглянути альтернативні способи отримання release events або збільшення GitHub API budget для repositories, де це можливо. Конкретний механізм має бути визначений окремим ADR.
