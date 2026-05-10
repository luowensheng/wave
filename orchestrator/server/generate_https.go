package servers

import infrahttp "wave/infra/http"

// generateHTTPS delegates TLS certificate generation to the infra adapter.
func generateHTTPS(config *HTTPSConfig) error {
	return infrahttp.GenerateSelfSigned(infrahttp.TLSConfig{
		SSLCertfile:  config.SSLCertfile,
		SSLKeyfile:   config.SSLKeyfile,
		Organization: config.Organization,
		DNSNames:     config.DNSNames,
		CommonName:   config.CommonName,
	})
}
