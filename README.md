# URL Shortener

A production-grade URL shortening service built in Go. 

**Live at:** [url-shortener-production-b02f.up.railway.app](https://url-shortener-production-b02f.up.railway.app)  
**GitHub:** [github.com/ieneshsharmaug24-png/url-shortener](https://github.com/ieneshsharmaug24-png/url-shortener)

## Features

- **URL Shortening** — create short links with randomly generated codes
- **Redirects** — visit a short code and get redirected to the original URL
- **Authentication** — JWT-based signup and login with bcrypt password hashing
- **Refresh Tokens** — 7-day refresh tokens with immediate revocation on logout
- **Authorization** — users can only delete their own links
- **Redis Caching** — redirect lookups cached in Redis, reducing response time from 140ms to 0.5ms (280x improvement)
- **Click Analytics** — async click tracking via background goroutines with stats endpoint
- **Rate Limiting** — fixed-window rate limiting on auth endpoints (5 req/min) to prevent brute force
- **Structured Logging** — JSON logs via Uber's zap library
- **Prometheus Metrics** — request rate, p99 latency, error rate, memory usage
- **Grafana Dashboard** — real-time production monitoring dashboard
- **Docker** — fully containerized with Docker Compose
- **CI/CD** — GitHub Actions pipeline running tests on every push
- **Integration Tests** — real PostgreSQL and Redis via testcontainers-go

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 |
| Web Framework | Gin |
| Database | PostgreSQL 17 |
| Cache | Redis 7 |
| Auth | JWT (HS256) + bcrypt + Refresh Tokens |
| Containerization | Docker + Docker Compose |
| Logging | Uber Zap (structured JSON) |
| Metrics | Prometheus + Grafana Cloud |
| Testing | testcontainers-go + testify |
| CI/CD | GitHub Actions |
| Deployment | Railway |

## Architecture
Users (worldwide)
↓
CDN (edge caching)
↓
Load Balancer
↓
Go App Servers (stateless, horizontally scalable)
↓                          ↓
Redis Cluster              Prometheus/Grafana
(116,000 reads/sec)        (real-time monitoring)
↓
PostgreSQL Primary + Read Replicas
(1,200 writes/sec — source of truth)

Designed to scale to 1 billion users with a 100:1 read/write ratio.

## API Endpoints

### Authentication
POST /signup          — create a new account
POST /login           — login, returns access token (15min) + refresh token (7 days)
POST /refresh         — get new access token using refresh token
POST /logout          — invalidate refresh token (requires auth)

### URL Management
POST   /shorten       — create a short link (requires auth)
GET    /:code         — redirect to original URL
GET    /urls          — list all links for logged-in user (requires auth)
GET    /stats/:code   — get click count for a link
DELETE /urls/:code    — delete a link (requires auth, must be owner)

### System
GET /health           — health check
GET /metrics          — Prometheus metrics endpoint
GET /debug/pprof/     — Go profiling endpoint

## Running Locally

### Prerequisites
- Go 1.26+
- Docker Desktop

### With Docker Compose (recommended)

```bash
# Clone the repo
git clone https://github.com/ieneshsharmaug24-png/url-shortener.git
cd url-shortener

# Create .env file
cp .env.example .env
# Edit .env with your values

# Start everything
docker compose up --build
```

App runs at `http://localhost:8080`

### Without Docker

```bash
# Start Redis (WSL on Windows)
wsl sudo service redis-server start

# Set environment variables
export DATABASE_URL=postgres://postgres:yourpassword@localhost:5432/url_shortener?sslmode=disable
export JWT_SECRET=your_jwt_secret
export REDIS_URL=localhost:6379

# Run
go run .
```

## Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `DATABASE_URL` | PostgreSQL connection string | `postgres://postgres:pass@localhost:5432/url_shortener?sslmode=disable` |
| `JWT_SECRET` | Secret key for signing JWTs | `your_random_secret_here` |
| `REDIS_URL` | Redis address | `localhost:6379` |
| `BASE_URL` | Public base URL for short links | `https://your-domain.com` |
| `PORT` | Port to listen on (set by Railway automatically) | `8080` |

## Example Usage

### Sign up
```bash
curl -X POST https://url-shortener-production-b02f.up.railway.app/signup \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"yourpassword"}'
```

### Login (returns access + refresh token)
```bash
curl -X POST https://url-shortener-production-b02f.up.railway.app/login \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"yourpassword"}'
```

### Shorten a URL
```bash
curl -X POST https://url-shortener-production-b02f.up.railway.app/shorten \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ACCESS_TOKEN" \
  -d '{"url":"https://google.com"}'
```

### List your links
```bash
curl https://url-shortener-production-b02f.up.railway.app/urls \
  -H "Authorization: Bearer YOUR_ACCESS_TOKEN"
```

### Visit a short link
https://url-shortener-production-b02f.up.railway.app/YOUR_CODE

### Refresh your access token
```bash
curl -X POST https://url-shortener-production-b02f.up.railway.app/refresh \
  -H "Content-Type: application/json" \
  -d '{"refresh_token":"YOUR_REFRESH_TOKEN"}'
```

### Logout
```bash
curl -X POST https://url-shortener-production-b02f.up.railway.app/logout \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_ACCESS_TOKEN" \
  -d '{"refresh_token":"YOUR_REFRESH_TOKEN"}'
```

## Running Tests

```bash
# Unit tests only (fast, no Docker needed)
go test ./... -run "TestGenerateCode" -v

# All tests including integration (requires Docker Desktop)
go test ./... -v -timeout 120s
```

## Performance

Load tested with `hey` (500 requests, 50 concurrent workers):

| Endpoint | p50 | p99 | Requests/sec |
|----------|-----|-----|-------------|
| `/health` | 347ms | 680ms | 125 |
| `/:code` (Redis cached) | 354ms | 887ms | 97 |

Server-side processing latency (measured via Prometheus): **~8ms p99**

Baseline latency dominated by Railway's free tier infrastructure. On a dedicated VPS, end-to-end p99 would drop below 100ms.

## Monitoring

Live Grafana dashboard tracking:
- **Request Rate** — requests per second across all endpoints
- **p99 Latency** — 99th percentile response time
- **Error Rate** — 4xx and 5xx error rates
- **Memory Usage** — real-time RAM consumption

Metrics exposed at `/metrics` in Prometheus format, scraped every 15 seconds by Grafana Alloy.

## Database Schema

```sql
users          — user accounts with bcrypt hashed passwords
urls           — short codes mapped to original URLs with owner reference
clicks         — click events for analytics (written asynchronously)
refresh_tokens — valid refresh tokens with expiry for secure logout
```

## CI/CD

GitHub Actions pipeline runs on every push to `main`:
1. Run unit tests
2. Run integration tests (real Postgres + Redis via testcontainers)
3. Build Docker image (only if tests pass)

## Project Structure
url-shortener/
├── .github/
│   └── workflows/
│       └── ci.yml           — GitHub Actions CI pipeline
├── cmd/
├── migrations/
│   └── 001_init.sql         — database schema
├── main.go                  — app entry point, router setup, DB/Redis init
├── handlers.go              — HTTP handlers (signup, login, URLs, refresh, logout)
├── middleware.go            — JWT auth middleware
├── ratelimit.go             — Redis-based rate limiting middleware
├── logger.go                — Zap structured logging middleware
├── metrics.go               — Prometheus metrics middleware
├── handlers_test.go         — unit tests
├── integration_test.go      — integration tests with testcontainers
├── Dockerfile               — multi-stage build (~15MB final image)
├── Dockerfile.alloy         — Grafana Alloy metrics collector
├── docker-compose.yml       — local dev orchestration
├── alloy-config.alloy       — Alloy scrape + remote write config
└── README.md

## System Design

This system is designed to scale to 1 billion users:

- **100:1 read/write ratio** — 116,000 reads/sec vs 1,200 writes/sec
- **CDN** — caches popular redirects at edge locations globally
- **Redis cluster** — absorbs the majority of read traffic in ~0.5ms
- **PostgreSQL primary + read replicas** — handles writes and cache misses
- **Stateless Go app servers** — horizontally scalable behind a load balancer
- **Snowflake IDs** — distributed unique ID generation at scale (vs current random codes)
- **Graceful degradation** — CDN down → Redis serves. Redis down → Postgres serves. One app server down → load balancer routes to others.
