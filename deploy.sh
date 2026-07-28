#!/usr/bin/env bash

# This script initializes and deploys the production Docker Compose stack.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${script_dir}"

usage() {
  echo "用法：" >&2
  echo "  ./deploy.sh                  # 终端中进入交互模式" >&2
  echo "  ./deploy.sh <实例名> <端口>" >&2
}

read_config_port() {
  local target_config="$1"
  local configured_port=""
  if [[ -f "${target_config}" ]]; then
    configured_port="$(sed -n 's/^port:[[:space:]]*//p' "${target_config}" | head -n 1)"
    configured_port="${configured_port%%#*}"
    configured_port="$(printf '%s' "${configured_port}" | tr -d "'\"[:space:]")"
  fi
  printf '%s' "${configured_port:-8317}"
}

interactive_mode=false
instance_name=""
requested_port=""
case $# in
  0)
    if [[ -t 0 ]]; then
      interactive_mode=true
      echo "CLIProxyAPI 生产部署"
      echo

      existing_names=()
      existing_ports=()
      default_port="$(read_config_port ./config.yaml)"
      if [[ -f ./config.yaml ]]; then
        echo "1) 更新默认实例（端口 ${default_port}）"
      else
        echo "1) 创建默认实例（端口 ${default_port}）"
      fi

      option_number=2
      shopt -s nullglob
      for instance_config in ./instances/*/config.yaml; do
        existing_name="$(basename "$(dirname "${instance_config}")")"
        if [[ ! "${existing_name}" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
          continue
        fi
        existing_port="$(read_config_port "${instance_config}")"
        existing_names+=("${existing_name}")
        existing_ports+=("${existing_port}")
        echo "${option_number}) 更新实例 ${existing_name}（端口 ${existing_port}）"
        ((option_number++))
      done
      shopt -u nullglob

      create_option="${option_number}"
      echo "${create_option}) 创建新的命名实例"
      echo
      read -r -p "请选择操作 [1-${create_option}]: " selected_option
      if [[ ! "${selected_option}" =~ ^[0-9]+$ ]] ||
        ((10#${selected_option} < 1 || 10#${selected_option} > create_option)); then
        echo "错误：选择无效：${selected_option}" >&2
        exit 1
      fi

      if ((10#${selected_option} == 1)); then
        instance_name=""
        requested_port=""
      elif ((10#${selected_option} == create_option)); then
        read -r -p "新实例名称: " instance_name
        if [[ ! "${instance_name}" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
          echo "错误：实例名只能包含小写字母、数字和连字符，且必须以字母或数字开头。" >&2
          exit 1
        fi
        if [[ -e "./instances/${instance_name}" ]]; then
          echo "错误：实例 ${instance_name} 已存在，请重新运行脚本并从列表中选择。" >&2
          exit 1
        fi
        read -r -p "服务端口（每个实例必须唯一）: " requested_port
        if [[ -f ./config.yaml && "${requested_port}" == "${default_port}" ]]; then
          echo "错误：端口 ${requested_port} 已由默认实例使用。" >&2
          exit 1
        fi
        for existing_index in "${!existing_ports[@]}"; do
          if [[ "${requested_port}" == "${existing_ports[${existing_index}]}" ]]; then
            echo "错误：端口 ${requested_port} 已由实例 ${existing_names[${existing_index}]} 使用。" >&2
            exit 1
          fi
        done
      else
        selected_index=$((10#${selected_option} - 2))
        instance_name="${existing_names[${selected_index}]}"
        requested_port="${existing_ports[${selected_index}]}"
      fi
      echo
    fi
    ;;
  2)
    instance_name="$1"
    requested_port="$2"
    ;;
  *)
    echo "错误：参数数量不正确。" >&2
    usage
    exit 1
    ;;
esac

if [[ -n "${instance_name}" && ! "${instance_name}" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
  echo "错误：实例名只能包含小写字母、数字和连字符，且必须以字母或数字开头。" >&2
  exit 1
fi
if [[ -n "${instance_name}" && -z "${requested_port}" ]]; then
  echo "错误：命名实例必须指定唯一的服务端口。" >&2
  exit 1
fi
if [[ -n "${requested_port}" ]] &&
  { [[ ! "${requested_port}" =~ ^[0-9]+$ ]] ||
    ((10#${requested_port} < 1 || 10#${requested_port} > 65535)); }; then
  echo "错误：端口无效：${requested_port}" >&2
  exit 1
fi

if [[ "${interactive_mode}" == true ]]; then
  if [[ -n "${instance_name}" ]]; then
    echo "实例：${instance_name}"
    echo "端口：${requested_port}"
  else
    echo "实例：默认实例"
    echo "端口：${requested_port:-${default_port:-8317}}"
  fi
  read -r -p "确认开始部署？[Y/n]: " confirmation
  case "${confirmation:-y}" in
    y | Y | yes | YES | Yes) ;;
    *)
      echo "已取消部署。"
      exit 0
      ;;
  esac
  echo
fi

if [[ -n "${instance_name}" ]]; then
  instance_path="./instances/${instance_name}"
  config_path="${CLI_PROXY_CONFIG_PATH:-${instance_path}/config.yaml}"
  data_path="${CLI_PROXY_DATA_PATH:-${instance_path}/data}"
  log_path="${CLI_PROXY_LOG_PATH:-${instance_path}/logs}"
  plugin_path="${CLI_PROXY_PLUGIN_PATH:-${instance_path}/plugins}"
  export COMPOSE_PROJECT_NAME="${COMPOSE_PROJECT_NAME:-cli-proxy-${instance_name}}"
  export CLI_PROXY_API_CONTAINER="${CLI_PROXY_API_CONTAINER:-cli-proxy-${instance_name}-api}"
  export CLI_PROXY_POSTGRES_CONTAINER="${CLI_PROXY_POSTGRES_CONTAINER:-cli-proxy-${instance_name}-postgres}"
else
  config_path="${CLI_PROXY_CONFIG_PATH:-./config.yaml}"
  data_path="${CLI_PROXY_DATA_PATH:-./data}"
  log_path="${CLI_PROXY_LOG_PATH:-./logs}"
  plugin_path="${CLI_PROXY_PLUGIN_PATH:-./plugins}"
fi
export CLI_PROXY_CONFIG_PATH="${config_path}"
export CLI_PROXY_DATA_PATH="${data_path}"
export CLI_PROXY_LOG_PATH="${log_path}"
export CLI_PROXY_PLUGIN_PATH="${plugin_path}"

compose_project="${COMPOSE_PROJECT_NAME:-$(basename "${script_dir}" | tr '[:upper:]_' '[:lower:]-')}"
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
  if [[ ! -d "${data_path}" ]]; then
    return 1
  fi
  if ! first_entry="$(find "${data_path}" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null)"; then
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
      echo "错误：数据目录已有 PostgreSQL 数据，但缺少配置文件 ${config_path}。" >&2
      echo "请先恢复原配置文件，避免生成与现有数据库不匹配的密码。" >&2
      exit 1
    fi
    mkdir -p "$(dirname "${config_path}")"
    cp ./config.example.yaml "${config_path}"
    echo "已创建生产配置：${config_path}"
  fi

  if grep -q "change-this-password" "${config_path}"; then
    if data_directory_has_content; then
      echo "错误：配置仍使用示例数据库密码，但数据目录已经包含数据。" >&2
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
  if [[ -n "${requested_port}" ]]; then
    if ! grep -q '^port:' "${config_path}"; then
      echo "错误：配置文件中缺少顶层 port 配置。" >&2
      exit 1
    fi
    sed -i "s/^port:.*/port: ${requested_port}/" "${config_path}"
  fi
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
mkdir -p "${data_path}" "${log_path}" "${plugin_path}"

if [[ -n "${instance_name}" ]]; then
  echo "正在部署实例：${instance_name}（端口 ${service_port}）"
else
  echo "正在部署默认实例（端口 ${service_port}）"
fi
echo "正在拉取 PostgreSQL 镜像..."
docker compose pull postgres

echo "正在构建并启动生产服务..."
bash ./docker-build.sh

wait_for_service

echo
echo "部署完成。"
echo "Compose 项目：${compose_project}"
echo "管理页面：http://服务器地址:${service_port}/management.html"
echo "查看日志：docker compose -p ${compose_project} logs -f --tail=200"
