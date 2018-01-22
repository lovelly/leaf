package cluster

import (
	"errors"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"encoding/json"

	"github.com/lovelly/leaf/log"
	"github.com/nsqio/go-nsq"
)

var (
	producer    *nsq.Producer
	consumers   []*nsq.Consumer
	proclose    bool
	prolock     sync.Mutex
	publishChan = make(chan *S2S_NsqMsg, 10000)
	SelfName    string
)

type Cluster_config struct {
	LogLv              string
	Channel            string   //唯一标识
	Csmtopics          []string //需要订阅的主题
	CsmUserAgent       string   //消费者的UserAgent
	CsmNsqdAddrs       []string
	CsmNsqLookupdAddrs []string
	CsmMaxInFlight     int
	PdrNsqdAddr        string //生产者需要连接的nsqd地址
	PdrUserAgent       string //生产者的UserAgent
	PdrMaxInFlight     int
	SelfName           string
}

func Start(cfg *Cluster_config) {
	if cfg.PdrMaxInFlight == 0 {
		cfg.PdrMaxInFlight = 100
	}

	if cfg.CsmMaxInFlight == 0 {
		cfg.CsmMaxInFlight = 100
	}

	if cfg.PdrUserAgent == "" {
		cfg.PdrUserAgent = "mqjx_producer"
	}

	if cfg.CsmUserAgent == "" {
		cfg.CsmUserAgent = "mqjx_consumer"
	}

	if cfg.SelfName == "" {
		log.Fatal("at nsq start selfname is nil")
	}

	if len(cfg.CsmNsqdAddrs) < 1 && len(cfg.CsmNsqLookupdAddrs) < 1 {
		log.Fatal("CsmNsqdAddrs and CsmNsqLookupdAddrs is nil")
	}

	var err error
	SelfName = cfg.SelfName
	nsqcfg := nsq.NewConfig()
	nsqcfg.UserAgent = cfg.PdrUserAgent
	nsqcfg.MaxInFlight = cfg.PdrMaxInFlight
	nsqcfg.DialTimeout = 10 * time.Second
	log.Debug("at Start Nsq Connect to PdrNsqdAddr %s", cfg.PdrNsqdAddr)
	if producer, err = nsq.NewProducer(cfg.PdrNsqdAddr, nsqcfg); err != nil {
		log.Fatal("start nsq client error:%s", err.Error())
	}

	loglv := getLogLovel(cfg.LogLv)
	producer.SetLogger(log.GetBaseLogger(), loglv)
	err = producer.Ping()
	if err != nil {
		log.Fatal("ping nsq client error:%s", err.Error())
	}

	for _, tpc := range cfg.Csmtopics {
		nsqcfg := nsq.NewConfig()
		nsqcfg.UserAgent = cfg.CsmUserAgent
		nsqcfg.MaxInFlight = cfg.CsmMaxInFlight
		nsqcfg.DialTimeout = 10 * time.Second
		consumer, err := nsq.NewConsumer(tpc, cfg.Channel, nsqcfg)
		if err != nil {
			log.Fatal(" nsq NewConsumer error:%s", err.Error())
		}

		consumer.SetLogger(log.GetBaseLogger(), loglv)
		consumer.AddHandler(NewNsqHandler())
		consumers = append(consumers, consumer)
		if len(cfg.CsmNsqdAddrs) > 0 {
			if err = consumer.ConnectToNSQDs(cfg.CsmNsqdAddrs); err != nil {
				log.Fatal(" ERROR:%s", err.Error())
			}
		}

		if len(cfg.CsmNsqLookupdAddrs) > 0 {
			if err = consumer.ConnectToNSQLookupds(cfg.CsmNsqLookupdAddrs); err != nil {
				log.Fatal(" ERROR:%s", err.Error())
			}
		}
	}

	go publishLoop()
}

func Publish(msg *S2S_NsqMsg) error {
	if msg.DstServerName == "" {
		log.Error("at Publish topc is nil === ")
		return errors.New("at Publish topc is ni")
	}
	prolock.Lock()
	if proclose {
		prolock.Unlock()
		log.Error("server is close at Publish")
		return errors.New("server is close at Publish")
	}
	prolock.Unlock()
	publishChan <- msg
	return nil
}

func Stop() {
	prolock.Lock()
	if proclose {
		return
	}
	proclose = true
	prolock.Unlock()
	for _, v := range consumers {
		v.Stop()
	}
	producer.Stop()
	log.Debug("at Nsq close @@@@@@@@@@@@@@@@@@@@@@")
	close(publishChan)
}

func isClose() bool {
	prolock.Lock()
	defer prolock.Unlock()
	return proclose
}

func publishLoop() {
	log.Debug("start publishLoop ... ")
	for {
		select {
		case msg, open := <-publishChan:
			if !open {
				log.Debug("at publishLoop return  ")
				return
			}
			if msg == nil {
				log.Debug("at publishLoop msg is nil ... ")
				continue
			}
			err := safePulishg(msg)
			if err != nil {
				msg.Err = err.Error()
				msg.MsgType = NsqMsgTypeRsp
				msg.Args = msg.Args[0:0]
				handleResponseMsg(msg)
			}
		}
	}
}

type nsqHandler struct {
}

func NewNsqHandler() *nsqHandler {
	return &nsqHandler{}
}

// HandleMessage - Handles an NSQ message.
func (h *nsqHandler) HandleMessage(message *nsq.Message) error {
	if message.Timestamp+3600 < time.Now().Unix() {
		return nil
	}
	//data, err := Processor.Unmarshal(decoder, message.Body)
	data, err := Processor.Unmarshal(message.Body)
	if err != nil {
		log.Error("handler msg error ===================== :%s %s", err.Error(), data)
		return nil
	}
	msg, ok := data.(*S2S_NsqMsg)
	if !ok {
		log.Debug("Unmarshal error ")
		return nil
	}

	str, _ := json.Marshal(msg.Args)
	log.Debug("Cluster IN ====name:%v, args:%v, SrcServerName:%s, DstServerName:%s", msg.MsgID, string(str), msg.SrcServerName, msg.DstServerName)
	//if msg.CallType == callBroadcast && msg.SrcServerName == SelfName {
	//	return nil
	//}
	defer func() {
		if err := recover(); err != nil {
			log.Error("%v \n %s ,%v", err, string(debug.Stack()), *msg)
		}
	}()

	switch msg.MsgType {
	case NsqMsgTypeRsp:
		handleResponseMsg(msg)
	default:
		handleRequestMsg(msg)
	}
	return nil
}

func safePulishg(msg *S2S_NsqMsg) (err error) {
	defer func() {
		if err1 := recover(); err != nil {
			switch err1.(type) {
			case runtime.Error:
				err = err1.(error)
			}
			log.Error("Publish msg recover error : %s, topc :%v ", err.Error(), msg)
		}
	}()

	//data, err := Processor.Marshal(encoder, msg)
	data, err := Processor.Marshal(msg)
	if err != nil {
		log.Error("Marshal error at Publish :%s", err.Error())
		return err
	}

	if len(data) < 1 {
		log.Error("error at Publish data is ni")
		return errors.New("error at Publish data is ni")
	}
	str, _ := json.Marshal(msg.Args)
	log.Debug("Cluster OUT ====name:%v, args:%s,SrcServerName:%s, DstServerName:%s", msg.MsgID, string(str), msg.SrcServerName, msg.DstServerName)
	if msg.Err != "" {
		log.Debug("Cluster OUT error ====err:%s, ", msg.Err)
	}
	err = producer.Publish(msg.DstServerName, data[0])
	if err != nil {
		msg.PushCnt++
		if msg.PushCnt < 2 {
			return safePulishg(msg)
		} else {
			return err
		}
	}
	return nil
}

func getLogLovel(loglv string) nsq.LogLevel {
	switch loglv {
	case "Debug":
		return nsq.LogLevelDebug
	case "Release":
		return nsq.LogLevelInfo
	case "Warn":
		return nsq.LogLevelWarning
	case "Error":
		return nsq.LogLevelError
	}

	return nsq.LogLevelError
}
