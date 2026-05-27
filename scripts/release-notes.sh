#!/usr/bin/env sh
set -eu

version="${1:-}"
changelog="${2:-CHANGELOG.md}"

if [ -z "$version" ]; then
	echo "usage: scripts/release-notes.sh <version> [CHANGELOG.md]" >&2
	exit 2
fi

version="${version#v}"

extract_section() {
	awk -v heading="$1" '
BEGIN {
	in_section = 0;
	found = 0;
}
/^## \[/ {
	if (in_section) {
		exit 0;
	}
	if (index($0, heading) == 1) {
		in_section = 1;
		found = 1;
		next;
	}
}
in_section {
	print;
}
END {
	if (!found) {
		exit 1;
	}
}
' "$changelog"
}

if extract_section "## [$version]"; then
	exit 0
fi

echo "warning: $changelog has no [$version] section; using [Unreleased]" >&2
if extract_section "## [Unreleased]"; then
	exit 0
fi

echo "warning: $changelog has no [Unreleased] section; using git log fallback" >&2
previous_tag="$(git describe --tags --abbrev=0 --match 'v[0-9]*' HEAD^ 2>/dev/null || true)"
if [ -n "$previous_tag" ]; then
	echo "Changes since $previous_tag:"
	echo
	git log --pretty='- %s (%h)' "$previous_tag..HEAD"
	exit 0
fi

echo "Recent changes:"
echo
git log --pretty='- %s (%h)' -n 50 HEAD
