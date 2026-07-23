#!/bin/bash
set -e

echo "Generating RSA 2048-bit JWT keys..."

KEY_DIR="configs"

mkdir -p "$KEY_DIR"

if [ ! -f "$KEY_DIR/jwt-private.pem" ]; then
    openssl genrsa -out "$KEY_DIR/jwt-private.pem" 2048
    echo "Generated private key: $KEY_DIR/jwt-private.pem"
fi

if [ ! -f "$KEY_DIR/jwt-public.pem" ]; then
    openssl rsa -in "$KEY_DIR/jwt-private.pem" -pubout -out "$KEY_DIR/jwt-public.pem"
    echo "Generated public key: $KEY_DIR/jwt-public.pem"
fi

chmod 600 "$KEY_DIR/jwt-private.pem"

echo "JWT keys generated successfully"
ls -la "$KEY_DIR"/jwt-*.pem
