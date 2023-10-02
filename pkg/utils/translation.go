package utils

import (
	"context"

	"github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/vmindtech/vke/pkg/constants"
)

var (
	TranslateByTemplateWithContextFunc = TranslateByTemplateWithContext
	TranslateByIDWithContextFunc       = TranslateByIDWithContext
)

func TranslateByTemplateWithContext(ctx context.Context, msgID string, template map[string]interface{}) string {
	if l, ok := ctx.Value(LocalizerKey).(*i18n.Localizer); ok {
		msg, _ := l.Localize(&i18n.LocalizeConfig{
			MessageID:    msgID,
			TemplateData: template,
		})
		return msg
	}
	return ""
}

func TranslateByIDWithContext(ctx context.Context, msgID string) string {
	l, ok := ctx.Value(LocalizerKey).(*i18n.Localizer)
	if ok {
		msg, _ := l.LocalizeMessage(&i18n.Message{
			ID: msgID,
		})

		return msg
	}

	return ""
}

func GetLanguageWithContext() string {
	return constants.EnglishLanguage
}
