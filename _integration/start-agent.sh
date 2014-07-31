#!/bin/bash
#
# start harpoon-agent on integration test port. The script assumes that it is
# called from its own mount namespace.
#
#     sudo unshare -m ./start-agent.sh

tmpdir=$(dirname "$0")/tmp

mkdir -p $tmpdir/run
mkdir -p $tmpdir/srv

mount --bind $tmpdir/run /run
mount --bind $tmpdir/srv /srv

exec harpoon-agent -addr :7777
