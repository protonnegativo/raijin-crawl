// Package raijincrawl holds the engine versions raijin-crawl was tested with (engines.json).
package raijincrawl

import (
	_ "embed"
	"encoding/json"
)

//go:embed engines.json
var enginesJSON []byte

// Engine is a tested engine version and where to install it from.
type Engine struct {
	Version string `json:"version"`
	Go      string `json:"go,omitempty"`
	Pip     string `json:"pip,omitempty"`
	System  string `json:"system,omitempty"`
}

// Install is the command that installs the tested version.
func (e Engine) Install() string {
	switch {
	case e.Go != "":
		return "go install " + e.Go + "@v" + e.Version
	case e.Pip != "":
		return "pipx install " + e.Pip + "==" + e.Version
	case e.System != "":
		return "install " + e.System + " " + e.Version + " with the system package manager"
	}
	return ""
}

// Engines are the tested engine versions, by engine name. The bench fixtures were made with them.
func Engines() (map[string]Engine, error) {
	var m map[string]Engine
	err := json.Unmarshal(enginesJSON, &m)
	return m, err
}
