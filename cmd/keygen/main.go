package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const (
	defaultOutDir = "secrets"
	defaultPrefix = "jwt_ed25519"
)

func main() {
	outDir := flag.String("dir", defaultOutDir, fmt.Sprintf("The directory to store keys. Default: %q", defaultOutDir))
	prefix := flag.String("prefix", defaultPrefix, fmt.Sprintf("The prefix of keys file name. Default: %q", defaultPrefix))
	flag.Parse()

	privPath := filepath.Join(*outDir, fmt.Sprintf("%s.key", *prefix))
	pubPath := filepath.Join(*outDir, fmt.Sprintf("%s.pub", *prefix))

	if fileExists(privPath) && fileExists(pubPath) {
		log.Printf("Keys existed in %s, skipping keygen", *outDir)
		os.Exit(0)
	}

	if err := os.MkdirAll(*outDir, 0700); err != nil {
		log.Fatalf("Create folder failed: %v", err)
	}

	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("Generate Ed25519 keys failed: %v", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		log.Fatalf("Marshal private key failed: %v", err)
	}
	privBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privDER,
	}

	pubDER, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		log.Fatalf("Marshal public key failed: %v", err)
	}
	pubBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubDER,
	}

	if err := os.WriteFile(privPath, pem.EncodeToMemory(privBlock), 0600); err != nil {
		log.Fatalf("Write private key to file failed: %v", err)
	}

	if err := os.WriteFile(pubPath, pem.EncodeToMemory(pubBlock), 0644); err != nil {
		log.Fatalf("Write private key to file failed: %v", err)
	}

	fmt.Println("✅ Generated Ed25519 key pair successfully:")
	fmt.Printf("  - Private key (PKCS#8): %s (0600)\n", privPath)
	fmt.Printf("  - Public key (PKIX):    %s (0644)\n", pubPath)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}
