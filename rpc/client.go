package rpc

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log"

	"github.com/smallnest/rpcx/client"
	kcp "github.com/xtaci/kcp-go"
	"golang.org/x/crypto/pbkdf2"
)

var (
	option client.Option = client.DefaultOption
	consul client.ServiceDiscovery
)

func StartCli(cfg *Cluster_config) {
	fmt.Println("111111111111111111111111111111 ")
	pass := pbkdf2.Key([]byte(cryptKey), []byte(cryptSalt), 4096, 32, sha1.New)
	bc, _ := kcp.NewAESBlockCrypt(pass)
	option.Block = bc

	consul = client.NewConsulDiscovery(cfg.ConsulPath, cfg.ConsulAddr, nil)
	for _, v := range consul.GetServices() {
		fmt.Println(v)
	}

}

type Args struct {
	A int
	B int
}

type Reply struct {
	C int
}

func Go() {
	fmt.Println("00000000000000000000000000000000")
	xclient := client.NewXClient("Arith", client.Failtry, client.RandomSelect, consul, option)
	reply := &Reply{}
	err := xclient.Call(context.Background(), "Mul", &Args{A: 10, B: 25}, reply)
	if err != nil {
		log.Fatalf("failed to call: %v", err)
	}
	fmt.Println("111111111 ", reply.C)
}
