//go:build unix

package log

import "syscall"

const openNoFollow = syscall.O_NOFOLLOW
