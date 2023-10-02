package localizer

import (
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

func InitLocalizer(defaultLang language.Tag, languages []language.Tag) *i18n.Bundle {
	bundle := i18n.NewBundle(defaultLang)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	for _, l := range languages {
		bundle.MustLoadMessageFile(fmt.Sprintf("locale/active.%s.toml", l.String()))
	}

	return bundle
}
