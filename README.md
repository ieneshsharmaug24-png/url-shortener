# URL Shortener

A production-grade URL shortening service built in Go. Live at [url-shortener-production-b02f.up.railway.app](https://url-shortener-production-b02f.up.railway.app)

## Features

- **URL Shortening** — create short links with randomly generated codes
- **Redirects** — visit a short code and get redirected to the original URL
- **Authentication** — JWT-based signup and login with bcrypt password hashing
- **Authorization** — users can only delete their own links
- **Redis Caching** — redirect lookups cached in Redis, reducing response time from 140ms to 0.5ms
- **Click Analytics** — async click tracking via background goroutines with stats endpoint
- **Rate Limiting** — fixed-window rate limiting on auth endpoints (5 req/min) to prevent brute force
- **Structured Logging** — JSON logs via Uber's zap library
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
| Auth | JWT (HS256) + bcrypt |
| Containerization | Docker + Docker Compose |
| Logging | Uber Zap |
| Testing | testcontainers-go |
| CI/CD | GitHub Actions |
| Deployment | Railway |

## Architecture
Users
↓
CDN (edge caching)
↓
Load Balancer
↓
Go App Servers (stateless, horizontally scalable)
↓
Redis (116,000 reads/sec — cache layer)
↓
PostgreSQL (1,200 writes/sec — source of truth)

Designed to scale to 1 billion users with a 100:1 read/write ratio.

## API Endpoints

### Authentication

POST /signup          — create a new account
POST /login           — login and receive a JWT token

### URL Management
POST /shorten         — create a short link (requires auth)
GET  /:code           — redirect to original URL
GET  /stats/:code     — get click count for a link
DELETE /urls/:code    — delete a link (requires auth, must be owner)

### System
GET /health           — health check
GET /debug/pprof/     — Go profiling endpoint

## Running Locally

### Prerequisites
- Go 1.26+
- Docker Desktop
- WSL (Windows) or native Linux/Mac for Redis

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

### Login
```bash
curl -X POST https://url-shortener-production-b02f.up.railway.app/login \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"yourpassword"}'
```

### Shorten a URL
```bash
curl -X POST https://url-shortener-production-b02f.up.railway.app/shorten \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -d '{"url":"https://google.com"}'
```

### Visit a short link
https://url-shortener-production-b02f.up.railway.app/61acde86

### Get click stats
```bash
curl https://url-shortener-production-b02f.up.railway.app/stats/YOUR_CODE
```

## Running Tests

```bash
# Unit tests only (fast)
go test ./... -run "TestGenerateCode" -v

# All tests including integration (requires Docker)
go test ./... -v -timeout 120s
```

## Performance

Load tested with `hey` (500 requests, 50 concurrent workers):

| Endpoint | p50 | p99 | Requests/sec |
|----------|-----|-----|-------------|
| `/health` | 347ms | 680ms | 125 |
| `/:code` (cached) | 354ms | 887ms | 97 |

Baseline latency is dominated by Railway's free tier infrastructure. On a dedicated VPS, p99 would drop below 100ms.

## Database Schema

```sql
users       — user accounts with bcrypt hashed passwords
urls        — short codes mapped to original URLs with owner reference  
clicks      — click events for analytics (written asynchronously)
```

## CI/CD

GitHub Actions pipeline runs on every push to `main`:
1. Run unit tests
2. Run integration tests (real Postgres + Redis via testcontainers)
3. Build Docker image (only if tests pass)

## Project Structure
url-shortener/
├── main.go              — app entry point, router setup, DB/Redis init
├── handlers.go          — HTTP handlers (signup, login, delete)
├── middleware.go        — JWT auth middleware
├── ratelimit.go         — Redis-based rate limiting middleware
├── logger.go            — Zap structured logging middleware
├── handlers_test.go     — unit tests
├── integration_test.go  — integration tests with testcontainers
├── Dockerfile           — multi-stage build (~15MB final image)
├── docker-compose.yml   — local dev orchestration
└── migrations/
└── 001_init.sql     — database schema

