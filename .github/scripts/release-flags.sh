#!/bin/sh
# release-flags.sh TAG prints gh release create's flags for TAG (BV-102,
# alpha releases): a tag with a hyphen (v0.5.0-alpha.1, v0.5.0-rc.1) is a
# prerelease and never «latest», so releases/latest stays the last stable
# one; any other tag is «latest», explicitly. ci.yml checks it on two
# example names.
case "$1" in
  *-*) echo "--prerelease --latest=false" ;;
  *) echo "--latest" ;;
esac
