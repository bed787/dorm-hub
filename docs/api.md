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
| GET /api/catalog | Районы, общежития, категории |
| GET /api/users/{id} | Публичный профиль без почты |
| PUT /api/users/{id}/star | Поставить звезду |
| DELETE /api/users/{id}/star | Отозвать звезду |

## Объявления

Создание/обновление: `kind,category,title,description,location,dorm_id,price_mode,
price_minor,telegram,vk,draft,revision,has_photos`.
`draft=true` допускает неполные поля. При отправке на проверку нужен хотя бы один
валидный HTTPS-контакт в объявлении. Полная ссылка, `@username` Telegram
или имя страницы VK нормализуются сервером перед сохранением. Цена — целое число копеек; 0 — бесплатно.
`revision` из карточки позволяет обнаружить конфликт вкладок. `has_photos` —
контейнер черновика перед отправкой фото, не разрешение публичного доступа.

| Метод / адрес | Действие |
|---|---|
| GET /api/listings | Лента; scope=mine/favorites/admin_author, фильтры и пагинация |
| POST /api/listings | Создать, с заголовком Idempotency-Key |
| GET /api/listings/{id} | Карточка с status/archive_kind/moderation_comment/revision |
| PUT /api/listings/{id} | Обновить; публикация снимается, нужна повторная проверка |
| DELETE /api/listings/{id} | Полностью удалить своё объявление и фото |
| POST /api/listings/{id}/status | `{action:"submit"/"archive"/"complete",outcome}` |
| PUT /api/listings/{id}/favorite | Избранное |
| DELETE /api/listings/{id}/favorite | Убрать из избранного |
| POST /api/listings/{id}/photos | multipart file, Idempotency-Key |
| PUT /api/listings/{id}/photos | `{ids:[...]}` — порядок/обложка |
| DELETE /api/listings/{id}/photos/{photo} | Удалить фото |
| GET /api/photos/{photo} | Фото с теми же правами, что карточка |

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

## Лимиты

30 созданий/час, 120 загрузок/час, 10 жалоб/час, 10 запросов кода удаления/час.
Повторные/неуспешные попытки учитываются. До 10 active+pending у автора, до 50
новых архивных черновиков, до 10 фото в объявлении, квота аккаунта из настроек.
JSON до 64 КиБ; фото до 5 МиБ / 16 Мп, преобразуются в JPEG до 1920 пикселей.


## Страницы и технические работы

`/` — открытая главная. При действующей сессии сервер отвечает перенаправлением на `/app`, не показывая главную перед лентой. `/about` позволяет прочитать описание даже вошедшему пользователю. `/app` — приложение с нейтральным экраном загрузки. `/login` — обычный вход, `/admin-login` — отдельная форма администратора. Документы доступны по `/rules`, `/privacy`, `/consent`.

`GET/PUT /api/admin/site-settings` требуют роли администратора. При включении нужен `reason` длиной 1..1000 символов. Технические работы возвращают 503 обычным посетителям, но не удаляют сессии. Документы, статические файлы, health/ready и вход администратора остаются доступны. Состояние сохраняется в БД между обновлениями.
