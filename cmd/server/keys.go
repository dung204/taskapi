package main

import (
	"crypto/ed25519"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

func loadEd25519Keys(cfg jwtCfg) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	privPEM, err := os.ReadFile(cfg.privateKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load ed25519 keys: %w", err)
	}

	privKey, err := jwt.ParseEdPrivateKeyFromPEM(privPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("load ed25519 keys: %w", err)
	}

	pubPEM, err := os.ReadFile(cfg.publicKeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load ed25519 keys: %w", err)
	}

	pubKey, err := jwt.ParseEdPublicKeyFromPEM(pubPEM)
	if err != nil {
		return nil, nil, fmt.Errorf("load ed25519 keys: %w", err)
	}

	//nolint:errcheck // ParseEd*KeyFromPEM only return Ed25519 keys, so the assertions can't fail.
	return privKey.(ed25519.PrivateKey), pubKey.(ed25519.PublicKey), nil
}
