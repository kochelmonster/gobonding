package gobonding

import (
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	// Name of the TUN adapter
	TunName string
	// Size of the Buffer to order messages
	OrderWindow int
	// The delay a channel tries to reconnect to the proxy after a connection close
	ReconnectTime string
	// The Port the proxy is listening to
	ProxyPort int
	// A map ifacename or ipaddress to proxy name or ip address
	Channels map[string]string
	// Certificate of quic Connection
	Certificate string
	// Private key of certificate
	PrivateKey string
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
