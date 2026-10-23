#!/usr/bin/env bash
# Sürümlü, gömülü arayüzlü ikilileri üretir.
#   scripts/build.sh                 → ./joe (bu makine, sürüm: dev)
#   scripts/build.sh v0.2.0 all      → dist/joe_v0.2.0_<os>_<arch>[.exe] + SHA256SUMS
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-dev}"
TARGETS="${2:-local}"
COMMIT="$(git rev-parse --short=12 HEAD 2>/dev/null || echo "")"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
PKG=github.com/MustafaKemal0146/joe/internal/version
LDFLAGS="-s -w -X ${PKG}.Version=${VERSION} -X ${PKG}.Commit=${COMMIT} -X ${PKG}.Date=${DATE}"

echo "→ arayüz derleniyor"
(cd web && pnpm install --frozen-lockfile && pnpm build)

if [ "$TARGETS" = "local" ]; then
  CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o joe ./cmd/joe
  echo "✓ ./joe ($VERSION)"
  exit 0
fi

mkdir -p dist
for t in linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64; do
  os="${t%/*}"; arch="${t#*/}"
  out="dist/joe_${VERSION}_${os}_${arch}"
  [ "$os" = "windows" ] && out="${out}.exe"
  echo "→ $os/$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" -o "$out" ./cmd/joe
done
(cd dist && sha256sum joe_* > SHA256SUMS)
echo "✓ dist/ hazır"
