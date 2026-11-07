#!/usr/bin/env bash
# Токен вводится скрыто и хранится только в памяти штатного Git credential-cache.
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
read -r -p 'Логин Gitea [ysoltamb]: ' gitea_user
gitea_user=${gitea_user:-ysoltamb}
read -r -s -p 'Токен Gitea с правом записи репозитория: ' gitea_token
printf '\n'
if [[ -z "$gitea_token" ]]; then
  printf 'Токен пуст. Доступ не изменён.\n' >&2
  exit 1
fi
git config --local credential.helper 'cache --timeout=3600'
printf 'protocol=https\nhost=01.tomorrow-school.ai\nusername=%s\npassword=%s\n\n' "$gitea_user" "$gitea_token" | git credential approve
unset gitea_token
git ls-remote origin
printf 'Проверка Git-доступа завершена.\n'
