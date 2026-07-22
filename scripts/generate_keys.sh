#!/bin/bash
set -e

echo "Generating JWT keys for production..."

mkdir -p keys

if [ ! -f keys/jwt-private.pem ]; then
    openssl genrsa -out keys/jwt-private.pem 2048
    echo "Generated private key"
fi

if [ ! -f keys/jwt-public.pem ]; then
    openssl rsa -in keys/jwt-private.pem -pubout -out keys/jwt-public.pem
    echo "Generated public key"
fi

echo "JWT keys generated successfully"
ls -la keys/
