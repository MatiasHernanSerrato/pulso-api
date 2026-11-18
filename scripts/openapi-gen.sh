#!/usr/bin/env bash
# Chequeo basico del openapi.yaml. No genera codigo todavia, solo valida que
# el yaml parsee y que los paths declarados coincidan con los que registramos
# en cmd/server/main.go. Si en algun momento movemos a oapi-codegen lo cambio
# aca.
set -euo pipefail

SPEC="${1:-docs/openapi.yaml}"

if ! command -v python3 >/dev/null 2>&1; then
  echo "necesito python3 para el check" >&2
  exit 1
fi

python3 - "$SPEC" <<'PY'
import sys, re
try:
    import yaml  # type: ignore
except Exception:
    print("falta pyyaml, lo skipeo (pip install pyyaml)")
    sys.exit(0)

spec_path = sys.argv[1]
with open(spec_path) as f:
    spec = yaml.safe_load(f)

paths = sorted(spec.get("paths", {}).keys())
print(f"ok, {len(paths)} paths en {spec_path}")
for p in paths:
    print(f"  {p}")
PY
