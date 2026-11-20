#!/usr/bin/env bash
for c in "go version" "node -v" "npm -v" "git --version" "docker --version" "docker compose version"; do
  echo "> $c"; $c || echo "MISSING: $c"
done
