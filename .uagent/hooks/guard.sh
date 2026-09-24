#!/bin/sh
# PreToolUse hook: the tool call arrives as JSON on stdin. Exit 2 blocks it,
# and stderr becomes the error the model sees.
input=$(cat)
if printf '%s' "$input" | grep -Eq 'rm -rf /|git push (-f|--force)|git reset --hard'; then
	echo "blocked by .uagent/hooks/guard.sh: destructive command" >&2
	exit 2
fi
