package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateCode(t *testing.T) {
	code := generateCode()
	assert.Len(t, code, 8, "code should be 8 characters long")
	assert.Regexp(t, "^[0-9a-f]+$", code, "code should only contain hex characters")
}

func TestGenerateCodeUniqueness(t *testing.T) {
	codes := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		code := generateCode()
		assert.False(t, codes[code], "generated duplicate code: %s", code)
		codes[code] = true
	}
}
