package cluster

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lovelly/leaf/chanrpc"
	"github.com/lovelly/leaf/gameError"
	"github.com/lovelly/leaf/log"
)

var (
	clientsMutex sync.Mutex
	clients      = make(map[string]*NsqClient)
)

type NsqClient struct {
	Addr       string
	ServerName string
}

func GetGameServerName(id int) string {
	return fmt.Sprintf("GameSvr_%d", id)
}

func GetHallServerName(id int) string {
	return fmt.Sprintf("HallSvr_%d", id)
}

func AddClient(c *NsqClient) {
	log.Debug("at cluster AddClient %s, %s", c.ServerName, c.Addr)
	clientsMutex.Lock()
	defer clientsMutex.Unlock()

	clients[c.ServerName] = c
}

func HasClient(name string) bool {
	_, ok := clients[name]
	return ok
}

func RemoveClient(serverName string) {
	_, ok := clients[serverName]
	if ok {
		log.Debug("at cluster _removeClient %s", serverName)
		delete(clients, serverName)
		ForEachRequest(func(id int64, request *RequestInfo) {
			if request.serverName != serverName {
				return
			}
			ret := &chanrpc.RetInfo{Ret: nil, Cb: request.cb}
			ret.Err = gameError.RenderServerError(fmt.Sprintf("at call %s server is close", serverName))
			request.chanRet <- ret
			delete(requestMap, id)
		})
		log.Debug("at cluster _removeClient ok %s", serverName)
	}
}

func Broadcast(serverName string, id string, args ...interface{}) {
	msg := &S2S_NsqMsg{MsgType: NsqMsgTypeBroadcast, MsgID: id, SrcServerName: SelfName, DstServerName: serverName, Args: args}
	Publish(msg)
}

func Go(serverName string, id string, args ...interface{}) {
	msg := &S2S_NsqMsg{MsgType: NsqMsgTypeNotForResult, MsgID: id, SrcServerName: SelfName, DstServerName: serverName, Args: args}
	Publish(msg)
}

//timeOutCall 会丢弃执行结果
func TimeOutCall1(serverName string, t time.Duration, id string, args ...interface{}) (interface{}, error) {
	if !HasClient(serverName) {
		return nil, fmt.Errorf("TimeOutCall1 %s is off line", serverName)
	}

	chanSyncRet := make(chan *chanrpc.RetInfo, 1)

	request := &RequestInfo{chanRet: chanSyncRet, serverName: serverName}
	requestID := registerRequest(request)
	msg := &S2S_NsqMsg{RequestID: requestID, MsgType: NsqMsgTypeForResult, MsgID: id, SrcServerName: SelfName, DstServerName: serverName, Args: args}
	Publish(msg)
	select {
	case ri := <-chanSyncRet:
		return ri.Ret, ri.Err.ToSysError()
	case <-time.After(time.Second * t):
		popRequest(requestID)
		return nil, errors.New(fmt.Sprintf("time out at TimeOutCall1 msg: %v", args))
	}
}

func Call0(serverName string, id string, args ...interface{}) error {
	if !HasClient(serverName) {
		return fmt.Errorf("TimeOutCall1 %s is off line", serverName)
	}
	chanSyncRet := make(chan *chanrpc.RetInfo, 1)

	request := &RequestInfo{chanRet: chanSyncRet, serverName: serverName}
	requestID := registerRequest(request)
	msg := &S2S_NsqMsg{RequestID: requestID, MsgType: NsqMsgTypeForResult, MsgID: id, SrcServerName: SelfName, DstServerName: serverName, Args: args}
	Publish(msg)

	ri := <-chanSyncRet
	return ri.Err.ToSysError()
}

func Call1(serverName string, id string, args ...interface{}) (interface{}, error) {
	if !HasClient(serverName) {
		return nil, fmt.Errorf("TimeOutCall1 %s is off line", serverName)
	}

	chanSyncRet := make(chan *chanrpc.RetInfo, 1)

	request := &RequestInfo{chanRet: chanSyncRet, serverName: serverName}
	requestID := registerRequest(request)
	msg := &S2S_NsqMsg{RequestID: requestID, MsgType: NsqMsgTypeForResult, MsgID: id, SrcServerName: SelfName, DstServerName: serverName, Args: args}
	Publish(msg)

	ri := <-chanSyncRet
	return ri.Ret, ri.Err.ToSysError()
}

func AsynCall(serverName string, chanAsynRet chan *chanrpc.RetInfo, id string, args ...interface{}) {
	lastIndex := len(args) - 1
	cb := args[lastIndex]
	args = args[:lastIndex]

	if !HasClient(serverName) {
		ret := &chanrpc.RetInfo{Cb: cb, Err: gameError.RenderServerError(fmt.Sprintf("AsynCall %s is off line", serverName))}
		chanAsynRet <- ret
		return
	}

	var callType uint8
	switch cb.(type) {
	case func(error):
		callType = NsqMsgTypeForResult
	case func(interface{}, error):
		callType = NsqMsgTypeForResult
	case func([]interface{}, error):
		callType = NsqMsgTypeForResult
	default:
		panic(fmt.Sprintf("%v asyn call definition of callback function is invalid", args))
	}

	request := &RequestInfo{cb: cb, chanRet: chanAsynRet, serverName: serverName}
	requestID := registerRequest(request)
	msg := &S2S_NsqMsg{RequestID: requestID, MsgType: callType, MsgID: id, SrcServerName: SelfName, DstServerName: serverName, Args: args}
	Publish(msg)
}
