#!/bin/sh
# Validates the macOS 12 x509 shim: compiles a tiny TLS client for both darwin
# arches with the patched stdlib and checks the final binary no longer carries
# a direct undefined reference to SecTrustCopyCertificateChain.
set -e
WORK=/tmp/xtest
rm -rf "$WORK"
mkdir -p "$WORK"
cd "$WORK"

cat > main.go <<'EOF'
package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	c := &http.Client{Timeout: 5 * time.Second}
	r, err := c.Get("https://example.com")
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	fmt.Println("TLS OK", r.Proto)
	r.Body.Close()
}
EOF
go mod init xtest >/dev/null 2>&1 || true

export CGO_ENABLED=1
export CGO_CFLAGS=-w
# Test binary keeps DWARF (we need the symbol table for scanning), which makes
# the darwin external linker invoke dsymutil. Production builds use -s -w and
# never hit this; provide a no-op stub for the container test.
mkdir -p /tmp/fakebin
printf '#!/bin/sh\nexit 0\n' > /tmp/fakebin/dsymutil
printf '#!/bin/sh\nexit 0\n' > /tmp/fakebin/strip
chmod +x /tmp/fakebin/dsymutil /tmp/fakebin/strip
export PATH="/tmp/fakebin:$PATH"
GOOS=darwin GOARCH=amd64 CC=zcc-darwin-amd64 go build -o xtest-amd64 .
echo BUILD_AMD64_OK
GOOS=darwin GOARCH=arm64 CC=zcc-darwin-arm64 go build -o xtest-arm64 .
echo BUILD_ARM64_OK

echo "==== undefined symbol scan (Mach-O parser) ===="
for f in xtest-amd64 xtest-arm64; do
	echo "-- $f"
	BIN="$f" python3 - <<'EOF'
import os, struct, sys
p = os.environ["BIN"]
d = open(p, "rb").read()
magic = struct.unpack_from("<I", d, 0)[0]
assert magic == 0xfeedfacf, hex(magic)
ncmds = struct.unpack_from("<I", d, 16)[0]
off = 32
symoff = nsyms = stroff = strsize = 0
for _ in range(ncmds):
    cmd, cmdsize = struct.unpack_from("<II", d, off)
    if cmd == 0x2:  # LC_SYMTAB
        symoff, nsyms, stroff, strsize = struct.unpack_from("<IIII", d, off + 8)
    off += cmdsize
bad = []
for i in range(nsyms):
    o = symoff + i * 16
    n_strx, n_type, n_sect = struct.unpack_from("<IBB", d, o)
    end = d.index(b"\x00", stroff + n_strx)
    name = d[stroff + n_strx:end].decode("utf-8", "replace")
    if n_sect == 0 and (n_type & 0x01) and name == "_SecTrustCopyCertificateChain":
        bad.append(name)
if bad:
    print("FAIL: undefined external import present:", bad)
    sys.exit(1)
print("OK: no undefined SecTrustCopyCertificateChain reference")
EOF
done
