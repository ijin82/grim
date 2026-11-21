#!/usr/bin/env bash
set -euo pipefail

# Release helper: checks, version bump, build, install, commit, and tag.
# It never pushes; the required push commands are printed at the end.

usage() {
	echo "usage: $0 X.Y.Z" >&2
	exit 1
}

[ "$#" -eq 1 ] || usage

VERSION="${1#v}"
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "invalid semantic version: $1" >&2
	exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [ -n "$(git status --porcelain)" ]; then
	echo "working tree is not clean; commit or stash changes first" >&2
	exit 1
fi

if git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null; then
	echo "tag v$VERSION already exists" >&2
	exit 1
fi

echo "==> formatting"
UNFORMATTED="$(gofmt -l cmd pkg)"
if [ -n "$UNFORMATTED" ]; then
	echo "unformatted files:" >&2
	echo "$UNFORMATTED" >&2
	exit 1
fi

echo "==> vet"
go vet ./...

echo "==> tests"
go test ./...

echo "==> race tests"
go test -race ./...

echo "==> bump version to $VERSION"
perl -0pi -e "s/var Version = \"[^\"]+\"/var Version = \"$VERSION\"/" cmd/grim/main.go
grep -q "var Version = \"$VERSION\"" cmd/grim/main.go

echo "==> build"
go build -o grim ./cmd/grim

echo "==> install"
mkdir -p "$HOME/.local/bin"
install -m 755 grim "$HOME/.local/bin/grim"
if [ -d "$HOME/go/bin" ]; then
	install -m 755 grim "$HOME/go/bin/grim"
fi
"$HOME/.local/bin/grim" --version

echo "==> commit and tag"
git add cmd/grim/main.go
git commit -m "chore: bump version to v$VERSION"
git tag -a "v$VERSION" -m "Grim v$VERSION"

echo
echo "Release v$VERSION is ready locally."
echo "Push with:"
echo "  git push origin master --tags"
echo "  git push origin-gv master --tags"
echo "  git push origin-wm master --tags"
