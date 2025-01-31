package cmdopts

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/hashicorp/memberlist"
	"github.com/hashicorp/raft"
	"github.com/james-lawrence/bw"
	"github.com/james-lawrence/bw/agent"
	"github.com/james-lawrence/bw/agent/dialers"
	"github.com/james-lawrence/bw/certificatecache"
	"github.com/james-lawrence/bw/clustering"
	"github.com/james-lawrence/bw/clustering/peering"
	"github.com/james-lawrence/bw/clustering/raftutil"
	"github.com/james-lawrence/bw/cmd/commandutils"
	"github.com/james-lawrence/bw/internal/envx"
	"github.com/james-lawrence/bw/internal/errorsx"
	"github.com/james-lawrence/bw/internal/tlsx"
	"github.com/james-lawrence/bw/notary"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
)

type Global struct {
	Verbosity int                `help:"increase verbosity of logging" short:"v" type:"counter" default:"0"`
	Context   context.Context    `kong:"-"`
	Shutdown  context.CancelFunc `kong:"-"`
	Cleanup   *sync.WaitGroup    `kong:"-"`
}

func (t Global) BeforeApply() error {
	commandutils.LogEnv(t.Verbosity)
	return nil
}

type Peering struct {
	Bootstrap     []*net.TCPAddr `name:"bootstrap-static-addresses" help:"addresses of the cluster to bootstrap from" env:"${env_bw_agent_bootstrap_static}"`
	DNSEnabled    bool           `name:"bootstrap-dns-enable" alias:"cluster-dns-enable" help:"enable dns peering" env:"${env_bw_agent_bootstrap_dns_enabled}"`
	AWSEnabled    bool           `name:"bootstrap-aws-enable" alias:"cluster-aws-enable" help:"enable aws autoscaling group peering" env:"${env_bw_agent_bootstrap_aws_autoscaling_enabled}"`
	GCloudEnabled bool           `name:"bootstrap-gcloud-enable" alias:"cluster-gcloud-enable" help:"enable gcloud target pools peering" env:"${env_bw_agent_bootstrap_gcloud_taget_pool_enabled}"`
}

func (t *Peering) Join(ctx context.Context, config agent.Config, c clustering.Joiner, snap peering.File) (err error) {
	var (
		p2ppeers    clustering.Source
		clipeers    clustering.Source = peering.NewStaticTCP(t.Bootstrap...)
		awspeers    clustering.Source = peering.NewStaticTCP()
		gcloudpeers clustering.Source = peering.NewStaticTCP()
		dnspeers    clustering.Source = peering.NewStaticTCP()
	)

	if p2ppeers, err = p2ppeering(config); err != nil {
		log.Println("WARNING: P2P discovery disabled", err)
		p2ppeers = peering.NewStaticTCP()
	}

	if t.DNSEnabled {
		log.Println("dns peering enabled")
		dnspeers = peering.NewDNS(config.P2PBind.Port, append(config.DNSBootstrap, config.ServerName)...)
	}

	if t.AWSEnabled {
		log.Println("aws autoscale groups peering enabled")
		awspeers = peering.AWSAutoscaling{
			Port:               config.P2PBind.Port,
			SupplimentalGroups: config.AWSBootstrap.AutoscalingGroups,
		}
	}

	if t.GCloudEnabled {
		log.Println("gcloud target pool peering enabled")
		gcloudpeers = peering.GCloudTargetPool{
			Port:    config.P2PBind.Port,
			Maximum: config.MinimumNodes,
		}
	}

	return commandutils.ClusterJoin(ctx, config, c, clipeers, p2ppeers, awspeers, gcloudpeers, snap, dnspeers)
}

func (t *Peering) Snapshot(c clustering.Rendezvous, fssnapshot peering.File, options ...clustering.SnapshotOption) {
	go clustering.Snapshot(
		c,
		fssnapshot,
		options...,
	)
}

func (t *Peering) Raft(ctx context.Context, conf agent.Config, node *memberlist.Node, eq *grpc.ClientConn, options ...raftutil.ProtocolOption) (p raftutil.Protocol, err error) {
	var (
		dir = filepath.Join(conf.Root, "raft.d")
	)

	if err = os.MkdirAll(dir, 0700); err != nil {
		return p, err
	}

	defaultOptions := []raftutil.ProtocolOption{
		raftutil.ProtocolOptionQuorumMinimum(conf.MinimumNodes),
		raftutil.ProtocolOptionEnableSingleNode(conf.MinimumNodes <= 1),
		raftutil.ProtocolOptionPassiveReset(func() (s raftutil.Storage, ss raft.SnapshotStore, err error) {
			if err = errorsx.Compact(os.RemoveAll(dir), os.MkdirAll(dir, 0700)); err != nil {
				return s, ss, errors.WithStack(err)
			}

			if s, err = commandutils.RaftStoreFilepath(filepath.Join(dir, "state.bin")); err != nil {
				return s, ss, errors.WithStack(err)
			}

			if ss, err = raft.NewFileSnapshotStore(dir, 5, os.Stderr); err != nil {
				return s, ss, errors.WithStack(err)
			}

			return s, ss, nil
		}),
	}

	return raftutil.NewProtocol(
		ctx,
		node,
		eq,
		append(defaultOptions, options...)...,
	)
}

// BootstrapPeers converts a list of Peers into a list of addresses to bootstrap from.
func BootstrapPeers(peers ...*agent.Peer) peering.Static {
	speers := make([]string, 0, len(peers))
	for _, p := range peers {
		speers = append(speers, agent.SWIMAddress(p))
	}

	return peering.NewStatic(speers...)
}

func p2ppeering(c agent.Config) (s clustering.Source, err error) {
	var (
		tlsconfig *tls.Config
		ss        notary.Signer
		d         dialers.Defaults
		address   = net.JoinHostPort(c.ServerName, envx.String(strconv.Itoa(c.P2PBind.Port), bw.EnvAgentClusterP2PDiscoveryPort))
	)

	if ss, err = notary.NewAgentSigner(c.Root); err != nil {
		return nil, err
	}

	if tlsconfig, err = certificatecache.TLSGenServer(c, tlsx.OptionNoClientCert); err != nil {
		return nil, err
	}

	d, err = dialers.DefaultDialer(address, tlsx.NewDialer(tlsconfig), grpc.WithPerRPCCredentials(ss))
	if err != nil {
		return nil, err
	}

	return peering.P2P{
		Address: agent.URIDiscovery(address),
		Dialer:  d,
	}, nil
}
