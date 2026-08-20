#!/usr/bin/env bash
# env-guard.sh —— 每次执行 docker compose 前跑一次，保证容器拿到的是 .env 最新值。
#
# 背景：Docker Compose 变量优先级是 shell 环境变量 > .env 文件。
# 如果终端里残留旧 key（曾手动 export、或 GUI 进程继承），会静默覆盖 .env。
# 本脚本：unset 项目相关变量 → 重新从 .env 加载 → 校验三处一致。
#
# 用法：
#   source scripts/env-guard.sh          # 在当前 shell 生效（推荐）
#   bash scripts/env-guard.sh && docker compose up -d
#
# 注意：不用 set -e —— source 模式下任何失败会连累调用者整个 shell 退出。
# BASH_SOURCE[0] 在 source/执行两种模式下都指向本脚本路径；$0 只在直接执行时可靠。
ENV_GUARD_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." && pwd)" || {
  echo "[env-guard] ⚠️ 无法定位项目根目录" >&2
  return 1 2>/dev/null || exit 1
}
cd "$ENV_GUARD_ROOT" || {
  echo "[env-guard] ⚠️ 无法进入项目根目录 $ENV_GUARD_ROOT" >&2
  return 1 2>/dev/null || exit 1
}

PROJECT_VARS="JWT_SECRET GLM_API_KEY XUAN_API_KEY DEEPSEEK_KEY LITELLM_MASTER_KEY \
LLM_BASE_URL LLM_API_KEY LLM_MODEL LITELLM_INTERPRETER_MODEL LITELLM_MENTOR_MODEL \
XUAN_MODEL XUAN_BASE_URL CORS_ALLOWED_ORIGINS HIDDENWORLD_INTERPRETER_PROVIDER \
HIDDENWORLD_MENTOR_PROVIDER EMBEDDING_API_KEY EMBEDDING_BASE_URL"

echo "[env-guard] 清除 shell 残留的项目变量..."
for v in $PROJECT_VARS; do
  unset "$v" 2>/dev/null || true
done

if [ ! -f .env ]; then
  echo "[env-guard] ⚠️ 未找到 .env，变量保持未设置状态"
  return 0 2>/dev/null || exit 0
fi

echo "[env-guard] 从 .env 重新加载..."
set -a
# shellcheck disable=SC1091
source <(sed '1s/^\xef\xbb\xbf//' .env)   # 防御：剥离可能存在的 UTF-8 BOM（编辑器常写入）
set +a

echo "[env-guard] ✅ 生效的关键值（截断显示）："
for k in GLM_API_KEY XUAN_API_KEY LITELLM_MASTER_KEY; do
  v="${!k}"
  if [ -n "$v" ]; then
    echo "  $k = ${v:0:10}...(len=${#v})"
  else
    echo "  $k = (空)"
  fi
done

echo "[env-guard] 提示：如需让容器 100% 生效，请执行："
echo "  docker compose up -d --force-recreate litellm agent api"
