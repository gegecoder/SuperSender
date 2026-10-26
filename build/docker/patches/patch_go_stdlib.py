#!/usr/bin/env python3
"""Patch the Go standard library for macOS 12 compatibility.

crypto/x509/internal/macos hard-imports SecTrustCopyCertificateChain, a
macOS 13+ symbol, via //go:cgo_import_dynamic. On macOS 12 dyld aborts the
process the first time an HTTPS connection verifies a certificate.

Go forbids mixing cgo files and Go assembly in one package, so the C shim
lives in a dedicated cgo-only internal package
(crypto/x509/internal/secchainshim) and the macos package (which keeps its
assembly trampolines) calls into it. The pristine upstream implementation is
preserved for CGO_ENABLED=0 builds via !cgo tagged copies.

Usage: patch_go_stdlib.py <GOROOT>
"""
import os
import re
import shutil
import sys

X509_INTERNAL = os.path.join("src", "crypto", "x509", "internal")
PKG_REL = os.path.join(X509_INTERNAL, "macos")
SHIM_PKG = os.path.join(X509_INTERNAL, "secchainshim")

CGO_HEADER = "//go:build darwin && cgo\n\n"
NO_CGO_HEADER = "//go:build darwin && !cgo\n\n"

# In cgo builds: import the shim package and route the chain call through it.
SHIM_IMPORT = '"crypto/x509/internal/secchainshim"\n'

NEW_FUNC = """func SecTrustCopyCertificateChain(trustObj CFRef) (CFRef, error) {
\t// SecTrustCopyCertificateChain is macOS 13+; a hard dynamic import makes
\t// dyld abort on macOS 12. The shim resolves it via dlsym and falls back to
\t// SecTrustGetCertificate{Count,AtIndex} on older systems.
\tp := secchainshim.CopyCertificateChain(uintptr(trustObj))
\tif p == 0 {
\t\treturn 0, errors.New("x509: SecTrustCopyCertificateChain returned NULL")
\t}
\treturn CFRef(p), nil
}
"""

OLD_FUNC_RE = re.compile(
    r'//go:cgo_import_dynamic x509_SecTrustCopyCertificateChain '
    r'SecTrustCopyCertificateChain "[^"]*"\n\n'
    r'func SecTrustCopyCertificateChain\(trustObj CFRef\) \(CFRef, error\) \{.*?'
    r'func x509_SecTrustCopyCertificateChain_trampoline\(\)\n',
    re.DOTALL,
)

OLD_TRAMPOLINE = (
    "TEXT \u00b7x509_SecTrustCopyCertificateChain_trampoline(SB),NOSPLIT,$0-0\n"
    "\tJMP x509_SecTrustCopyCertificateChain(SB)\n"
)

SHIM_GO = """// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin && cgo

// Package secchainshim provides SecTrustCopyCertificateChain on macOS 13+
// with a pre-13 fallback, hiding the version-gated symbol behind dlsym so
// that no strong link-time reference reaches the final binary.
package secchainshim

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
// The implementation lives in sec_chain_shim.c in this package directory;
// cgo compiles C sources in the package automatically. Declaring (rather than
// #include-ing the .c) avoids defining the symbol in every generated object.
#include <stdint.h>
extern void *xinSecTrustCopyCertificateChain(uintptr_t trust_obj);
*/
import "C"

// CopyCertificateChain returns a CFArrayRef of SecCertificateRef (0 on error),
// matching SecTrustCopyCertificateChain semantics on every supported macOS.
func CopyCertificateChain(trust uintptr) uintptr {
	return uintptr(C.xinSecTrustCopyCertificateChain(C.uintptr_t(trust)))
}
"""


def fail(msg):
    print(f"patch_go_stdlib: ERROR: {msg}", file=sys.stderr)
    sys.exit(1)


def write(path, content):
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(content)


def main():
    if len(sys.argv) != 2:
        fail("usage: patch_go_stdlib.py <GOROOT>")
    root = sys.argv[1]
    pkg = os.path.join(root, PKG_REL)
    if not os.path.isdir(pkg):
        fail(f"package directory not found: {pkg}")

    here = os.path.dirname(os.path.abspath(__file__))
    shim_src = os.path.join(here, "sec_chain_shim.c")
    if not os.path.isfile(shim_src):
        fail(f"missing shim source: {shim_src}")

    sec_go_path = os.path.join(pkg, "security.go")
    sec_s_path = os.path.join(pkg, "security.s")
    with open(sec_go_path, encoding="utf-8") as f:
        sec_go = f.read()
    with open(sec_s_path, encoding="utf-8") as f:
        sec_s = f.read()

    if "//go:build darwin\n\npackage macos\n" not in sec_go:
        fail("unexpected security.go header layout")
    if not OLD_FUNC_RE.search(sec_go):
        fail("cannot locate SecTrustCopyCertificateChain block in security.go")
    if OLD_TRAMPOLINE not in sec_s:
        fail("cannot locate SecTrustCopyCertificateChain trampoline in security.s")

    # Pristine copies for CGO_ENABLED=0 (Go upstream behaviour).
    write(os.path.join(pkg, "security_nocgo.go"),
          sec_go.replace("//go:build darwin\n", NO_CGO_HEADER, 1))
    write(os.path.join(pkg, "security_nocgo.s"),
          sec_s.replace("//go:build darwin\n", NO_CGO_HEADER, 1))

    # cgo variant: drop the hard dynamic import and call the shim package.
    cgo_go = sec_go.replace("//go:build darwin\n", CGO_HEADER, 1)
    if "import (\n" not in cgo_go:
        fail("cannot locate import block in security.go")
    cgo_go = cgo_go.replace("import (\n", "import (\n\t" + SHIM_IMPORT, 1)
    cgo_go, n = OLD_FUNC_RE.subn(NEW_FUNC, cgo_go, count=1)
    if n != 1:
        fail("failed to substitute SecTrustCopyCertificateChain implementation")
    write(sec_go_path, cgo_go)

    cgo_s = sec_s.replace("//go:build darwin\n", CGO_HEADER, 1)
    cgo_s = cgo_s.replace(OLD_TRAMPOLINE, "", 1)
    write(sec_s_path, cgo_s)

    # Dedicated cgo-only shim package (own directory: no assembly conflict).
    shim_pkg_abs = os.path.join(root, SHIM_PKG)
    os.makedirs(shim_pkg_abs, exist_ok=True)
    write(os.path.join(shim_pkg_abs, "secchain.go"), SHIM_GO)
    with open(shim_src, "rb") as f:
        data = f.read().replace(b"\r\n", b"\n")
    with open(os.path.join(shim_pkg_abs, "sec_chain_shim.c"), "wb") as f:
        f.write(data)

    print("patch_go_stdlib: patched crypto/x509 for macOS 12 "
          "(SecTrustCopyCertificateChain shim via secchainshim package)")


if __name__ == "__main__":
    main()
