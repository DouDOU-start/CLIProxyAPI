#!/usr/bin/env bash

# This script initializes and deploys the production Docker Compose stack.

set -euo pipefail

if [[ $# -ne 0 ]]; then
  echo "错误：不支持参数 '${1}'。" >&2
  echo "用法：./deploy.sh" >&2
  exit 1
fi

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${script_dir}"

config_path="${CLI_PROXY_CONFIG_PATH:-./config.yaml}"
log_path="${CLI_PROXY_LOG_PATH:-./logs}"
plugin_path="${CLI_PROXY_PLUGIN_PATH:-./plugins}"
service_port="8317"

require_command() {
  local command_name="$1"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "错误：缺少命令 ${command_name}。" >&2
    exit 1
  fi
}

generate_password() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
}

data_directory_has_content() {
  local first_entry
  if [[ ! -d ./data ]]; then
    return 1
  fi
  if ! first_entry="$(find ./data -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)"; then
    return 0
  fi
  [[ -n "${first_entry}" ]]
}

initialize_config() {
  if [[ -d "${config_path}" ]]; then
    echo "错误：配置路径是目录而不是文件：${config_path}" >&2
    exit 1
  fi

  if [[ ! -f "${config_path}" ]]; then
    if data_directory_has_content; then
      echo "错误：data 目录已有 PostgreSQL 数据，但缺少配置文件 ${config_path}。" >&2
      echo "请先恢复原配置文件，避免生成与现有数据库不匹配的密码。" >&2
      exit 1
    fi
    mkdir -p "$(dirname "${config_path}")"
    cp ./config.example.yaml "${config_path}"
    echo "已创建生产配置：${config_path}"
  fi

  if grep -q "change-this-password" "${config_path}"; then
    if data_directory_has_content; then
      echo "错误：配置仍使用示例数据库密码，但 data 目录已经包含数据。" >&2
      echo "请先修改 PostgreSQL 用户密码并同步更新 config.yaml。" >&2
      exit 1
    fi
    local postgres_password
    postgres_password="$(generate_password)"
    sed -i "s/change-this-password/${postgres_password}/g" "${config_path}"
    echo "已生成 PostgreSQL 随机密码。"
  fi

  chmod 600 "${config_path}"
}

configure_service_port() {
  local configured_port
  configured_port="$(sed -n 's/^port:[[:space:]]*//p' "${config_path}" | head -n 1)"
  configured_port="${configured_port%%#*}"
  configured_port="$(printf '%s' "${configured_port}" | tr -d "'\"[:space:]")"
  if [[ -z "${configured_port}" ]]; then
    configured_port="8317"
  fi
  if [[ ! "${configured_port}" =~ ^[0-9]+$ ]] ||
    ((10#${configured_port} < 1 || 10#${configured_port} > 65535)); then
    echo "错误：config.yaml 中的端口无效：${configured_port}" >&2
    exit 1
  fi

  service_port="${configured_port}"
  export CLI_PROXY_PORT="${service_port}"
}

wait_for_service() {
  local health_url="http://127.0.0.1:${service_port}/healthz"
  local attempt

  if command -v curl >/dev/null 2>&1; then
    for ((attempt = 1; attempt <= 60; attempt++)); do
      if curl --fail --silent --show-error "${health_url}" >/dev/null 2>&1; then
        echo "服务健康检查已通过。"
        return
      fi
      sleep 1
    done
  elif command -v wget >/dev/null 2>&1; then
    for ((attempt = 1; attempt <= 60; attempt++)); do
      if wget --quiet --output-document=/dev/null "${health_url}"; then
        echo "服务健康检查已通过。"
        return
      fi
      sleep 1
    done
  else
    echo "未检测到 curl 或 wget，已跳过 HTTP 健康检查。"
    return
  fi

  echo "错误：服务未能在 60 秒内通过健康检查。" >&2
  docker compose logs --tail=100 cli-proxy-api >&2
  exit 1
}

require_command docker
require_command find
require_command git
require_command grep
require_command sed
require_command tr
if ! command -v openssl >/dev/null 2>&1; then
  require_command od
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "错误：未安装 Docker Compose 插件。" >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "错误：无法连接 Docker 服务，请检查 Docker 是否运行以及当前用户权限。" >&2
  exit 1
fi

initialize_config
configure_service_port
mkdir -p ./data "${log_path}" "${plugin_path}"

echo "正在拉取 PostgreSQL 镜像..."
docker compose pull postgres

echo "正在构建并启动生产服务..."
bash ./docker-build.sh

wait_for_service

echo
echo "部署完成。"
echo "管理页面：http://服务器地址:${service_port}/management.html"
echo "查看日志：docker compose logs -f --tail=200"
