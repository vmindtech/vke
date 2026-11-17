package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidate(t *testing.T) {
	type input struct {
		Name  string `json:"name" validate:"required"`
		Page  int    `query:"page" validate:"min=1"`
		Email string `json:"email" validate:"email"`
	}
	v := InitValidator()
	assert.Empty(t, v.Validate(input{Name: "cluster", Page: 1, Email: "test@example.com"}))
	assert.Equal(t, map[string]string{
		"name":  "name is a required field",
		"page":  "page must be 1 or greater",
		"email": "email must be a valid email address",
	}, v.Validate(input{Email: "invalid"}))
}
