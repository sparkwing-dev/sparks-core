#!/usr/bin/env bash
# Run shellcheck across every tracked shell script in the repo.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v shellcheck >/dev/null 2>&1; then
  echo "check-shell: shellcheck not installed (brew install shellcheck)" >&2
  exit 1
fi

mapfile -t scripts < <(git ls-files '*.sh' 2>/dev/null | sort -u)

# bin/ scripts often carry a shebang without the .sh suffix.
while IFS= read -r f; do
  [[ -z "$f" || ! -f "$f" ]] && continue
  case "$f" in
    *.sh) continue ;;
  esac
  head -c 64 "$f" 2>/dev/null | head -n1 | grep -qE '^#!.*\b(bash|sh)\b' && scripts+=("$f")
done < <(git ls-files | grep -E '^(bin|scripts)/' || true)

if [[ ${#scripts[@]} -gt 0 ]]; then
  mapfile -t scripts < <(printf '%s\n' "${scripts[@]}" | sort -u)
fi

if [[ ${#scripts[@]} -eq 0 ]]; then
  exit 0
fi

shellcheck --severity=warning --shell=bash "${scripts[@]}"
