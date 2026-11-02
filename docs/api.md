# API Dorm Hub

Ответ: `{"data":...}`; ошибка: `{"error":{"code":"...","message":"..."}}`.
Для изменений нужен X-CSRF-Token. В production Origin должен совпадать с BASE_URL.
В development также принимается Origin фактического адреса запроса, если это
localhost или частный/loopback/link-local IP; схема и порт должны совпадать.
Подставные forwarded-заголовки не дают разрешения. Сессия — cookie.

## Вход и аккаунт

| Метод / адрес | Действие |
|---|---|
| GET /api/auth/bootstrap | Гостевой CSRF и версии согласия/правил |
| POST /api/auth/request | Код входа: `{email,consent,rules,consent_version,rules_version}` |
| POST /api/auth/verify | Вход: `{email,code}`; совместимое поле reactivate игнорируется |
| POST /api/auth/admin/request | Код только для почты администратора: `{email}`; без флажков согласия и правил |
| POST /api/auth/admin/verify | Вход администратора: `{email,code}`; роль проверяется до создания сессии |
| POST /api/auth/logout | Завершить текущую сессию |
| GET /api/me | Свой профиль, email и CSRF |
| PUT /api/me | Никнейм, Telegram/VK; контакты необязательны |
| POST /api/me/deletion-code | Отдельный код удаления в текущей сессии |
| DELETE /api/me | `{code}`; полное удаление, ответ `{deleted,files_pending}` |
| GET /api/statistics | `{completed_deals}` без индивидуальных показателей |
| GET /api/admin/analytics?days=7&offset=0 | Только администратор; статистика за 1/7/30 дней и таблица объявлений по 20 строк |
| POST /api/analytics/events | Авторизация и CSRF; `{listing_id,event:"telegram"/"vk"}`; 120 запросов/час, дневная дедупликация |
| GET /api/catalog | Местоположения, общежития, категории |
| GET /api/users/{id} | Публичный профиль без почты |
| PUT /api/users/{id}/star | Поставить звезду |
| DELETE /api/users/{id}/star | Отозвать звезду |

## Объявления

Создание/обновление: `kind,category,title,description,location,address,dorm_id,price_mode,
price_minor,telegram,vk,draft,revision,has_photos`.
`location=EXTERNAL` — «Другой адрес»: `address` необязателен, до 300 символов, `dorm_id=null`. Для остальных локаций `address` должен быть пустым. Если передан `dorm_id`, сервер определяет `location` по справочнику общежитий: отдельно выбирать его не нужно.
`draft=true` допускает неполные поля. При отправке на проверку нужен хотя бы один
валидный HTTPS-контакт в объявлении. Полная ссылка, `@username` Telegram
или имя страницы VK нормализуются сервером перед сохранением. Цена — целое число копеек; 0 — бесплатно.
`revision` из карточки позволяет обнаружить конфликт вкладок. `has_photos` —
контейнер черновика перед отправкой фото, не разрешение публичного доступа.

| Метод / адрес | Действие |
|---|---|
| GET /api/listings | Лента; scope=mine/favorites/admin_author, фильтры и пагинация |
| GET /api/showcase | Категории с количеством и до 8 карточек; kind, location, seed |
| POST /api/listings | Создать, с заголовком Idempotency-Key |
| GET /api/listings/{id} | Карточка с status/archive_kind/moderation_comment/revision |
| PUT /api/listings/{id} | Обновить; публикация снимается, нужна повторная проверка |
| DELETE /api/listings/{id} | Полностью удалить своё объявление и фото |
| POST /api/listings/{id}/status | `{action:"submit"/"archive"/"complete",outcome}` |
| PUT /api/listings/{id}/favorite | Избранное |
| DELETE /api/listings/{id}/favorite | Убрать из избранного |
| POST /api/listings/{id}/photos | multipart file, Idempotency-Key |
| PUT /api/listings/{id}/photos | `{ids:[...]}` — порядок/обложка |
| PUT /api/listings/{id}/photos/{photo} | multipart file, Idempotency-Key — заменить фото с сохранением позиции; возвращает новый ID |
| DELETE /api/listings/{id}/photos/{photo} | Удалить фото |
| GET /api/photos/{photo} | Фото с теми же правами, что карточка |

Замена фото атомарна: старый файл ставится в очередь удаления только вместе с успешным сохранением нового. Замена учитывает разницу размеров в квоте и доступна при 10 фото в объявлении. Повтор с тем же ключом и телом возвращает прежний результат. Правки фотографии требуют повторной модерации.

`complete` требует `outcome:"success"` или `"no_longer_needed"`.
`submit` полностью проверяет поля и ставит приватное объявление в очередь.
Совместимый `activate` также означает отправку на проверку, а не публикацию.
Завершённое объявление при обновлении не отправляется на проверку автоматически.
Статусы: active/pending/archived/completed. Завершённые приватны и не архивируются
по таймеру. Пагинация limit до 50, offset. `admin_author` требует администратора.

## Модератор

| Метод / адрес | Действие |
|---|---|
| GET /api/admin/listings | Очередь pending, пагинация |
| GET /api/admin/listings?category_review=true | Старые объявления для распределения по категориям |
| PUT /api/admin/listings/{id}/category | `{category,revision}`; изменить только категорию, сохранив статус |
| POST /api/admin/listings/{id}/review | `{action:"approve"/"reject",revision,comment}` |
| POST /api/reports | Жалоба `{target_type,target_id,reason}` |
| GET /api/admin/reports | Жалобы |
| PUT /api/admin/reports/{id} | `{status,resolution}` |
| GET /api/admin/users | Пользователи |
| POST /api/admin/actions | `{target_type,target_id,action,reason}` |
| POST /api/admin/dorms | Добавить общежитие |
| DELETE /api/admin/dorms/{id} | Удалить общежитие |

Для отказа `comment` обязателен (3..2000 символов). Ревизия обязательна для обоих
решений: несовпадение возвращает 409. Hide/show не заменяют approval: снятие
блокировки не публикует объявление автоматически.

На главной `/api/showcase` смешивает новые и более старые активные объявления,
учитывая разнообразие авторов. `seed` — неотрицательное число до 13 цифр;
одинаковый seed сохраняет порядок при неизменном наборе объявлений. По умолчанию
он меняется каждые 4 часа. Пустые категории скрыты, остальные идут по количеству,
«Другое» — в конце. `/api/listings` сохраняет обычную пагинацию и сортировку:
поиск по соответствию запросу, категория без запроса — по дате.

## Лимиты

30 созданий/час, 120 загрузок/час, 10 жалоб/час, 10 запросов кода удаления/час.
Повторные/неуспешные попытки учитываются. До 10 active+pending у автора, до 50
новых архивных черновиков, до 10 фото в объявлении, квота аккаунта из настроек.
JSON до 64 КиБ; фото до 5 МиБ / 16 Мп, преобразуются в JPEG до 1920 пикселей.


## Страницы и технические работы

`/` — открытая главная. При действующей сессии сервер отвечает перенаправлением на `/app`, не показывая главную перед лентой. `/about` позволяет прочитать описание даже вошедшему пользователю. `/app` — приложение с нейтральным экраном загрузки. `/login` — обычный вход, `/admin-login` — отдельная форма администратора. Документы доступны по `/rules`, `/privacy`, `/consent`.

`GET/PUT /api/admin/site-settings` требуют роли администратора. При включении нужен `reason` длиной 1..1000 символов. Технические работы возвращают 503 обычным посетителям, но не удаляют сессии. Документы, статические файлы, health/ready и вход администратора остаются доступны. Состояние сохраняется в БД между обновлениями.

`GET /api/admin/logs?level=ERROR&q=...` — последние 200 совпадений из буфера 500 сообщений приложения; только администратор. Уровень WARN включает ERROR. `q` до 200 символов.

`/monitoring/` — прокси Grafana, если установлен `MONITORING_URL`. Каждый запрос,
включая статические файлы и API Grafana, требует админской сессии Dorm Hub.
Grafana дополнительно использует собственный вход. Чужой Origin для изменений
отклоняется. Сессия Dorm Hub и заголовки аутентификации в Grafana не передаются.

`GET /metrics` доступен только на отдельном служебном слушателе `METRICS_ADDR`.
На публичном слушателе сайта этот адрес возвращает 404. Ресурс/сеть настраиваются
установщиком; подробности — в [monitoring.md](monitoring.md).
