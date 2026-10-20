#!/bin/sh
# Writes manifest.json beside the binaries in DIR. The web UI reads it to list
# the downloads, so it needs no rebuild when the set of platforms changes.
#
#   ./manifest.sh DIR VERSION
set -eu
dir=$1
version=$2
cd "$dir"

sep=
{
    printf '{"version":"%s","files":[' "$version"
    for f in imapsync-mcp-*; do
        # imapsync-mcp-<os>-<arch>[.exe]
        platform=${f#imapsync-mcp-}
        platform=${platform%.exe}
        size=$(wc -c <"$f" | tr -d ' ')
        sum=$(shasum -a 256 "$f" | cut -d' ' -f1)
        printf '%s{"name":"%s","os":"%s","arch":"%s","size":%s,"sha256":"%s"}' \
            "$sep" "$f" "${platform%-*}" "${platform#*-}" "$size" "$sum"
        sep=,
    done
    printf ']'
    # The setup and usage guide, when the build put it beside the binaries.
    [ -f HUONG_DAN.md ] && printf ',"guide":"HUONG_DAN.md"'
    printf '}\n'
} >manifest.json
