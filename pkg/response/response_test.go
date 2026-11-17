package response

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"

	"github.com/vmindtech/vke/pkg/utils"
)

func TestResponseSchemas(t *testing.T) {
	data := map[string]string{"cluster": "cid"}
	assert.Equal(t, data, NewSuccessResponse(data).Data)
	assert.Nil(t, NewSuccessResponse(nil).Data)
	assert.Equal(t, ErrorSchema{Code: utils.BodyParserErrCode, Message: utils.BodyParserMsg}, NewBodyParserErrorResponse().Error)
	assert.Equal(t, ErrorSchema{Code: utils.UnauthorizedErrCode, Message: utils.UnauthorizedMsg}, NewAuthorizationError().Error)
	got := NewValidationErrorResponse(map[string]string{"name": "required", "size": "too small"})
	assert.Equal(t, utils.ValidationErrCode, got.Error.Code)
	assert.Equal(t, utils.ValidationMsg, got.Error.Message)
	assert.ElementsMatch(t, []ErrorAttribute{{"name", "required"}, {"size", "too small"}}, got.Attributes)
	assert.Empty(t, NewValidationErrorResponse(nil).Attributes)
}

func TestErrorResponses(t *testing.T) {
	ctx := context.Background()
	unknown := errors.New("private database details")
	got := NewErrorResponse(ctx, unknown)
	assert.Equal(t, utils.UnexpectedErrCode, got.Error.Code)
	assert.Equal(t, utils.UnexpectedMsg, got.Error.Message)
	bag := utils.ErrorBag{Code: utils.ConflictErrCode, Cause: unknown}
	assert.Equal(t, "conflict", NewErrorResponse(ctx, bag, "conflict", "ignored").Error.Message)
	b := i18n.NewBundle(language.English)
	require.NoError(t, b.AddMessages(language.English, &i18n.Message{ID: utils.ConflictErrCode, Other: "Already exists"}))
	ctx = context.WithValue(ctx, utils.LocalizerKey, i18n.NewLocalizer(b, "en"))
	assert.Equal(t, "Already exists", NewErrorResponse(ctx, bag).Error.Message)
	for _, tc := range []struct {
		name          string
		err           error
		code, message string
	}{
		{"typed", bag, utils.ConflictErrCode, "conflict"},
		{"untyped", unknown, utils.UnexpectedErrCode, utils.UnexpectedMsg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			r := NewErrorResponseWithDetails(tc.err, "conflict", "cid", "ng", "project").Error
			assert.Equal(t, tc.code, r.Code)
			assert.Equal(t, tc.message, r.Message)
			assert.Equal(t, "cid", r.ClusterUUID)
			assert.Equal(t, "ng", r.NodeGroupUUID)
			assert.Equal(t, "project", r.ProjectUUID)
			assert.False(t, r.Date.Before(start))
			assert.False(t, r.Date.After(time.Now()))
		})
	}
}
