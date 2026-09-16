#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

go build -o ./loginserver ./cmd/loginserver
exec ./loginserver -conf ./config/loginserver.yaml -logger ./config/tlog.yaml
