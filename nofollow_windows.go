//go:build windows

package main

// oNoFollow is 0 on Windows: O_NOFOLLOW does not exist there, and the pidfile
// lives under a per-user %LOCALAPPDATA% directory we create with restrictive
// permissions.
const oNoFollow = 0
