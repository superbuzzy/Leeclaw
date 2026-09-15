#!/usr/bin/env sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)

for name in openclaw weknora openviking; do
  repo="$root/upstream/$name"
  if [ ! -d "$repo" ]; then
    echo "MISSING: $repo" >&2
    exit 1
  fi
  repo_top="$(git -C "$repo" rev-parse --show-toplevel 2>/dev/null || true)"
  if [ "$repo_top" = "$repo" ]; then
    if [ -n "$(git -C "$repo" status --porcelain)" ]; then
      echo "DIRTY: $repo" >&2
      git -C "$repo" status --short
      exit 1
    fi
    echo "CLEAN: $repo"
  else
    case "$name" in
      openclaw) archive="$root/../openclaw-main.zip"; archive_root="openclaw-main" ;;
      weknora) archive="$root/../WeKnora-main.zip"; archive_root="WeKnora-main" ;;
      openviking) archive="$root/../OpenViking-main.zip"; archive_root="OpenViking-main" ;;
    esac
    [ -f "$archive" ] || { echo "FAIL: no Git metadata or baseline archive for $repo" >&2; exit 1; }
    tmp="$(mktemp -d /tmp/leeclaw-upstream-check.XXXXXX)"
    trap 'rm -rf "$tmp"' EXIT HUP INT TERM
    unzip -qq "$archive" -d "$tmp"
    if ! diff -qr --exclude=.git "$tmp/$archive_root" "$repo" >/dev/null; then
      echo "DIRTY SNAPSHOT: $repo differs from $archive" >&2
      diff -qr --exclude=.git "$tmp/$archive_root" "$repo" | head -50 >&2
      exit 1
    fi
    rm -rf "$tmp"; trap - EXIT HUP INT TERM
    echo "CLEAN SNAPSHOT: $repo matches $archive"
  fi
done
