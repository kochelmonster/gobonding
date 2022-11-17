package gobonding

import (
	"os"

	"gopkg.in/yaml.v2"
)

const (
	CONFFILE = "gobonding.yml"
)

type Config struct {
	// Name of the TUN adapter
	TunName string
	// If not empty the path to a monitor file
	MonitorPath string
	// The Tick interval to update the monitor file
	MonitorTick string
	// The delay a channel tries to reconnect to the proxy after a connection close
	ReconnectTime string
	// The Port the proxy is listening to
	ProxyPort int
	// A map ifacename or ipaddress of the wan device to ip address of proxy server
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
