package rpc

import (
	"crypto/sha1"
	"log"
	"reflect"
	"time"

	metrics "github.com/rcrowley/go-metrics"
	"github.com/smallnest/rpcx/server"
	"github.com/smallnest/rpcx/serverplugin"
	kcp "github.com/xtaci/kcp-go"
	"golang.org/x/crypto/pbkdf2"
)

var (
	rpcSvr *server.Server
)

func StartSvr(cfg *Cluster_config) {
	pass := pbkdf2.Key([]byte(cryptKey), []byte(cryptSalt), 4096, 32, sha1.New)
	bc, err := kcp.NewAESBlockCrypt(pass)
	if err != nil {
		panic(err)
	}
	rpcSvr = server.NewServer(server.WithBlockCrypt(bc))
	addRegistryPlugin(rpcSvr, cfg)
	go rpcSvr.Serve("kcp", cfg.ListenAddr)
}

func RegistServer(className string, class interface{}, metadata string) {
	vType := reflect.TypeOf(class)
	if vType == nil || vType.Kind() != reflect.Ptr {
		panic("RegistServer class must ptr")
	}

	if rpcSvr == nil {
		StartSvr(&Cluster_config{
			ConsulAddr: []string{"127.0.0.1:8500"},
			ListenAddr: "192.168.1.141:5521",
			ConsulPath: "/rpc",
		})
		//panic("RegistServer rpcSvr is nil ptr")
	}
	rpcSvr.RegisterName(className, class, metadata)
}

func addRegistryPlugin(s *server.Server, cfg *Cluster_config) {
	r := &serverplugin.ConsulRegisterPlugin{
		ServiceAddress: "tcp@" + cfg.ListenAddr,
		ConsulServers:  cfg.ConsulAddr,
		BasePath:       cfg.ConsulPath,
		Metrics:        metrics.NewRegistry(),
		UpdateInterval: time.Minute,
	}
	err := r.Start()
	if err != nil {
		log.Fatal(err)
	}
	s.Plugins.Add(r)
}
