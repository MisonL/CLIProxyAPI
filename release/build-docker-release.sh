#!/usr/bin/env bash
set -euo pipefail

VERSION=${1:-}
if [[ -z "$VERSION" ]]; then
  echo "usage: ./release/build-docker-release.sh <version>" >&2
  exit 1
fi

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
SRC_DIR="$ROOT_DIR/release/docker"
DIST_ROOT="$ROOT_DIR/dist"
TARGET_DIR="$DIST_ROOT/docker-release/cliproxyapi-release"
ARCHIVE_NAME="CLIProxyAPI_docker_release_${VERSION#v}.tar.gz"
ARCHIVE_PATH="$DIST_ROOT/$ARCHIVE_NAME"

rm -rf "$TARGET_DIR"
mkdir -p "$TARGET_DIR"

python3 - "$SRC_DIR" "$TARGET_DIR" "$VERSION" <<'PY'
from pathlib import Path
from shutil import copy2, copytree
import sys

src = Path(sys.argv[1])
dst = Path(sys.argv[2])
version = sys.argv[3]

for child in src.iterdir():
    target = dst / child.name
    if child.is_dir():
        copytree(child, target, dirs_exist_ok=True)
    else:
        copy2(child, target)

for path in dst.rglob('*'):
    if not path.is_file():
        continue
    try:
        text = path.read_text(encoding='utf-8')
    except UnicodeDecodeError:
        continue
    if '__VERSION__' in text:
        path.write_text(text.replace('__VERSION__', version), encoding='utf-8')
PY

(
  cd "$DIST_ROOT/docker-release"
  rm -f "$ARCHIVE_PATH"
  tar -czf "$ARCHIVE_PATH" cliproxyapi-release
)

printf 'built %s\n' "$ARCHIVE_PATH"
