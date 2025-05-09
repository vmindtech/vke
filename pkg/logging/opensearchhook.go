package logging

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/opensearch-project/opensearch-go"
	"github.com/sirupsen/logrus"
)

type OpenSearchHook struct {
	client *opensearch.Client
	index  string
}

func NewOpenSearchClient(config OpenSearchConfig) (*opensearch.Client, error) {
	cfg := opensearch.Config{
		Addresses: config.Addresses,
		Username:  config.Username,
		Password:  config.Password,
	}

	return opensearch.NewClient(cfg)
}

func NewOpenSearchHook(client *opensearch.Client, index string) *OpenSearchHook {
	return &OpenSearchHook{
		client: client,
		index:  index,
	}
}

func (h *OpenSearchHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *OpenSearchHook) getIndexName() string {
	today := time.Now().Format("2006-01-02")
	return fmt.Sprintf("%s-%s", h.index, today)
}

func (h *OpenSearchHook) Fire(entry *logrus.Entry) error {
	doc := map[string]interface{}{
		"timestamp": entry.Time,
		"level":     entry.Level.String(),
		"message":   entry.Message,
		"fields":    entry.Data,
	}

	_, err := h.client.Index(
		h.getIndexName(),
		strings.NewReader(mustMarshal(doc)),
		h.client.Index.WithContext(entry.Context),
	)
	return err
}

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
