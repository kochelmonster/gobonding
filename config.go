package gobonding

import (
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	// Name of the TUN adapters
	RouterTunName string
	ProxyTunName  string
	OrderWindow   int
	ProxyIP       string
	Channels      []string
	PrivateKey    string
	Certificate   string
	ReconnectTime string
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config := Config{}
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}
