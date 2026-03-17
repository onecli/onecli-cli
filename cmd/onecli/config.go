package main

import (
	"github.com/onecli/onecli-cli/internal/config"
	"github.com/onecli/onecli-cli/pkg/output"
)

// ConfigCmd is the `onecli config` command group.
type ConfigCmd struct {
	Get ConfigGetCmd `cmd:"" help:"Get a config value."`
	Set ConfigSetCmd `cmd:"" help:"Set a config value."`
}

// ConfigGetCmd is `onecli config get <key>`.
type ConfigGetCmd struct {
	Key string `arg:"" required:"" help:"Config key to read. Keys: api-host."`
}

// ConfigGetResponse is the JSON output of config get.
type ConfigGetResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (c *ConfigGetCmd) Run(out *output.Writer) error {
	val, err := config.GetConfigValue(c.Key)
	if err != nil {
		return err
	}
	return out.Write(ConfigGetResponse{Key: c.Key, Value: val})
}

// ConfigSetCmd is `onecli config set <key> <value>`.
type ConfigSetCmd struct {
	Key   string `arg:"" required:"" help:"Config key to set. Keys: api-host."`
	Value string `arg:"" required:"" help:"Value to set."`
}

// ConfigSetResponse is the JSON output of config set.
type ConfigSetResponse struct {
	Status string `json:"status"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

func (c *ConfigSetCmd) Run(out *output.Writer) error {
	if err := config.SetConfigValue(c.Key, c.Value); err != nil {
		return err
	}
	return out.Write(ConfigSetResponse{Status: "ok", Key: c.Key, Value: c.Value})
}
