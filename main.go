package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var dbPool *pgxpool.Pool
var redisClient *redis.Client
var logger *zap.Logger

type ShortenRequest struct {
	URL string `json:"url" binding:"required"`
}

func generateCode() string {
	bytes := make([]byte, 4)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func main() {
	var err error

	// 1. Initialize logger first — everything else may need to log
	logger, err = zap.NewProduction()
	if err != nil {
		log.Fatal("Failed to initialize logger:", err)
	}
	defer logger.Sync()

	// 2. Load .env
	if err := godotenv.Load(); err != nil {
		logger.Warn("No .env file found, relying on system environment variables")
	}

	// 3. Connect to Postgres
	connString := os.Getenv("DATABASE_URL")
	if connString == "" {
		logger.Fatal("DATABASE_URL environment variable is not set")
	}

	dbPool, err = pgxpool.New(context.Background(), connString)
	if err != nil {
		logger.Fatal("Unable to connect to database", zap.Error(err))
	}
	defer dbPool.Close()

	// 4. Connect to Redis
	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr: redisAddr,
		DB:   0,
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		logger.Fatal("Unable to connect to Redis", zap.Error(err))
	}
	logger.Info("Connected to Redis")

	// 5. Set up router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(ZapLogger(logger))
	router.Use(gin.Recovery())

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.POST("/shorten", RateLimiter(20, time.Minute), AuthMiddleware(), func(c *gin.Context) {
		var req ShortenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User not authenticated"})
			return
		}

		userIDFloat, ok := userID.(float64)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID"})
			return
		}

		userIDInt := int(userIDFloat)
		code := generateCode()

		_, err := dbPool.Exec(
			context.Background(),
			`INSERT INTO urls (code, original_url, user_id) VALUES ($1, $2, $3)`,
			code,
			req.URL,
			userIDInt,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save url"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"code":      code,
			"short_url": "http://localhost:8080/" + code,
		})
	})

	router.GET("/:code", func(c *gin.Context) {
		code := c.Param("code")

		cachedURL, err := redisClient.Get(context.Background(), code).Result()
		if err == nil {
			go func() {
				var urlID int64
				err := dbPool.QueryRow(context.Background(),
					"SELECT id FROM urls WHERE code = $1", code).Scan(&urlID)
				if err == nil {
					trackClick(urlID)
				}
			}()
			c.Redirect(http.StatusFound, cachedURL)
			return
		}

		var originalURL string
		var urlID int64
		err = dbPool.QueryRow(context.Background(),
			"SELECT id, original_url FROM urls WHERE code = $1", code).Scan(&urlID, &originalURL)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "short URL not found"})
			return
		}

		redisClient.Set(context.Background(), code, originalURL, 24*time.Hour)
		go trackClick(urlID)
		c.Redirect(http.StatusFound, originalURL)
	})

	router.POST("/signup", RateLimiter(5, time.Minute), SignupHandler)
	router.POST("/login", RateLimiter(5, time.Minute), LoginHandler)
	router.DELETE("/urls/:code", RateLimiter(10, time.Minute), AuthMiddleware(), DeleteURLHandler)
	router.GET("/stats/:code", RateLimiter(10, time.Minute), StatsHandler)

	router.Run(":8080")
}
