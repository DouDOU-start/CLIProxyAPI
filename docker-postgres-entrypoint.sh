#!/usr/bin/env bash

set -euo pipefail

config_path="${POSTGRES_BOOTSTRAP_CONFIG:-/bootstrap/config.yaml}"

if [[ ! -f "${config_path}" ]]; then
  echo "PostgreSQL bootstrap config is missing: ${config_path}" >&2
  exit 1
fi

dsn="$(sed -n 's/^[[:space:]]*dsn:[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "${config_path}" | head -n 1)"
if [[ -z "${dsn}" ]]; then
  echo "postgresql.dsn is missing from ${config_path}" >&2
  exit 1
fi

case "${dsn}" in
  postgresql://*:*@*/* | postgres://*:*@*/*) ;;
  *)
    echo "postgresql.dsn must include a user, password, host, and database" >&2
    exit 1
    ;;
esac

decode_uri_component() {
  local encoded="${1//+/ }"
  printf '%b' "${encoded//%/\\x}"
}

connection="${dsn#*://}"
credentials="${connection%%@*}"
host_and_path="${connection#*@}"
database_path="${host_and_path#*/}"

user_encoded="${credentials%%:*}"
password_encoded="${credentials#*:}"
database_encoded="${database_path%%\?*}"

export POSTGRES_USER="$(decode_uri_component "${user_encoded}")"
export POSTGRES_PASSWORD="$(decode_uri_component "${password_encoded}")"
export POSTGRES_DB="$(decode_uri_component "${database_encoded}")"

if [[ -z "${POSTGRES_USER}" || -z "${POSTGRES_PASSWORD}" || -z "${POSTGRES_DB}" ]]; then
  echo "postgresql.dsn contains an empty user, password, or database" >&2
  exit 1
fi

if [[ "${1:-}" == "healthcheck" ]]; then
  exec pg_isready -h 127.0.0.1 -p 5432 -U "${POSTGRES_USER}" -d "${POSTGRES_DB}"
fi

exec docker-entrypoint.sh postgres
