#!/usr/bin/env bash

set -euo pipefail

version="$(sed -n 's/^  "mlflow==\([^"]*\)",$/\1/p' infra/mlflow/pyproject.toml)"
if [[ ! ${version} =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	printf 'infra/mlflow/pyproject.toml must pin exactly one stable mlflow dependency\n' >&2
	exit 1
fi
printf '%s\n' "${version}"
