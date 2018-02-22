package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"runtime/debug"

	"github.com/lovelly/leaf/chanrpc"
	"github.com/lovelly/leaf/gameError"
	"github.com/lovelly/leaf/log"
)

type Processor struct {
	msgInfo map[string]*MsgInfo
}

type MsgInfo struct {
	msgType    reflect.Type
	msgRouter  *chanrpc.Server
	msgHandler MsgHandler
}
type MsgHandler func([]interface{}) interface{}

func NewProcessor() *Processor {
	p := new(Processor)
	p.msgInfo = make(map[string]*MsgInfo)
	return p
}

// It's dangerous to call the method on routing or marshaling (unmarshaling)
func (p *Processor) Register(msg interface{}) string {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		log.Fatal("json message pointer required", string(debug.Stack()))
	}
	msgID := msgType.Elem().Name()
	if msgID == "" {
		log.Fatal("unnamed json message", string(debug.Stack()))
	}
	if _, ok := p.msgInfo[msgID]; ok {
		log.Fatal("message %v is already registered %s", msgID, string(debug.Stack()))
	}

	i := new(MsgInfo)
	i.msgType = msgType
	p.msgInfo[msgID] = i
	return msgID
}

// It's dangerous to call the method on routing or marshaling (unmarshaling)
func (p *Processor) SetRouter(msg interface{}, msgRouter *chanrpc.Server) {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		log.Fatal("json message pointer required", string(debug.Stack()))
	}
	msgID := msgType.Elem().Name()
	i, ok := p.msgInfo[msgID]
	if !ok {
		log.Fatal("message %v not registered, %s", msgID, string(debug.Stack()))
	}

	i.msgRouter = msgRouter
}

// It's dangerous to call the method on routing or marshaling (unmarshaling)
func (p *Processor) SetHandler(msg interface{}, msgHandler MsgHandler) {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		log.Fatal("json message pointer required", string(debug.Stack()))
	}
	msgID := msgType.Elem().Name()
	i, ok := p.msgInfo[msgID]
	if !ok {
		log.Fatal("message %v not registered", msgID)
	}

	i.msgHandler = msgHandler
}

// goroutine safe
func (p *Processor) Route(msg interface{}, userData interface{}) error {
	// json
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		return errors.New("json message pointer required")
	}
	msgID := msgType.Elem().Name()
	i, ok := p.msgInfo[msgID]
	if !ok {
		return fmt.Errorf("at RouteByType 11 message %v not registered", msgID)
	}
	if i.msgHandler != nil {
		i.msgHandler([]interface{}{msg, userData})
	} else if i.msgRouter != nil {
		i.msgRouter.Go(msgID, msg, userData)
	} else {
		log.Error("%v msg without any handler", msgID)
	}
	return nil
}

// goroutine safe
func (p *Processor) RouteByID(msgID string, msg interface{}, userData interface{}, cb func(interface{}, *gameError.ErrCode)) {
	i, ok := p.msgInfo[msgID]
	if !ok {
		cb(nil, &gameError.ErrCode{ErrorCode: 0xFFFFFFFF, DescribeString: "not found msg id" + msgID})
		return
	}
	if i.msgHandler != nil {
		p.callHandler(i.msgHandler, []interface{}{msg, userData}, cb)
	} else if i.msgRouter != nil {
		i.msgRouter.AsynCall(nil, msgID, msg, userData, cb)
	} else {
		log.Error("%v msg without any handler", msgID)
	}
	return
}

func (p *Processor) callHandler(h MsgHandler, args []interface{}, cb func(interface{}, *gameError.ErrCode)) {
	defer func() {
		if err := recover(); err != nil {
			switch err.(type) {
			case *gameError.ErrCode:
				cb(nil, err.(*gameError.ErrCode))
			case error:
				cb(nil, &gameError.ErrCode{ErrorCode: 0xFFFFFFFF, DescribeString: err.(error).Error()})
			case runtime.Error:
				log.Error(string(debug.Stack()))
				cb(nil, &gameError.ErrCode{ErrorCode: 0xFFFFFFFF, DescribeString: err.(runtime.Error).Error()})
			}
		}
	}()
	ret := h(args)
	switch ret.(type) {
	case *gameError.ErrCode:
		cb(nil, ret.(*gameError.ErrCode))
	default:
		cb(ret, nil)
	}
}

// goroutine safe
func (p *Processor) Unmarshal(data []byte) (interface{}, error) {
	var m map[string]json.RawMessage
	err := json.Unmarshal(data, &m)
	if err != nil {
		return nil, err
	}
	if len(m) != 1 {
		return nil, errors.New("invalid json data")
	}

	for msgID, data := range m {
		i, ok := p.msgInfo[msgID]
		if !ok {
			return nil, fmt.Errorf("at json Unmarshal message %v not registered", msgID)
		}
		msg := reflect.New(i.msgType.Elem()).Interface()
		return msg, json.Unmarshal(data, msg)
	}

	panic("bug")
}

// goroutine safe
func (p *Processor) Marshal(msg interface{}, id ...string) ([][]byte, error) {
	var msgID string
	if len(id) > 0 {
		msgID = id[0]
	} else {
		msgType := reflect.TypeOf(msg)
		if msgType == nil || (msgType.Kind() != reflect.Ptr && msgType.Kind() != reflect.Map) {
			return nil, fmt.Errorf("json message pointer required cur:%s", msgType.String())
		}
		msgID = msgType.Elem().Name()
		if _, ok := p.msgInfo[msgID]; !ok {
			return nil, fmt.Errorf("at json Marshal message %v not registered", msgID)
		}
	}

	var errCode *gameError.ErrCode
	switch msg.(type) {
	case nil:
	case *gameError.ErrCode:
		errCode = msg.(*gameError.ErrCode)
	case error:
		errCode = &gameError.ErrCode{}
		errCode.DescribeString = msg.(error).Error()
		errCode.ErrorCode = 0xFFFFFFFF
	case runtime.Error:
		log.Error(string(debug.Stack()))
		errCode = &gameError.ErrCode{}
		errCode.DescribeString = msg.(runtime.Error).Error()
		errCode.ErrorCode = 0xFFFFFFFF
	default:

	}
	// data
	m := map[string]interface{}{}
	m["Name"] = msgID
	if errCode != nil {
		m["Error"] = errCode
	} else if msg != nil {
		m["Data"] = msg
	}

	data, err := json.Marshal(m)
	return [][]byte{data}, err
}

func (p *Processor) GetMsgId(msg interface{}) (string, error) {
	msgType := reflect.TypeOf(msg)
	if msgType == nil || msgType.Kind() != reflect.Ptr {
		return "", fmt.Errorf("json message pointer required cur:%s", msgType.String())
	}
	msgID := msgType.Elem().Name()
	if _, ok := p.msgInfo[msgID]; !ok {
		return msgID, fmt.Errorf("at json GetMsgId message %v not registered", msgID)
	}
	return msgID, nil
}

func (p *Processor) GetAllMsgs() map[string]interface{} {
	m := make(map[string]interface{})
	for _, v := range p.msgInfo {
		msg := reflect.New(v.msgType.Elem()).Interface()
		m[v.msgType.String()] = msg
	}
	return m
}
