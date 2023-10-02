package validation

import (
	"reflect"

	"github.com/go-playground/locales/en"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	enTranslation "github.com/go-playground/validator/v10/translations/en"
)

const (
	englishTranslatorCode = "en"
)

type IValidator interface {
	Validate(i interface{}) map[string]string
}

type validation struct {
	validator  *validator.Validate
	translator ut.Translator
}

func InitValidator() IValidator {
	v := validator.New()
	enLocale := en.New()
	universal := ut.New(enLocale, enLocale)
	translator, _ := universal.GetTranslator(englishTranslatorCode)

	_ = enTranslation.RegisterDefaultTranslations(v, translator)

	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		if jsonField := field.Tag.Get("json"); jsonField != "" {
			return jsonField
		}

		return field.Tag.Get("query")
	})

	return &validation{
		validator:  v,
		translator: translator,
	}
}

func (v *validation) Validate(i interface{}) map[string]string {
	messages := make(map[string]string)
	if errors := v.validator.Struct(i); errors != nil {
		for _, err := range errors.(validator.ValidationErrors) {
			messages[err.Field()] = err.Translate(v.translator)
		}
	}

	return messages
}
