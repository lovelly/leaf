package cluster

import (
	"encoding/gob"
	"fmt"
	"sync"

	"github.com/lovelly/leaf/chanrpc"
	"github.com/lovelly/leaf/log"
	//"github.com/lovelly/leaf/network/json"

	"github.com/lovelly/leaf/gameError"
	lgob "github.com/lovelly/leaf/network/gob"
)

const (
	NsqMsgTypeRsp          = iota //回应消息
	NsqMsgTypeBroadcast           //广播消息
	NsqMsgTypeNotForResult        // 不用回请求的消息
	NsqMsgTypeForResult           //要回请求的消息
)

var (
	routeMap        = map[string]*chanrpc.Server{}
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

func SetRouter(msgID string, server *chanrpc.Server) {
	_, ok := routeMap[msgID]
	if ok {
		panic(fmt.Sprintf("function id %v: already set route", msgID))
	}

	routeMap[msgID] = server
}

type S2S_NsqMsg struct {
	RequestID     int64
	MsgID         string
	MsgType       uint8
	SrcServerName string
	DstServerName string
	Args          []interface{}
	Err           string
	PushCnt       int
}

func handleRequestMsg(recvMsg *S2S_NsqMsg) {
	sendMsg := &S2S_NsqMsg{MsgType: NsqMsgTypeRsp, MsgID: "Return :" + recvMsg.MsgID, DstServerName: recvMsg.SrcServerName, RequestID: recvMsg.RequestID}
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
		sendMsgFunc := func(ret interface{}, err error) {
			if ret != nil {
				sendMsg.Args = []interface{}{ret}
			}
			if err != nil {
				sendMsg.Err = err.Error()
			}

			Publish(sendMsg)
		}

		args = append(args, sendMsgFunc)
		client.AsynCall(nil, msgID, args...)
	} else {
		client.Go(msgID, args...)
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
		ret.Err = gameError.RenderServerError(msg.Err)
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
