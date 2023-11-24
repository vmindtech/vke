package resource

import (
	"time"
)

type AppResource struct {
	App     string    `json:"app"`
	Env     string    `json:"env"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}
