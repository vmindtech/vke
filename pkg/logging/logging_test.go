package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoggerConsoleAndFallback(t *testing.T) {
	for _, cfg := range []*LogstashConfig{nil, {Host: "127.0.0.1", Port: 0}, {Host: "invalid:::address", Port: 1234}} {
		l := NewLogger(Config{Service: ServiceConfig{Env: "test", AppName: "vke"}, Logstash: cfg})
		var buf bytes.Buffer
		l.SetOutput(&buf)
		l.Info("ready")
		var got map[string]any
		require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
		assert.Equal(t, "ready", got["message"])
		assert.Equal(t, "test", got["env"])
		assert.Equal(t, "vke", got["serviceName"])
		assert.NotEmpty(t, got["timestamp"])
		assert.NotEmpty(t, got["func"])
		assert.NotContains(t, got, "msg")
	}
	assert.Equal(t, logrus.AllLevels, NewServiceHook(ServiceConfig{}).Levels())
}

func TestLogstashDelivery(t *testing.T) {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	require.NoError(t, err)
	defer listener.Close()
	addr := listener.LocalAddr().(*net.UDPAddr)
	l := NewLogger(Config{Service: ServiceConfig{Env: "test", AppName: "vke"}, Logstash: &LogstashConfig{Host: "127.0.0.1", Port: addr.Port}})
	assert.Equal(t, io.Discard, l.Out)
	var udpHook *LogstashHook
	for _, h := range l.Hooks[logrus.InfoLevel] {
		if h, ok := h.(*LogstashHook); ok {
			udpHook = h
		}
	}
	require.NotNil(t, udpHook)
	defer udpHook.conn.Close()
	assert.Equal(t, logrus.AllLevels, udpHook.Levels())
	l.WithField("clusterUUID", "cid").WithError(errors.New("test failure")).Info("event")
	require.NoError(t, listener.SetReadDeadline(time.Now().Add(time.Second)))
	data := make([]byte, 8192)
	n, _, err := listener.ReadFromUDP(data)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data[:n], &doc))
	assert.Equal(t, "event", doc["message"])
	assert.Equal(t, "info", doc["level"])
	assert.Equal(t, "test failure", doc["error"].(map[string]any)["message"])
	assert.Equal(t, "cid", doc["fields"].(map[string]any)["clusterUUID"])
	require.NoError(t, udpHook.conn.Close())
	assert.NoError(t, udpHook.Fire(&logrus.Entry{Time: time.Now(), Data: logrus.Fields{}, Level: logrus.InfoLevel}), "logging failures must not break the caller")
}

func TestLogstashErrors(t *testing.T) {
	_, err := NewLogstashClient(LogstashConfig{Host: "invalid:::address", Port: 1234})
	require.ErrorContains(t, err, "failed to resolve")
	assert.JSONEq(t, `{"a":1}`, mustMarshal(map[string]int{"a": 1}))
	assert.Panics(t, func() { mustMarshal(make(chan int)) })
}
