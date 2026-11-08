# Dorm Hub

Доска объявлений для студентов СПбГУ: продажа и обмен вещей, услуги и помощь
в общежитиях. Сайт: [dorm-hub.ru](https://dorm-hub.ru).

Backend — Go (`net/http`), PostgreSQL (`pgx`), Meilisearch. Интерфейс — HTML,
CSS и JavaScript. Статика и SQL-миграции встроены в Go-бинарник.

Вход по коду из письма. Объявления проходят ручную модерацию. Есть черновики
с автосохранением, фотографии, избранное и жалобы.

## Локальный запуск

```bash
docker compose up -d --build
```

Приложение: http://localhost:8080. Письма с кодами: http://localhost:8025.
Тестовые адреса: `student@students.example` и `admin@students.example`.

## Тесты

JavaScript:

```bash
node --test tests/*.test.cjs
```

Go с тестовыми PostgreSQL и Meilisearch:

```bash
docker compose -f compose.test.yaml up --abort-on-container-exit --exit-code-from tests
docker compose -f compose.test.yaml down
```
