package chanrpc

import (
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"runtime"

	"github.com/lovelly/leaf/gameError"
	"github.com/lovelly/leaf/log"
)

const (
	InternalServerError = 0xFFFFFFFF
)

// one server per goroutine (goroutine not safe)
// one client per goroutine (goroutine not safe)
type Server struct {
	functions      map[string]*FuncInfo
	ChanCall       chan *CallInfo
	AsynRet        chan *RetInfo //接收发出去的异步调用的回值
	AsynCallLen    int32
	CloseFlg       int32
	AsyncCallCount int32
}

type FuncInfo struct {
	id interface{}
	f  interface{}
}

type CallInfo struct {
	fInfo   *FuncInfo
	args    []interface{}
	chanRet chan *RetInfo
	cb      interface{}
}

func (c *CallInfo) GetFid() interface{} {
	return c.fInfo.id
}

func (c *CallInfo) GetArgs() []interface{} {
	return c.args
}
func (c *CallInfo) SetArgs(a []interface{}) {
	c.args = a
}

type RetInfo struct {
	Ret interface{}
	Err *gameError.ErrCode
	Cb  interface{}
}

func NewServer(l int) *Server {
	s := new(Server)
	s.functions = make(map[string]*FuncInfo)
	s.ChanCall = make(chan *CallInfo, l)
	s.AsynRet = make(chan *RetInfo, l*2)
	s.AsynCallLen = int32(l * 2)
	return s
}

func (s *Server) HasFunc(id string) (*FuncInfo, bool) {
	f, ok := s.functions[id]
	return f, ok
}

func (s *Server) Register(id string, f interface{}) {
	switch f.(type) {
	case func([]interface{}):
	case func([]interface{}) interface{}:
	default:
		panic(fmt.Sprintf("function id %v: definition of function is invalid", id))
	}

	if _, ok := s.functions[id]; ok {
		panic(fmt.Sprintf("function id %v: already registered", id))
	}

	s.functions[id] = &FuncInfo{id: id, f: f}
}

func (s *Server) ret(ci *CallInfo, ri interface{}) (err error) {
	retInfo := &RetInfo{}
	switch ri.(type) {
	case nil:
	case *gameError.ErrCode:
		retInfo.Err = ri.(*gameError.ErrCode)
	case error:
		retInfo.Err = &gameError.ErrCode{}
		retInfo.Err.ErrorCode = InternalServerError
		retInfo.Err.DescribeString = ri.(error).Error()
	case runtime.Error:
		retInfo.Err = &gameError.ErrCode{}
		retInfo.Err.ErrorCode = InternalServerError
		retInfo.Err.DescribeString = ri.(runtime.Error).Error()
	default:
		retInfo.Ret = ri
	}

	if ci.cb != nil {
		atomic.AddInt32(&s.AsyncCallCount, -1)
	}

	retInfo.Cb = ci.cb
	if ci.chanRet == nil {
		if ci.cb != nil {
			s.ExecRemoveCb(retInfo)
		}
		return
	}

	ci.chanRet <- retInfo
	return
}

func (s *Server) exec(ci *CallInfo) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Recover(r)
			s.ret(ci, r)
		}
	}()

	// execute
	var ret interface{}
	f := ci.fInfo.f
	switch f.(type) {
	case func([]interface{}):
		f.(func([]interface{}))(ci.args)
	case func([]interface{}) interface{}:
		ret = f.(func([]interface{}) interface{})(ci.args)
	default:
		panic(ci.fInfo.id)
	}

	return s.ret(ci, ret)
}

func (s *Server) Exec(ci *CallInfo) {
	err := s.exec(ci)
	if err != nil {
		log.Error("%v", err)
	}
}

// goroutine safe
func (s *Server) Go(id string, args ...interface{}) {
	if atomic.LoadInt32(&s.CloseFlg) == 1 {
		log.Error("at go services is close")
		return
	}
	f := s.functions[id]
	if f == nil {
		log.Error("function id %v: function not registered", id)
		return
	}

	defer func() {
		if r := recover(); r != nil {
			log.Recover(r)
		}
	}()

	err := s.call(&CallInfo{
		fInfo: f,
		args:  args,
	}, false)
	if err != nil {
		log.Error("function id %v: error :%s", id, err.Error())
	}
}

// goroutine safe
func (s *Server) Call0(id string, args ...interface{}) error {
	if atomic.LoadInt32(&s.CloseFlg) == 1 {
		log.Error("at Call0 chan is close %v", id)
		return errors.New("] send on closed channel")
	}
	f := s.functions[id]
	if f == nil {
		return fmt.Errorf("function id %v: function not registered", id)
	}

	ChanSyncRet := make(chan *RetInfo, 1)
	err := s.call(&CallInfo{
		fInfo:   f,
		args:    args,
		chanRet: ChanSyncRet,
	}, true)
	if err != nil {
		return err
	}

	ri := <-ChanSyncRet
	return ri.Err.ToSysError()
}

// goroutine safe
func (s *Server) Call(id string, args ...interface{}) (interface{}, error) {
	if atomic.LoadInt32(&s.CloseFlg) == 1 {
		log.Error("at Call1 chan is close %v", id)
		return nil, errors.New("] send on closed channel")
	}

	f := s.functions[id]
	if f == nil {
		return nil, fmt.Errorf("function id %v: function not registered", id)
	}

	ChanSyncRet := make(chan *RetInfo, 1)
	err := s.call(&CallInfo{
		fInfo:   f,
		args:    args,
		chanRet: ChanSyncRet,
	}, true)
	if err != nil {
		return nil, err
	}

	ri := <-ChanSyncRet
	return ri.Ret, ri.Err.ToSysError()
}

// src 发起调用的携程
func (s *Server) AsynCall(srcCh chan *RetInfo, id string, args ...interface{}) {
	if len(args) < 1 {
		panic("callback function not found")
	}

	cb := args[len(args)-1]
	args = args[:len(args)-1]

	switch cb.(type) {
	case func(error):
	case func(interface{}, error):
	case func(interface{}, *gameError.ErrCode):
	default:
		panic("definition of callback function is invalid")
	}

	f := s.functions[id]
	callInfo := &CallInfo{
		fInfo:   f,
		args:    args,
		chanRet: srcCh,
		cb:      cb,
	}

	if f == nil {
		s.ret(callInfo, fmt.Errorf("function %d not found", id))
		return
	}

	if s.AsyncCallCount > s.AsynCallLen {
		s.ret(callInfo, fmt.Errorf("call function %d chan full", id))
		return
	}

	err := s.call(callInfo, true)
	if err != nil {
		s.ret(callInfo, err)
		return
	}

	atomic.AddInt32(&s.AsyncCallCount, 1)
}

func (s *Server) TimeOutCall(id string, t time.Duration, args ...interface{}) (interface{}, error) {
	if atomic.LoadInt32(&s.CloseFlg) == 1 {
		log.Error("at Call1 chan is close %v", id)
		return nil, errors.New("] send on closed channel")
	}

	f := s.functions[id]
	if f == nil {
		return nil, fmt.Errorf("function id %v: function not registered", id)
	}

	ChanSyncRet := make(chan *RetInfo, 1)
	err := s.call(&CallInfo{
		fInfo:   f,
		args:    args,
		chanRet: ChanSyncRet,
	}, true)
	if err != nil {
		return nil, err
	}

	select {
	case ri := <-ChanSyncRet:
		return ri.Ret, ri.Err.ToSysError()
	case <-time.After(t):
		return nil, errors.New(fmt.Sprintf("time out at TimeOut Call1 function: %v", id))
	}
}

func (s *Server) call(ci *CallInfo, block bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Recover(r)
			err = fmt.Errorf("%v", r)
		}
	}()

	if block {
		s.ChanCall <- ci
	} else {
		select {
		case s.ChanCall <- ci:
		default:
			err = errors.New("chanrpc channel full")
		}
	}
	return
}

func (s *Server) Close() {
	if atomic.LoadInt32(&s.CloseFlg) == 1 {
		log.Error("double cloe chan server")
		return
	}
	atomic.StoreInt32(&s.CloseFlg, 1)
	close(s.ChanCall)
	for ci := range s.ChanCall {
		s.ret(ci, &RetInfo{Err: &gameError.ErrCode{ErrorCode: InternalServerError, DescribeString: "Server Is Close"}})
	}
	close(s.AsynRet)
	for ri := range s.AsynRet {
		s.ExecRemoveCb(ri)
	}
}

func (s *Server) ExecRemoveCb(ri *RetInfo) {
	defer func() {
		if r := recover(); r != nil {
			log.Recover(r)
		}
	}()

	// execute
	switch ri.Cb.(type) {
	case func(error):
		if ri.Err == nil {
			ri.Cb.(func(error))(nil)
		} else {
			ri.Cb.(func(error))(fmt.Errorf("errorCode:%d, error:%s", ri.Err.ErrorCode, ri.Err.DescribeString))
		}
	case func(interface{}, error):
		if ri.Err == nil {
			ri.Cb.(func(interface{},error))(ri.Ret,nil)
		} else {
			ri.Cb.(func(interface{}, error))(ri.Ret, fmt.Errorf("errorCode:%d, error:%s", ri.Err.ErrorCode, ri.Err.DescribeString))
		}
	case func(interface{}, *gameError.ErrCode):
		ri.Cb.(func(interface{}, *gameError.ErrCode))(ri.Ret, ri.Err)
	default:
		panic("bug")
	}
	return
}

func (s *Server) Idle() bool {
	return atomic.LoadInt32(&s.AsyncCallCount) == 1
}
