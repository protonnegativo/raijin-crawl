#!/usr/bin/env bash
# Tags HEAD with the next release and creates its GitHub Release.
# Usage: release.sh [patch|minor|major] [base]
# With base (the commit before a push), it skips when the push changed no file the binary is built from.
# Needs GH_TOKEN and a checkout with the tags (fetch-depth: 0).
set -euo pipefail

bump=${1:-patch}
base=${2:-}

if [ -n "$(git tag --points-at HEAD 'v*')" ]; then
  echo "HEAD is already tagged: $(git tag --points-at HEAD 'v*')"
  exit 0
fi

# fixtures, docs and workflows do not change the binary; engines.json does (it is embedded)
if [ -n "$base" ] && ! [[ "$base" =~ ^0+$ ]] && git cat-file -e "$base^{commit}" 2>/dev/null; then
  if ! git diff --name-only "$base" HEAD | grep -qvE '^(bench/fixtures/|docs/|\.github/)|\.md$'; then
    echo "only fixtures, docs or workflows changed since $base; no release"
    exit 0
  fi
fi

last=$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -1 || true)
if [ -z "$last" ]; then
  next=v0.1.0
else
  IFS=. read -r major minor patch <<< "${last#v}"
  case "$bump" in
    major) next="v$((major + 1)).0.0" ;;
    minor) next="v$major.$((minor + 1)).0" ;;
    patch) next="v$major.$minor.$((patch + 1))" ;;
    *) echo "unknown bump: $bump" >&2; exit 1 ;;
  esac
fi

git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
  tag -a "$next" -m "$next"
git push origin "$next"
gh release create "$next" --generate-notes --title "$next"
# a tag and a Release made with GITHUB_TOKEN start no workflow, so start the binaries build by hand
gh workflow run goreleaser.yml -f tag="$next"
echo "released $next"
