package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type SignupRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type URLResponse struct {
	ID          int       `json:"id"`
	Code        string    `json:"code"`
	OriginalURL string    `json:"original_url"`
	CreatedAt   time.Time `json:"created_at"`
}

func SignupHandler(c *gin.Context) {
	var req SignupRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request body",
		})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to hash password",
		})
		return
	}

	_, err = dbPool.Exec(
		context.Background(),
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)`,
		req.Email,
		string(hashedPassword),
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "User created successfully",
	})
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func LoginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	var userID int
	var storedHash string

	err := dbPool.QueryRow(
		context.Background(),
		`SELECT user_id, password_hash FROM users WHERE email = $1`,
		req.Email,
	).Scan(&userID, &storedHash)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.Password))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	token, err := generateJWT(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": token,
	})
}

func generateJWT(userID int) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(15 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	secret := os.Getenv("JWT_SECRET")
	return token.SignedString([]byte(secret))
}

func DeleteURLHandler(c *gin.Context) {
	code := c.Param("code")

	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	userIDFloat, ok := userID.(float64)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID"})
		return
	}

	userIDInt := int(userIDFloat)

	var ownerID int
	err := dbPool.QueryRow(
		context.Background(),
		`SELECT user_id FROM urls WHERE code = $1`,
		code,
	).Scan(&ownerID)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
		return
	}

	if ownerID != userIDInt {
		c.JSON(http.StatusForbidden, gin.H{"error": "You do not own this URL"})
		return
	}

	_, err = dbPool.Exec(
		context.Background(),
		`DELETE FROM urls WHERE code = $1`,
		code,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete URL"})
		return
	}

	redisClient.Del(context.Background(), code)

	c.JSON(http.StatusNoContent, nil)
}

func trackClick(urlID int64) {
	_, err := dbPool.Exec(
		context.Background(),
		`INSERT INTO clicks (url_id) VALUES ($1)`,
		urlID,
	)
	if err != nil {
		logger.Error("Failed to track click",
			zap.Error(err),
			zap.Int64("url_id", urlID),
		)
	}
}

func StatsHandler(c *gin.Context) {
	code := c.Param("code")

	var urlID int64
	err := dbPool.QueryRow(
		context.Background(),
		"SELECT id FROM urls WHERE code = $1",
		code,
	).Scan(&urlID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
		return
	}

	var clickCount int
	err = dbPool.QueryRow(
		context.Background(),
		"SELECT COUNT(*) FROM clicks WHERE url_id = $1",
		urlID,
	).Scan(&clickCount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get stats"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":        code,
		"click_count": clickCount,
	})
}

func GetURLsHandler(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	userIDFloat, ok := userID.(float64)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid user ID"})
		return
	}
	userIDInt := int(userIDFloat)

	rows, err := dbPool.Query(
		context.Background(),
		`SELECT id, code, original_url, created_at
		 FROM urls
		 WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userIDInt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch URLs"})
		return
	}
	defer rows.Close()

	var urls []URLResponse
	for rows.Next() {
		var url URLResponse
		err := rows.Scan(
			&url.ID,
			&url.Code,
			&url.OriginalURL,
			&url.CreatedAt,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read URLs"})
			return
		}
		urls = append(urls, url)
	}

	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if urls == nil {
		urls = []URLResponse{}
	}

	c.JSON(http.StatusOK, urls)
}
