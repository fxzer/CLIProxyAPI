#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# CLIProxyAPI：从本仓库构建并部署到 Homebrew 安装的 cliproxyapi 服务（macOS）
#
# 典型场景：上游已合并新管理接口（如 /v0/management/usage），官方 bottle 尚未
# 带上时，用本地源码编译并覆盖 Cellar 内二进制，同时保持与 brew 相同的
# DefaultConfigPath（见 Homebrew formula 中的 -X main.DefaultConfigPath=...）。
#
# 用法：
#   ./scripts/deploy-brew-service.sh              # git pull + 编译 + 安装 + 重启服务
#   ./scripts/deploy-brew-service.sh --no-pull    # 不拉代码，仅编译部署
#   ./scripts/deploy-brew-service.sh --no-restart # 安装后不执行 brew services restart
#   ./scripts/deploy-brew-service.sh --dry-run    # 只打印将要执行的命令
#
# 依赖：git、go（与 go.mod 一致）、brew、已安装 cliproxyapi 公式
# -----------------------------------------------------------------------------
set -euo pipefail

NO_PULL=false
NO_RESTART=false
DRY_RUN=false

for arg in "$@"; do
  case "$arg" in
    --no-pull) NO_PULL=true ;;
    --no-restart) NO_RESTART=true ;;
    --dry-run) DRY_RUN=true ;;
    -h|--help)
      sed -n '1,25p' "$0" | tail -n +2
      exit 0
      ;;
    *)
      echo "未知参数: $arg" >&2
      exit 1
      ;;
  esac
done

run() {
  if [[ "$DRY_RUN" == true ]]; then
    printf '[dry-run] '; printf ' %q' "$@"; echo
  else
    "$@"
  fi
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

if ! command -v brew >/dev/null 2>&1; then
  echo "未找到 brew，请先安装 Homebrew。" >&2
  exit 1
fi

if ! brew list --formula cliproxyapi >/dev/null 2>&1; then
  echo "未安装 cliproxyapi 公式。请先: brew install cliproxyapi" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "未找到 go，请先安装与 go.mod 匹配的 Go。" >&2
  exit 1
fi

CONFIG_PATH="$(brew --prefix)/etc/cliproxyapi.conf"
OPT_BIN="$(brew --prefix cliproxyapi)/bin/cliproxyapi"

if [[ ! -e "$CONFIG_PATH" ]]; then
  echo "默认配置不存在: $CONFIG_PATH" >&2
  echo "若从未运行过 brew 安装，请先: brew reinstall cliproxyapi" >&2
  exit 1
fi

# 解析 Cellar 内真实二进制路径（避免误写坏 /opt/homebrew/bin 下的 symlink）
resolve_target() {
  python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$1"
}
INSTALL_BIN="$(resolve_target "$OPT_BIN")"

if [[ "$NO_PULL" != true ]]; then
  if [[ ! -d "$REPO_ROOT/.git" ]]; then
    echo "当前目录不是 git 仓库: $REPO_ROOT" >&2
    exit 1
  fi
  run git fetch --tags origin
  # 优先快进合并当前分支跟踪的远程分支
  if git rev-parse --abbrev-ref '@{u}' >/dev/null 2>&1; then
    run git pull --ff-only
  else
    echo "未设置上游分支，跳过 git pull（可手动 git pull 后加 --no-pull 再部署）。" >&2
  fi
fi

VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
BUILDDATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

LDFLAGS="-s -w"
LDFLAGS+=" -X main.Version=${VERSION}"
LDFLAGS+=" -X main.Commit=${COMMIT}"
LDFLAGS+=" -X main.BuildDate=${BUILDDATE}"
LDFLAGS+=" -X main.DefaultConfigPath=${CONFIG_PATH}"

OUT="$(mktemp -t cliproxyapi-build.XXXXXX)"
cleanup() { rm -f "$OUT"; }
trap cleanup EXIT

echo "仓库:     $REPO_ROOT"
echo "版本串:  $VERSION"
echo "提交:     $COMMIT"
echo "构建时间: $BUILDDATE"
echo "配置路径: $CONFIG_PATH"
echo "安装到:   $INSTALL_BIN"
echo ""

run go build -trimpath -ldflags "$LDFLAGS" -o "$OUT" ./cmd/server

if [[ "$DRY_RUN" != true ]]; then
  chmod +x "$OUT"
  # 原子替换：同分区 mv
  mv "$OUT" "$INSTALL_BIN"
  trap - EXIT
fi

if [[ "$NO_RESTART" != true ]]; then
  if brew services list 2>/dev/null | grep -q '^cliproxyapi[[:space:]]'; then
    run brew services restart cliproxyapi
  else
    echo "未检测到 brew services 中的 cliproxyapi，请自行启动服务。" >&2
  fi
else
  echo "已跳过 brew services restart（--no-restart）"
fi

echo ""
echo "完成。可用以下命令自检（无密钥时应为 401，不应为 404）："
echo "  curl -sS -o /dev/null -w '%{http_code}\\n' http://127.0.0.1:8317/v0/management/usage"
