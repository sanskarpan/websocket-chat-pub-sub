# Deployment

Three deployment targets: local Docker Compose, production Docker, and Kubernetes.

---

## Local development (Docker Compose)

Docker Compose brings up PostgreSQL and Redis. The Go server runs on your host.

```bash
# Start infrastructure
docker compose up -d

# Generate JWT keys (once)
bash scripts/generate_keys.sh

# Run the server
go run cmd/server/main.go
```

The `docker-compose.yaml` at the repo root exposes:

| Service | Port |
|---|---|
| PostgreSQL | `5432` |
| Redis | `6379` |

---

## Production Docker

### Build

Multi-stage Dockerfile produces a minimal binary-only image:

```bash
docker build -t websocket-chat:latest .
```

The `Dockerfile` uses `golang:1.23-alpine` for the build stage and `scratch` / `alpine` for the runtime stage — keeping the image lean.

### Run

```bash
docker run \
  --name websocket-chat \
  -p 8085:8085 \
  -p 8086:8086 \
  -p 9090:9090 \
  -e APP_ENVIRONMENT=production \
  -e DB_PASSWORD="your-db-password" \
  -e DB_HOST="your-postgres-host" \
  -e REDIS_ADDR="your-redis-host:6379" \
  -e JWT_PRIVATE_KEY="$(cat configs/jwt-private.pem)" \
  -e JWT_PUBLIC_KEY="$(cat configs/jwt-public.pem)" \
  websocket-chat:latest
```

Or use a secrets manager and pass keys as env vars from a mounted volume.

---

## Kubernetes

Manifests are in `deployments/kubernetes/`.

### Apply

```bash
kubectl apply -f deployments/kubernetes/
kubectl get pods,services
```

### Deployment overview

```yaml
# deployments/kubernetes/deployment.yaml (excerpt)
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: server
        image: websocket-chat:latest
        ports:
        - containerPort: 8085
          name: rest
        - containerPort: 8086
          name: websocket
        - containerPort: 9090
          name: metrics
        env:
        - name: APP_ENVIRONMENT
          value: "production"
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: chat-secrets
              key: db-password
        - name: JWT_PRIVATE_KEY
          valueFrom:
            secretKeyRef:
              name: chat-secrets
              key: jwt-private-key
        - name: JWT_PUBLIC_KEY
          valueFrom:
            secretKeyRef:
              name: chat-secrets
              key: jwt-public-key
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8085
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8085
          initialDelaySeconds: 5
          periodSeconds: 5
```

### Create secrets

```bash
kubectl create secret generic chat-secrets \
  --from-literal=db-password="your-db-password" \
  --from-file=jwt-private-key=configs/jwt-private.pem \
  --from-file=jwt-public-key=configs/jwt-public.pem
```

### No sticky sessions required

Because Redis Pub/Sub fans events to all pods, clients can connect to any pod and receive all room messages. Do **not** configure sticky sessions on your load balancer — it limits horizontal scaling and creates uneven pod load.

### Rolling updates

The server handles `SIGTERM` gracefully with a 30-second drain. Set `terminationGracePeriodSeconds` to at least `35` to give the server time to finish:

```yaml
spec:
  template:
    spec:
      terminationGracePeriodSeconds: 35
```

Rolling update strategy — allows updating one pod at a time with no downtime:

```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxSurge: 1
    maxUnavailable: 0
```

---

## TLS

Terminate TLS at the ingress layer (Nginx, Traefik, or a cloud LB). The application does not handle TLS directly.

Example Nginx upstream block for WebSocket proxying:

```nginx
upstream ws_backend {
    server pod1:8086;
    server pod2:8086;
    server pod3:8086;
}

server {
    listen 443 ssl;
    ssl_certificate     /etc/ssl/certs/cert.pem;
    ssl_certificate_key /etc/ssl/private/key.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    location /ws {
        proxy_pass http://ws_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 3600s;
    }

    location / {
        proxy_pass http://rest_backend:8085;
    }
}
```

---

## Database migrations

SQL migrations live in `migrations/`. Run them before starting the server for the first time, or after any migration added during an upgrade:

```bash
# Using psql directly
psql -h localhost -U chat -d chat -f migrations/001_init.sql
```

Or wire them into an init container in Kubernetes.

---

## Environment variable summary

| Variable | Required | Description |
|---|---|---|
| `APP_ENVIRONMENT` | yes (prod) | Set to `production` to enforce strict config validation |
| `JWT_PRIVATE_KEY` | yes (prod) | RSA-2048 private key PEM content |
| `JWT_PUBLIC_KEY` | yes (prod) | RSA-2048 public key PEM content |
| `DB_PASSWORD` | yes (prod) | PostgreSQL password |
| `DB_HOST` | no | PostgreSQL host (default: `localhost`) |
| `REDIS_ADDR` | no | Redis address (default: `localhost:6379`) |
| `REDIS_PASSWORD` | no | Redis password (default: empty) |
| `SERVER_PORT` | no | REST API port (default: `8085`) |
