---
name: deploy
description: Bump patch version in addon/config.yaml, commit, push, wait for CI build, then update zabkiss addon on Home Assistant
disable-model-invocation: true
tools: Bash, Read, Edit
---

Выполни деплой аддона zabkiss на Home Assistant. Шаги строго по порядку, без пропусков.

## 1. Поднять версию

Прочитай текущую версию из `addon/config.yaml` (строка `version: "X.Y.Z"`).
Увеличь патч-версию на 1 (X.Y.Z → X.Y.Z+1).
Обнови файл через Edit.

## 2. Сделать коммит и запушить

```bash
git add addon/config.yaml
git commit -m "chore: bump version to <новая_версия>"
git push origin main
```

## 3. Дождаться завершения CI

Подожди 10 секунд после пуша, потом найди запущенный workflow и жди его завершения:

```bash
sleep 10
RUN_ID=$(gh run list --repo ognick/ognick.github.io --workflow=release.yml --limit=1 --json databaseId -q '.[0].databaseId')
echo "Watching run $RUN_ID..."
gh run watch "$RUN_ID" --repo ognick/ognick.github.io --exit-status
```

Если `gh run watch` вернул ненулевой код — билд упал. Сообщи пользователю и остановись.

## 4. Обновить аддон на Home Assistant

После успешного завершения CI выполни обновление через SSH:

```bash
ssh homeassistant.local "ha store reload && ha apps update 0a184fd3_zabkiss"
```

Если SSH недоступен — попроси пользователя выполнить команду вручную:
```
ha store reload && ha apps update 0a184fd3_zabkiss
```
