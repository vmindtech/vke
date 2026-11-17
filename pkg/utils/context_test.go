package utils

import (
	"context"
	"testing"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/vmindtech/vke/pkg/validation"
)

func TestTranslations(t *testing.T) {
	b := i18n.NewBundle(language.English)
	require.NoError(t, b.AddMessages(language.English, &i18n.Message{ID: "hello", Other: "Hello {{.Name}}"}, &i18n.Message{ID: "plain", Other: "Hello"}))
	ctx := context.WithValue(context.Background(), LocalizerKey, i18n.NewLocalizer(b, "en"))
	assert.Equal(t, "Hello VKE", TranslateByTemplateWithContext(ctx, "hello", map[string]interface{}{"Name": "VKE"}))
	assert.Equal(t, "Hello", TranslateByIDWithContext(ctx, "plain"))
	assert.Empty(t, TranslateByIDWithContext(ctx, "unknown"))
	assert.Empty(t, TranslateByTemplateWithContext(context.Background(), "hello", nil))
	assert.Empty(t, TranslateByIDWithContext(context.Background(), "plain"))
	assert.Equal(t, "en", GetLanguageWithContext())
}

func TestValidateWithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), ValidatorKey, validation.InitValidator())
	type input struct {
		Name string `json:"name" validate:"required"`
	}
	assert.Nil(t, ValidateWithContext(ctx, input{Name: "cluster"}))
	assert.Equal(t, map[string]string{"name": "name is a required field"}, ValidateWithContext(ctx, input{}))
}
