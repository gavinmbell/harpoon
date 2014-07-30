#!/bin/bash
#
# installs harpoon commit hooks

function install() {
  hook=$1

  [ -f .git/hooks/$hook ] && {
    echo ".git/hooks/$hook exists" >&3;
    return 1;
  }

  ln -s ../../misc/hooks/$hook .git/hooks/$hook
}

[ -d .git ] || {
  echo ".git is not a directory; are you running from the project root?" >&2;
  exit 1;
}

install "pre-commit"
install "prepare-commit-msg"
