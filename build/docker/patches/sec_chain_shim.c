// Compatibility shim for crypto/x509 certificate-chain extraction on macOS.
//
// SecTrustCopyCertificateChain only exists on macOS 13+. Go's standard
// library (crypto/x509/internal/macos) references it through a hard dynamic
// import, so dyld aborts the process on macOS 12 when the symbol is first
// bound. This shim resolves the symbol lazily via dlsym (which leaves no
// strong link-time reference) and falls back to the pre-13
// SecTrustGetCertificate{Count,AtIndex} pair, returning the same CFArray of
// SecCertificateRef that the Go verifier consumes.
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <dlfcn.h>
#include <stdint.h>

typedef CFArrayRef (*xin_sec_chain_fn)(SecTrustRef);

__attribute__((visibility("default")))
void *xinSecTrustCopyCertificateChain(uintptr_t trust_obj) {
    static xin_sec_chain_fn resolved = (xin_sec_chain_fn)1;
    if (resolved == (xin_sec_chain_fn)1) {
        resolved = (xin_sec_chain_fn)dlsym(RTLD_DEFAULT,
                                           "SecTrustCopyCertificateChain");
    }
    SecTrustRef trust = (SecTrustRef)trust_obj;
    if (resolved != NULL) {
        return (void *)resolved(trust);
    }

    CFIndex count = SecTrustGetCertificateCount(trust);
    CFMutableArrayRef chain = CFArrayCreateMutable(
        kCFAllocatorDefault, count, &kCFTypeArrayCallBacks);
    if (chain == NULL) {
        return NULL;
    }
    for (CFIndex i = 0; i < count; i++) {
        CFArrayAppendValue(
            chain, (const void *)SecTrustGetCertificateAtIndex(trust, i));
    }
    return (void *)chain;
}
