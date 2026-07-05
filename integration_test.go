package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

func setupTestEnvironment(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	// Connect to Postgres
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get postgres connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	dbPool = pool

	// Run migrations manually
	migrations := `
CREATE TABLE IF NOT EXISTS users (
    user_id SERIAL PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    password_hash VARCHAR(60) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS urls (
    id BIGSERIAL PRIMARY KEY,
    code VARCHAR(10) UNIQUE NOT NULL,
    original_url TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    user_id INTEGER REFERENCES users(user_id)
);
CREATE TABLE IF NOT EXISTS clicks (
    id BIGSERIAL PRIMARY KEY,
    url_id BIGINT NOT NULL REFERENCES urls(id),
    clicked_at TIMESTAMP NOT NULL DEFAULT NOW()
);`

	_, err = pool.Exec(ctx, migrations)
	if err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Start Redis container
	rdContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}

	// Connect to Redis
	rdAddr, err := rdContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("failed to get redis endpoint: %v", err)
	}
	redisClient = redis.NewClient(&redis.Options{Addr: rdAddr})

	// Initialize logger
	logger, _ = zap.NewDevelopment()

	// Set up router
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/signup", RateLimiter(100, time.Minute), SignupHandler)
	router.POST("/login", RateLimiter(100, time.Minute), LoginHandler)
	router.POST("/shorten", RateLimiter(100, time.Minute), AuthMiddleware(), func(c *gin.Context) {
		var req ShortenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		userID, _ := c.Get("user_id")
		userIDInt := int(userID.(float64))
		code := generateCode()
		_, err := dbPool.Exec(ctx,
			`INSERT INTO urls (code, original_url, user_id) VALUES ($1, $2, $3)`,
			code, req.URL, userIDInt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save url"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": code, "short_url": "http://localhost:8080/" + code})
	})

	cleanup := func() {
		pool.Close()
		pgContainer.Terminate(ctx)
		rdContainer.Terminate(ctx)
	}

	return router, cleanup
}

func TestSignup(t *testing.T) {
	router, cleanup := setupTestEnvironment(t)
	defer cleanup()

	body := `{"email":"test@example.com","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "User created successfully")
}

func TestSignupDuplicateEmail(t *testing.T) {
	router, cleanup := setupTestEnvironment(t)
	defer cleanup()

	body := `{"email":"dupe@example.com","password":"password123"}`

	// First signup
	req1 := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusCreated, w1.Code)

	// Second signup with same email
	req2 := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

func TestLogin(t *testing.T) {
	router, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// Sign up first
	signupBody := `{"email":"login@example.com","password":"password123"}`
	req1 := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(signupBody))
	req1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req1)

	// Now login
	loginBody := `{"email":"login@example.com","password":"password123"}`
	req2 := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(loginBody))
	req2.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req2)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)
	assert.NotEmpty(t, response["token"], "login should return a JWT token")
}

func TestLoginWrongPassword(t *testing.T) {
	router, cleanup := setupTestEnvironment(t)
	defer cleanup()

	// Sign up first
	signupBody := `{"email":"wrong@example.com","password":"correctpassword"}`
	req1 := httptest.NewRequest(http.MethodPost, "/signup", bytes.NewBufferString(signupBody))
	req1.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(httptest.NewRecorder(), req1)

	// Login with wrong password
	loginBody := `{"email":"wrong@example.com","password":"wrongpassword"}`
	req2 := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(loginBody))
	req2.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req2)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
