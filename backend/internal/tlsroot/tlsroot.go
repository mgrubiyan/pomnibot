// Package tlsroot provides the certificate pool for outgoing HTTPS clients.
//
// MAX and GigaChat serve certificates issued by the Russian Trusted Sub CA,
// which chains to the Russian Trusted Root CA (НУЦ Минцифры). That root is not
// in the default system stores, so it is bundled here and added on top of the
// system pool.
package tlsroot

import (
	"crypto/x509"
	_ "embed"
)

//go:embed rootca.pem
var rootCAPEM []byte

// Pool returns the system cert pool extended with the Russian Trusted Root CA.
// Each call returns a fresh pool, so callers may add their own roots to it.
func Pool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	pool.AppendCertsFromPEM(rootCAPEM)
	return pool
}
