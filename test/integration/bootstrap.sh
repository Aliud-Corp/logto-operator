#!/bin/sh
# Writes the m-default credentials of the THROWAWAY integration Logto to build/it.env (mode 0600).
# m-default is Logto's built-in admin-tenant M2M application; it gets its token at the admin endpoint for the
# resource https://default.logto.app/api. Its secret is read (never written) from the throwaway database: this is
# the documented bootstrap route, exercised here so no credential is ever created by hand.
set -eu
: "${IT_COMPOSE:?}"
mkdir -p build
umask 077
secret=$($IT_COMPOSE exec -T postgres psql -U logto -d logto -tAc "select secret from applications where id = 'm-default'")
if [ -z "$secret" ]; then
  echo "it:bootstrap FAILED: no 'm-default' application in this Logto; its bootstrap application changed." >&2
  exit 1
fi
{
  echo "LOGTO_IT_ENDPOINT=http://localhost:3001"
  echo "LOGTO_IT_ADMIN_ENDPOINT=http://localhost:3002"
  echo "LOGTO_IT_CLIENT_ID=m-default"
  echo "LOGTO_IT_CLIENT_SECRET=$secret"
  echo "LOGTO_IT_VERSION=${LOGTO_VERSION:-}"
} > build/it.env
chmod 600 build/it.env
echo "it:bootstrap: build/it.env written (mode 0600)."
