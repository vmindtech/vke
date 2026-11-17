package localizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
)

func TestInitLocalizer(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "locale"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "locale", "active.en.toml"), []byte("[hello]\nother = \"Hello {{.Name}}\"\n"), 0600))
	b := InitLocalizer(language.English, []language.Tag{language.English})
	got, err := i18n.NewLocalizer(b, "en").Localize(&i18n.LocalizeConfig{MessageID: "hello", TemplateData: map[string]string{"Name": "VKE"}})
	require.NoError(t, err)
	assert.Equal(t, "Hello VKE", got)
	assert.Panics(t, func() { InitLocalizer(language.English, []language.Tag{language.Turkish}) })
}
