package logging

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/sirupsen/logrus"
)

type LogstashConfig struct {
	Host string
	Port int
}

type LogstashHook struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

func NewLogstashClient(config LogstashConfig) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", config.Host, config.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial UDP: %w", err)
	}

	return conn, nil
}

func NewLogstashHook(conn *net.UDPConn, addr *net.UDPAddr) *LogstashHook {
	return &LogstashHook{
		conn: conn,
		addr: addr,
	}
}

func (h *LogstashHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (h *LogstashHook) Fire(entry *logrus.Entry) error {
	doc := map[string]interface{}{
		"@timestamp": entry.Time.Format(time.RFC3339),
		"level":      entry.Level.String(),
		"message":    entry.Message,
		"fields":     entry.Data,
	}

	if err, ok := entry.Data["error"]; ok {
		if errObj, ok := err.(error); ok {
			doc["error"] = map[string]interface{}{
				"message": errObj.Error(),
				"type":    fmt.Sprintf("%T", errObj),
			}
		}
	}
	data := mustMarshal(doc)
	_, err := h.conn.Write([]byte(data))
	if err != nil {
		fmt.Printf("Failed to send log via UDP: %v\n", err)
	}
	return nil
}

func mustMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
