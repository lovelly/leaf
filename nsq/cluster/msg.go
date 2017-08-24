package cluster

import (
	"encoding/gob"
	"errors"
	"fmt"
	"sync"

	"github.com/lovelly/leaf/chanrpc"
	"github.com/lovelly/leaf/log"
	//"github.com/lovelly/leaf/network/json"

	lgob "github.com/lovelly/leaf/network/gob"
)

const (
	NsqMsgTypeRsp          = iota //回应消息
	NsqMsgTypeBroadcast           //广播消息
	NsqMsgTypeNotForResult        // 不用回请求的消息
	NsqMsgTypeForResult           //要回请求的消息
)

var (
	routeMap        = map[interface{}]*chanrpc.Client{}
	Processor       = lgob.NewProcessor()
	RequestInfoLock sync.Mutex
	requestID       int64
	requestMap      = make(map[int64]*RequestInfo)
	encoder         = lgob.NewEncoder()
	decoder         = lgob.NewDecoder()
)

type RequestInfo struct {
	serverName string
	cb         interface{}
	chanRet    chan *chanrpc.RetInfo
}

func init() {
	gob.Register(&S2S_NsqMsg{})
	gob.Register(&chanrpc.RetInfo{})

	Processor.Register(&S2S_NsqMsg{})
	Processor.Register(&chanrpc.RetInfo{})
}

func SetRouter(msgID interface{}, server *chanrpc.Server) {
	_, ok := routeMap[msgID]
	if ok {
		panic(fmt.Sprintf("function id %v: already set route", msgID))
	}

	routeMap[msgID] = server.Open(0)
}

type S2S_NsqMsg struct {
	RequestID     int64
	MsgID         interface{}
	MsgType       uint8
	SrcServerName string
	DstServerName string
	Args          []interface{}
	Err           string
	PushCnt       int
}

func handleRequestMsg(recvMsg *S2S_NsqMsg) {
	sendMsg := &S2S_NsqMsg{MsgType: NsqMsgTypeRsp, MsgID: "Return", DstServerName: recvMsg.SrcServerName, RequestID: recvMsg.RequestID}
	if isClose() && recvMsg.MsgType == NsqMsgTypeForResult {
		sendMsg.Err = fmt.Sprintf("%v server is closing", SelfName)
		Publish(sendMsg)
		return
	}

	msgID := recvMsg.MsgID
	client, ok := routeMap[msgID]
	if !ok {
		err := fmt.Sprintf("%v msg is not set route", msgID)
		log.Error(err)

		if recvMsg.MsgType == NsqMsgTypeForResult {
			sendMsg.Err = err
			Publish(sendMsg)
		}
		return
	}

	args := recvMsg.Args
	if recvMsg.MsgType == NsqMsgTypeForResult {
		sendMsgFunc := func(ret *chanrpc.RetInfo) {
			if ret.Ret != nil {
				sendMsg.Args = []interface{}{ret.Ret}
			}
			if ret.Err != nil {
				sendMsg.Err = ret.Err.Error()
			}

			Publish(sendMsg)
		}

		args = append(args, sendMsgFunc)
		client.RpcCall(msgID, args...)
	} else {
		args = append(args, nil)
		client.RpcCall(msgID, args...)
	}
}

func handleResponseMsg(msg *S2S_NsqMsg) {
	request := popRequest(msg.RequestID)
	if request == nil {
		log.Error("%v: request id %v is not exist", msg.SrcServerName, msg.RequestID)
		return
	}

	ret := &chanrpc.RetInfo{Cb: request.cb}
	if len(msg.Args) > 0 {
		ret.Ret = msg.Args[0]
	}

	if msg.Err != "" {
		ret.Err = errors.New(msg.Err)
	}
	request.chanRet <- ret
}

func registerRequest(request *RequestInfo) int64 {
	RequestInfoLock.Lock()
	defer RequestInfoLock.Unlock()
	reqID := requestID
	requestMap[reqID] = request
	requestID += 1
	return reqID
}

func ForEachRequest(f func(id int64, request *RequestInfo)) {
	RequestInfoLock.Lock()
	defer RequestInfoLock.Unlock()
	for id, v := range requestMap {
		f(id, v)
	}
}

func popRequest(requestID int64) *RequestInfo {
	RequestInfoLock.Lock()
	defer RequestInfoLock.Unlock()

	request, ok := requestMap[requestID]
	if ok {
		delete(requestMap, requestID)
		return request
	} else {
		return nil
	}
}
