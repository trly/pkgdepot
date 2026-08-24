#!/bin/sh

set -eu

usage() {
	printf 'Usage: %s BASE_URL REPOSITORY ARCHITECTURE\n' "$0" >&2
	printf '\nBASE_URL should be the pkgdepot origin, for example https://packages.example.com\n' >&2
}

if [ "$#" -ne 3 ]; then
	usage
	exit 2
fi

base_url=${1%/}
repository=$2
architecture=$3
data_root=${PKGDEPOT_DATA_ROOT:-/var/lib/pkgdepot}
destination="$data_root/repositories/$repository/$architecture"
source_url="$base_url/repos/$repository/$architecture"

case "$repository/$architecture" in
	*/*/*|*" "*)
		printf 'invalid repository or architecture\n' >&2
		exit 2
		;;
esac

if [ -e "$destination" ]; then
	printf 'destination already exists: %s\n' "$destination" >&2
	exit 1
fi

temporary=$(mktemp -d "${TMPDIR:-/tmp}/pkgdepot-bootstrap.XXXXXX")
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
staging="$temporary/repository"
mkdir -p "$staging"

database="$repository.db.tar.gz"
curl --fail --location --silent --show-error \
	"$source_url/$database" \
	-o "$temporary/$database"

cp "$temporary/$database" "$staging/$database"

package_list="$temporary/packages"
tar -xOzf "$temporary/$database" --wildcards '*/desc' 2>/dev/null \
	| awk '/^%FILENAME%$/{getline; print}' >"$package_list"

while IFS= read -r package; do
		[ -n "$package" ] || continue
		case "$package" in
			*/*|*" "*)
				printf 'invalid package filename in database: %s\n' "$package" >&2
				exit 1
				;;
		esac
	printf 'Downloading %s\n' "$package"
		curl --fail --location --silent --show-error \
			"$source_url/$package" \
			-o "$staging/$package"
done <"$package_list"

mkdir -p "$(dirname "$destination")"
mv "$staging" "$destination"
printf 'Bootstrapped %s/%s in %s\n' "$repository" "$architecture" "$destination"
