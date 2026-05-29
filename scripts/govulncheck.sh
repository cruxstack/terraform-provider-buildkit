#!/usr/bin/env bash
#
# Runs govulncheck and fails on any vulnerability EXCEPT a small, explicit
# allowlist of findings that have no available fix and do not affect this
# provider.
#
# Allowlisted findings:
#   GO-2026-4887 - docker/docker AuthZ plugin bypass (daemon-side; no fixed
#                  version in the docker/docker module path).
#   GO-2026-4883 - docker/docker plugin privilege validation off-by-one
#                  (daemon-side; no fixed version in the docker/docker module
#                  path).
#
# This provider only uses github.com/docker/docker/client for daemon discovery
# and never exercises the affected server code paths. The upstream fixes live in
# github.com/moby/moby/v2, a different module path, so there is no dependency
# bump that resolves them today. Remove an entry from ALLOWLIST once a fix is
# reachable.

set -euo pipefail

ALLOWLIST=(
  "GO-2026-4887"
  "GO-2026-4883"
)

GOTOOLCHAIN="${GOTOOLCHAIN:-local}"
export GOTOOLCHAIN

# Collect the set of OSV IDs that govulncheck reports as affecting the code
# (i.e. with a call trace). -scan symbol (the default) only reports reachable
# vulnerabilities.
found_ids=$(govulncheck -format json ./... \
  | jq -r 'select(.finding != null) | .finding.osv' \
  | sort -u)

if [ -z "${found_ids}" ]; then
  echo "govulncheck: no vulnerabilities found"
  exit 0
fi

unexpected=()
while IFS= read -r id; do
  [ -z "${id}" ] && continue
  allowed=false
  for a in "${ALLOWLIST[@]}"; do
    if [ "${id}" = "${a}" ]; then
      allowed=true
      break
    fi
  done
  if [ "${allowed}" = true ]; then
    echo "govulncheck: ignoring allowlisted finding ${id}"
  else
    unexpected+=("${id}")
  fi
done <<< "${found_ids}"

if [ "${#unexpected[@]}" -gt 0 ]; then
  echo ""
  echo "govulncheck: found vulnerabilities not on the allowlist:"
  for id in "${unexpected[@]}"; do
    echo "  - ${id} (https://pkg.go.dev/vuln/${id})"
  done
  echo ""
  echo "Full report:"
  govulncheck ./... || true
  exit 1
fi

echo "govulncheck: only allowlisted findings remain"
