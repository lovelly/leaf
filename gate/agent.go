package gate

import (
	"net"

	"github.com/lovelly/leaf/chanrpc"
	"github.com/lovelly/leaf/module"
)

type Agent interface {
	WriteMsg(msg interface{}, id ...string)
	LocalAddr() net.Addr
	RemoteAddr() net.Addr
	RemoteIP() string
	Close(int)
	Destroy()
	UserData() interface{}
	SetUserData(data interface{})
	SetCloseFunc(f func(args ...interface{}))
	Skeleton() *module.Skeleton
	ChanRPC() *chanrpc.Server
}
