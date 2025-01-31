package agent

import (
	"crypto/sha256"
	"math"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hashicorp/memberlist"

	"github.com/james-lawrence/bw"
	"github.com/james-lawrence/bw/internal/systemx"
)

// ConfigClientOption options for the client configuration.
type ConfigClientOption func(*ConfigClient)

// CCOptionTLSConfig tls set to load for the client configuration.
func CCOptionTLSConfig(name string) ConfigClientOption {
	return ConfigClientTLS(name)
}

// CCOptionInsecure insecure tls configuration
func CCOptionInsecure(b bool) ConfigClientOption {
	return func(c *ConfigClient) {
		c.Credentials.Insecure = b
	}
}

// CCOptionAddress set address for the configuration.
func CCOptionAddress(s string) ConfigClientOption {
	return func(c *ConfigClient) {
		c.Address = net.JoinHostPort(s, strconv.Itoa(bw.DefaultP2PPort))
		c.ServerName = s
	}
}

// CCOptionDeployDataDir set the deployment configuration directory for the configuration.
func CCOptionDeployDataDir(s string) ConfigClientOption {
	return func(c *ConfigClient) {
		c.Deployment.DataDir = s
	}
}

// CCOptionConcurrency set the deployment configuration directory for the configuration.
func CCOptionConcurrency(d float64) ConfigClientOption {
	return func(c *ConfigClient) {
		c.Concurrency = d
	}
}

// CCOptionEnvironment set the environment string for the configuration.
func CCOptionEnvironment(s string) ConfigClientOption {
	return func(c *ConfigClient) {
		c.Environment = s
	}
}

// NewConfigClient ...
func NewConfigClient(template ConfigClient, options ...ConfigClientOption) ConfigClient {
	for _, opt := range options {
		opt(&template)
	}

	return template
}

func defaultDeployment() Deployment {
	return Deployment{
		Timeout: bw.DefaultDeployTimeout,
		DataDir: bw.DefaultDeployspaceDir,
	}
}

// DefaultConfigClient creates a default client configuration.
func DefaultConfigClient(options ...ConfigClientOption) ConfigClient {
	config := ConfigClient{
		Deployment: defaultDeployment(),
		Address:    systemx.HostnameOrLocalhost(),
	}

	ConfigClientTLS(bw.DefaultEnvironmentName)(&config)

	return NewConfigClient(config, options...)
}

// ExampleConfigClient creates an example configuration.
func ExampleConfigClient(options ...ConfigClientOption) ConfigClient {
	deploy := defaultDeployment()
	deploy.Prompt = "are you sure you want to deploy? (remove this field to disable the prompt)"
	config := ConfigClient{
		Deployment: deploy,
		Address:    systemx.HostnameOrLocalhost(),
	}

	ConfigClientTLS(bw.DefaultEnvironmentName)(&config)

	return NewConfigClient(config, options...)
}

type Deployment struct {
	DataDir   string        `yaml:"dir"`
	Timeout   time.Duration `yaml:"timeout"`
	Prompt    string        `yaml:"prompt"`  // used to prompt before a deploy is started, useful for deploying to sensitive systems like production.
	CommitRef string        `yaml:"treeish"` // used to populate commit information in the environment
}

// ConfigClient ...
type ConfigClient struct {
	root        string `yaml:"-"` // filepath of the configuration on disk.
	Address     string // cluster address
	Concurrency float64
	Deployment  Deployment `yaml:"deploy"`
	Credentials struct {
		Mode      string `yaml:"source"`
		Directory string `yaml:"directory"`
		Insecure  bool   `yaml:"-"`
	} `yaml:"credentials"`
	CA          string
	ServerName  string
	Environment string
}

// LoadConfig create a new configuration from the specified path using the current
// configuration as the default values for the new configuration.
func (t ConfigClient) LoadConfig(path string) (ConfigClient, error) {
	if err := bw.ExpandAndDecodeFile(path, &t); err != nil {
		return t, err
	}

	t.root = filepath.Dir(path)

	return t, nil
}

// Dir path to the configuration on disk
func (t ConfigClient) Dir() string {
	return t.root
}

func (t ConfigClient) Deployspace() string {
	cdir := t.Deployment.DataDir
	if !filepath.IsAbs(cdir) {
		cdir = filepath.Join(t.WorkDir(), t.Deployment.DataDir)
	}

	return cdir
}

func (t ConfigClient) WorkDir() string {
	return filepath.Dir(filepath.Dir(t.Dir()))
}

// Partitioner ...
func (t ConfigClient) Partitioner() (_ bw.Partitioner) {
	return bw.PartitionFromFloat64(t.Concurrency)
}

// NewConfig creates a default configuration.
func NewConfig(options ...ConfigOption) Config {
	c := Config{
		Name:              systemx.HostnameOrLocalhost(),
		Root:              bw.DefaultCacheDirectory(),
		KeepN:             3,
		SnapshotFrequency: time.Hour,
		MinimumNodes:      3,
		Bootstrap: bootstrap{
			Attempts: math.MaxInt32,
		},
		DNSBind: dnsBind{
			TTL:       60,
			Frequency: time.Hour,
		},
	}

	newTLSAgent(bw.DefaultEnvironmentName)(&c)

	for _, opt := range options {
		opt(&c)
	}

	return c
}

// ConfigOption - for overriding configurations.
type ConfigOption func(*Config)

// ConfigOptionCompose allow grouping together configuration options to be applied simultaneously.
func ConfigOptionCompose(options ...ConfigOption) ConfigOption {
	return func(c *Config) {
		for _, opt := range options {
			opt(c)
		}
	}
}

// ConfigOptionDefaultBind default connection bindings.
func ConfigOptionDefaultBind(ip net.IP) ConfigOption {
	return ConfigOptionCompose(
		ConfigOptionP2P(&net.TCPAddr{
			IP:   ip,
			Port: bw.DefaultP2PPort,
		}),
	)
}

// ConfigOptionP2P sets the address to bind.
func ConfigOptionP2P(p *net.TCPAddr) ConfigOption {
	return func(c *Config) {
		c.P2PBind = p
	}
}

// ConfigOptionAdvertised set the ip address to advertise.
func ConfigOptionAdvertised(ip *net.TCPAddr) ConfigOption {
	return func(c *Config) {
		c.P2PAdvertised = ip
	}
}

// ConfigOptionSecondaryBindings set additional ip/ports to bindings to use.
func ConfigOptionSecondaryBindings(alternates ...*net.TCPAddr) ConfigOption {
	return func(c *Config) {
		c.AlternateBinds = alternates
	}
}

// ConfigOptionName set the name of the agent.
func ConfigOptionName(name string) ConfigOption {
	return func(c *Config) {
		c.Name = name
	}
}

type bootstrap struct {
	Attempts         int    `yaml:"attempts"`
	ReadOnly         bool   `yaml:"readonly"`
	ArchiveDirectory string `yaml:"archiveDirectory"`
}

// Config - configuration for agent processes.
type Config struct {
	Name              string
	Root              string        // root directory to store long term data.
	KeepN             int           `yaml:"keepN"`
	MinimumNodes      int           `yaml:"minimumNodes"`
	Bootstrap         bootstrap     `yaml:"bootstrap"`
	SnapshotFrequency time.Duration `yaml:"snapshotFrequency"`
	P2PBind           *net.TCPAddr
	P2PAdvertised     *net.TCPAddr
	AlternateBinds    []*net.TCPAddr
	ClusterTokens     []string `yaml:"clusterTokens"`
	ServerName        string
	CA                string `yaml:"ca"`
	CredentialsMode   string `yaml:"credentialsSource"` // deprecated
	CredentialsDir    string `yaml:"credentialsDir"`    // deprecated
	Credentials       struct {
		Mode      string `yaml:"source"`
		Directory string `yaml:"directory"`
	} `yaml:"credentials"`
	DNSBind      dnsBind  `yaml:"dnsBind"`
	DNSBootstrap []string `yaml:"dnsBootstrap"`
	AWSBootstrap struct {
		AutoscalingGroups []string `yaml:"autoscalingGroups"` // additional autoscaling groups to check for instances.
	} `yaml:"awsBootstrap"`
}

func (t Config) Sanitize() Config {
	dup := t
	dup.ClusterTokens = []string{}
	return dup
}

// EnsureDefaults values after configuration load
func (t Config) EnsureDefaults() Config {
	if t.CredentialsDir == "" {
		t.CredentialsDir = filepath.Join(t.Root, bw.DefaultDirAgentCredentials)
	}

	if t.Credentials.Directory == "" {
		t.Credentials.Directory = filepath.Join(t.Root, bw.DefaultDirAgentCredentials)
	}

	if t.CA == "" {
		t.CA = filepath.Join(t.CredentialsDir, bw.DefaultTLSCertCA)
	}

	if t.P2PAdvertised == nil {
		t.P2PAdvertised = t.P2PBind
	}

	return t
}

type dnsBind struct {
	TTL       uint32 // TTL for the generated records.
	Frequency time.Duration
}

// Clone the config applying any provided options.
func (t Config) Clone(options ...ConfigOption) Config {
	for _, opt := range options {
		opt(&t)
	}

	return t
}

// Peer - builds the Peer information from the configuration.
func (t Config) Peer() *Peer {
	return &Peer{
		Status:  Peer_Node,
		Name:    t.Name,
		Ip:      t.P2PAdvertised.IP.String(),
		P2PPort: uint32(t.P2PAdvertised.Port),
	}
}

// Keyring - returns the hash of the Secret.
func (t Config) Keyring() (ring *memberlist.Keyring, err error) {
	var (
		tokens [][]byte
	)

	for _, token := range t.ClusterTokens {
		hashed := sha256.Sum256([]byte(token))
		tokens = append(tokens, hashed[:])
	}

	switch len(tokens) {
	case 0:
		hashed := sha256.Sum256([]byte(t.ServerName))
		return memberlist.NewKeyring([][]byte{}, hashed[:])
	case 1:
		return memberlist.NewKeyring([][]byte{}, tokens[0])
	default:
		return memberlist.NewKeyring(tokens[1:], tokens[0])
	}
}
