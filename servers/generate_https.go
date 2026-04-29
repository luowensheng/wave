package servers

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func generateHTTPS(config *HTTPSConfig) error {
	log.Println("Generating HTTPS config")

	if config.SSLCertfile == "" {
		return fmt.Errorf("missing 'ssl_certfile' path")
	}
	if config.SSLKeyfile == "" {
		return fmt.Errorf("missing 'ssl_keyfile' path")
	}

	// Ensure certificate and key directories exist
	certDir := filepath.Dir(config.SSLCertfile)
	keyDir := filepath.Dir(config.SSLKeyfile)
	log.Printf("Creating certificate directory: %s", certDir)
	if err := os.MkdirAll(certDir, 0755); err != nil {
		log.Printf("Failed to create certificate directory %s: %v", certDir, err)
		return err
	}
	log.Printf("Creating key directory: %s", keyDir)
	if err := os.MkdirAll(keyDir, 0755); err != nil {
		log.Printf("Failed to create key directory %s: %v", keyDir, err)
		return err
	}

	// Get local IP addresses
	log.Println("Retrieving local IP addresses")
	ips, err := getLocalIPs()
	if err != nil {
		log.Printf("Failed to retrieve local IP addresses: %v", err)
		return err
	}
	log.Printf("Local IP addresses: %v", ips)

	// Generate RSA private key (2048-bit)
	log.Println("Generating 2048-bit RSA private key")
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Printf("Failed to generate RSA private key: %v", err)
		return err
	}
	log.Println("RSA private key generated successfully")

	// Create certificate template
	log.Println("Creating certificate serial number")
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Printf("Failed to generate certificate serial number: %v", err)
		return err
	}

	organization := []string{}
	if len(config.Organization) == 0 {
		organization = append(organization, "Local Network")
		log.Println("Using default organization: 'Local Network'")
	} else {
		log.Printf("Using configured organization(s): %v", config.Organization)
		organization = config.Organization
	}

	dnsNames := []string{}
	if len(config.DNSNames) == 0 {
		dnsNames = append(dnsNames, "localhost", "*.local")
		log.Println("Using default DNS names: [localhost, *.local]")
	} else {
		dnsNames = config.DNSNames
		log.Printf("Using configured DNS names: %v", dnsNames)
	}

	commonName := strings.TrimSpace(config.CommonName)
	if commonName == "" {
		commonName = "localhost"
		log.Println("Using default CommonName: 'localhost'")
	} else {
		log.Printf("Using configured CommonName: %s", commonName)
	}

	log.Println("Building certificate template")
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: organization,
			CommonName:   commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(1, 0, 0), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           ips,
		DNSNames:              dnsNames,
	}

	// Create self-signed certificate
	log.Println("Creating self-signed certificate")
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		log.Printf("Failed to create self-signed certificate: %v", err)
		return err
	}
	log.Println("Self-signed certificate created successfully")

	// Write certificate to file
	log.Printf("Writing certificate to file: %s", config.SSLCertfile)
	certFile, err := os.Create(config.SSLCertfile)
	if err != nil {
		log.Printf("Failed to create certificate file %s: %v", config.SSLCertfile, err)
		return err
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	}); err != nil {
		log.Printf("Failed to encode certificate to PEM format: %v", err)
		return err
	}
	log.Printf("Certificate successfully written to %s", config.SSLCertfile)

	// Write private key to file
	log.Printf("Writing private key to file: %s", config.SSLKeyfile)
	keyFile, err := os.Create(config.SSLKeyfile)
	if err != nil {
		log.Printf("Failed to create private key file %s: %v", config.SSLKeyfile, err)
		return err
	}
	defer keyFile.Close()

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	if err := pem.Encode(keyFile, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privateKeyBytes,
	}); err != nil {
		log.Printf("Failed to encode private key to PEM format: %v", err)
		return err
	}
	log.Printf("Private key successfully written to %s", config.SSLKeyfile)

	log.Println("HTTPS certificate and key generation completed successfully")
	return nil
}

func getLocalIPs() ([]net.IP, error) {
	var ips []net.IP

	// Add loopback addresses
	ips = append(ips, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))

	// Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return ips, err
	}

	for _, iface := range interfaces {
		// Skip down and loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip != nil && !ip.IsLoopback() {
				ips = append(ips, ip)
			}
		}
	}

	return ips, nil
}
