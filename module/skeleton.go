package module

import (
	"time"

	"github.com/lovelly/leaf/chanrpc"
	"github.com/lovelly/leaf/go"
	"github.com/lovelly/leaf/log"
	"github.com/lovelly/leaf/timer"
	"github.com/robfig/cron"
)

type Skeleton struct {
	GoLen              int
	TimerDispatcherLen int
	AsynCallLen        int
	ChanRPCServer      *chanrpc.Server
	g                  *g.Go
	dispatcher         *timer.Dispatcher
	server             *chanrpc.Server
	cronChan           chan func()
}

func (s *Skeleton) Init() {
	if s.GoLen <= 0 {
		s.GoLen = 0
	}
	if s.TimerDispatcherLen <= 0 {
		s.TimerDispatcherLen = 0
	}
	if s.AsynCallLen <= 0 {
		s.AsynCallLen = 100
	}

	s.g = g.New(s.GoLen)
	s.dispatcher = timer.NewDispatcher(s.TimerDispatcherLen)
	s.server = s.ChanRPCServer
	s.cronChan = make(chan func(), 1)

	if s.server == nil {
		s.server = chanrpc.NewServer(s.AsynCallLen)
	}
}

func (s *Skeleton) Run(closeSig chan bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Recover(r)
			if !s.server.Idle() {
				go s.Run(closeSig)
			}
		}
	}()
	for {
		select {
		case <-closeSig:
			s.server.Close()
			for !s.g.Idle() {
				s.g.Close()
			}
			return
		case ri := <-s.server.AsynRet:
			s.server.ExecRemoveCb(ri)
		case ci := <-s.server.ChanCall:
			s.server.Exec(ci)
		case cb := <-s.g.ChanCb:
			s.g.Cb(cb)
		case t := <-s.dispatcher.ChanTimer:
			t.Cb()
		case f := <-s.cronChan:
			f()
		}
	}
}

func (s *Skeleton) GetChanAsynRet() chan *chanrpc.RetInfo {
	return s.server.AsynRet
}

func (s *Skeleton) AfterFunc(d time.Duration, cb func()) *timer.Timer {
	if s.TimerDispatcherLen == 0 {
		panic("invalid TimerDispatcherLen")
	}

	return s.dispatcher.AfterFunc(d, cb)
}

/*
	Field name   | Mandatory? | Allowed values  | Allowed special characters
	----------   | ---------- | --------------  | --------------------------
	Seconds      | Yes        | 0-59            | * / , -
	Minutes      | Yes        | 0-59            | * / , -
	Hours        | Yes        | 0-23            | * / , -
	Day of month | Yes        | 1-31            | * / , - ?
	Month        | Yes        | 1-12 or JAN-DEC | * / , -
	Day of week  | Yes        | 0-6 or SUN-SAT  | * / , - ?
*/

//每隔5秒执行一次：*/5 * * * * ?
//每隔1分钟执行一次：0 */1 * * * ?
//每天23点执行一次：0 0 23 * * ?
//每天凌晨1点执行一次：0 0 1 * * ?
//每月1号凌晨1点执行一次：0 0 1 1 * ?
//在26分、29分、33分执行一次：0 26,29,33 * * * ?
//每天的0点、13点、18点、21点都执行一次：0 0 0,13,18,21 * * ?

func (s *Skeleton) CronFunc(spec string, cb func()) *cron.Cron {
	cron := cron.New()
	err := cron.AddFunc(spec, func() {
		s.cronChan <- cb
	})
	if err != nil {
		log.Error("CronFunc error : %s", err.Error())
		return nil
	}
	cron.Start()
	return cron
}

func (s *Skeleton) Go(f func(), cb func()) {
	if s.GoLen == 0 {
		panic("invalid GoLen")
	}

	s.g.Go(f, cb)
}

func (s *Skeleton) NewLinearContext() *g.LinearContext {
	if s.GoLen == 0 {
		panic("invalid GoLen")
	}

	return s.g.NewLinearContext()
}
