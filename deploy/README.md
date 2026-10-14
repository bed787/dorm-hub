# Автоматическое обновление dorm-hub

После настройки: коммит и `git push` в main → GitHub запускает 30 существующих
фронтенд-тестов, собирает Docker-образ (Dockerfile также выполняет go vet и
go test), публикует его в приватном GHCR и обновляет приложение на VPS.
Разворачивается конкретный digest образа, а не изменяемый тег latest.

## Установка: на Mac

Распакуйте архив непосредственно в папку проекта. В архиве только новые файлы:
`.github/workflows/deploy.yml` и папка `deploy`. Исходники сайта не заменяются.

```bash
cd /Users/ruslan/Desktop/projects/dorm-hub
unzip -o ~/Downloads/dorm-hub-autodeploy.zip -d .
scp -r deploy root@157.228.138.75:/opt/dorm-hub/
```

## Установка: на сервере

```bash
bash /opt/dorm-hub/deploy/install-server.sh
```

Установщик проверяет конфигурацию, устанавливает файл Compose и команду
обновления и разрешает вход отдельным ключом только для этой команды.
Обычный вход root по паролю не меняется. Контейнеры не перезапускаются.
Если отдельный ключ github_actions_deploy уже создан, он используется повторно.

## Доступ GitHub: на Mac

Эти команды передают ключ напрямую в секрет GitHub, не выводя его на экран.
Первое SSH-подключение использует обычный пароль сервера. Второе читает
публичный ключ сервера через уже доверенное SSH-соединение; ssh-keyscan
из GitHub Actions не используется.

```bash
cd /Users/ruslan/Desktop/projects/dorm-hub
set -o pipefail
ssh root@157.228.138.75 'cat /root/.ssh/github_actions_deploy' | gh secret set DEPLOY_SSH_KEY --repo bed787/dorm-hub
ssh root@157.228.138.75 'printf "157.228.138.75 "; cat /etc/ssh/ssh_host_ed25519_key.pub' | gh secret set DEPLOY_KNOWN_HOSTS --repo bed787/dorm-hub
```

Секреты видны по именам в Settings → Secrets and variables → Actions.
Токен для GHCR создавать не нужно: workflow использует временный GITHUB_TOKEN,
а сервер хранит его Docker-авторизацию только на время выкладки.

GitHub CLI может потребовать разрешение на загрузку workflow. Один раз:

```bash
gh auth refresh --hostname github.com --scopes workflow
```

## Первый запуск: на Mac

```bash
git add .github/workflows/deploy.yml deploy
git commit -m "Build and deploy website with GitHub Actions"
git push
```

Этот push уже запустит первую выкладку. Смотрите GitHub → Actions → Deploy
website. Успех — зелёная отметка. При ошибке откройте красный шаг и его лог.
Изменение не публикуется, если тесты или сборка не прошли.

## Дальнейшая работа: на Mac

```bash
git add .
git commit -m "Описание изменений"
git push
```

GitHub запустит обновление только для main. В Actions есть и ручная кнопка Run
workflow для повторного запуска. Одновременные выкладки выполняются по очереди;
при серии push GitHub может заменить ожидающий запуск самым новым.

## Что происходит на сервере

- Постоянная конфигурация: `/etc/dorm-hub/compose.registry.yaml`.
- Секреты приложения: существующий `/opt/dorm-hub/.env.production`.
- Состояние выкладки: `/var/lib/dorm-hub-deploy/`.
- Обновляется только app. Используются существующие volumes
  `dorm-hub_postgres` и `dorm-hub_uploads`, порт приложения 127.0.0.1:8080
  и порт БД 127.0.0.1:5432. Caddy и DBeaver продолжают работать.
- Сервер скачивает готовый образ. git pull и сборка на VPS больше не нужны.
  Файлы исходников в /opt/dorm-hub автоматически не обновляются.
- Возможен короткий перерыв при замене app. После запуска проверяется /ready.
  Если проверка не прошла, скрипт пытается вернуть предыдущий образ приложения
  и всё равно сообщает GitHub об ошибке.
- Предыдущий образ остаётся локально. Не удаляйте его через docker image prune -a,
  если хотите сохранить возможность отката. Образы постепенно занимают диск;
  смотреть использование можно командой docker system df.

## Откат: на сервере

```bash
/usr/local/sbin/dorm-hub-deploy rollback
```

Это откат образа приложения, не базы данных. Миграции выполняются приложением
при старте; изменение схемы может быть несовместимо со старым образом.
Перед будущими изменениями схемы нужен отдельный backup БД и план миграции.
Этот комплект не создаёт резервные копии базы и фотографий.

Для устойчивого отката также отмените ошибочный коммит на Mac через git revert
и отправьте исправление, иначе следующий push снова выложит тот же код.

## Изменения настроек развёртывания

Автоматически обновляется код приложения, включая встроенные HTML/CSS/JS.
Правки Dockerfile также участвуют в следующей сборке. Если меняете конфигурацию
Compose или server-deploy.sh, повторите scp папки deploy и запуск установщика
до следующего push. Установщик не запускается автоматически из репозитория.
Переменные окружения меняются отдельно на сервере в .env.production.

После перехода для просмотра статуса можно выполнить на сервере:

```bash
APP_IMAGE=$(cat /var/lib/dorm-hub-deploy/current-image) docker compose --project-name dorm-hub --env-file /opt/dorm-hub/.env.production -f /etc/dorm-hub/compose.registry.yaml ps
```

Логи по-прежнему: `docker logs --tail=100 -f dorm-hub-app-1`.
Не запускайте старую команду up --build с compose.production.yaml поверх этой
схемы: она переключит приложение обратно на локальную сборку.

## Проверено до передачи

Проверены синтаксис Bash и YAML, сохранение имён volumes и привязок портов,
ограничение SSH-команд и сценарии скрипта с имитацией Docker: успешная выкладка,
ошибка скачивания, ошибка healthcheck с восстановлением, ручной откат и
отклонение посторонней команды. На имеющейся локальной копии исходников прошли
30 фронтенд-тестов. Полная сборка образа и работа реального SSH/Compose должны
подтвердиться первым запуском Actions: здесь нет доступа к вашему VPS и Docker.

Документация GitHub:
https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images
https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry
