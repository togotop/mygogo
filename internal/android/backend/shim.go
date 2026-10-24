//go:build android && cgo

package backend

/*
#include <sys/syscall.h>
#include <unistd.h>

// memfd_create is API 30, and this library is linked with BIND_NOW: a device
// whose libc does not export it fails the whole dlopen, and the app never
// starts. A vendor's compatibility layer may report API 31 and still be
// older inside.
//
// The syscall is older than the wrapper — Linux 3.17 — so this definition
// answers from it directly. Symbols resolve to this library first (it is
// linked -Bsymbolic), so a libc that has the wrapper is unaffected in
// behaviour: same syscall, same result.
int memfd_create(const char *name, unsigned int flags) {
	return (int)syscall(SYS_memfd_create, name, flags);
}
*/
import "C"
