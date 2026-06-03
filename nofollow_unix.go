//go:build !windows

package main

import "syscall"

// oNoFollow makes open(2) refuse to follow a symlink at the final path
// component, so a symlink planted at the pidfile path is rejected rather than
// silently followed.
const oNoFollow = syscall.O_NOFOLLOW
