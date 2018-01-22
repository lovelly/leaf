package rpc

type Cluster_config struct {
	ConsulAddr []string
	ListenAddr string
	ConsulPath string
}

const (
	cryptKey  = "rpcx-key"
	cryptSalt = "rpcx-salt"
)

func Start(cfg *Cluster_config) {
	StartSvr(cfg)
	StartCli(cfg)
}
