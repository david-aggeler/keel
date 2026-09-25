#!/usr/bin/env bash

# install_pinned_node provisions Node from the NodeSource channel selected by
# the pin block, then verifies the executable that apt placed on PATH.
# DHF-REQ: keel/requirement-173
install_pinned_node() {
	local expected="$1"
	echo "Installing Node.js major ${expected} from NodeSource..."
	curl -fsSL "https://deb.nodesource.com/setup_${expected}.x" | bash -
	apt-get install -y nodejs
	require_node_major "$expected"
}

# require_node_major verifies the executable that setup_as_root placed on PATH.
# DHF-REQ: keel/requirement-173
require_node_major() {
	local expected="$1" installed_version installed_major
	installed_version="$(node --version 2>/dev/null || true)"
	installed_major="${installed_version#v}"
	installed_major="${installed_major%%.*}"
	if [[ "$installed_major" != "$expected" ]]; then
		echo "ERROR: node major version mismatch: installed=${installed_major:-(none)} expected=${expected}" >&2
		return 1
	fi
	echo "Node installed: ${installed_version} (expected major ${expected})"
}
