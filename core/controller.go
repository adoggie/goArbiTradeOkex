package core

import (
	"context"
	"encoding/json"
	"github.com/gookit/goutil/mathutil"
	//"github.com/pebbe/zmq4"
	"io"
	"net/http"
	"reflect"
	"strings"
)

/*
1.启动 mx ，接收外部发送的命令
*/

type CommandMessage struct {
	Message string    `json:"message"`
	Data    *Argument `json:"data,omitempty"`
}

type Argument struct {
	arg        map[string]interface{}
	untypedArg []interface{}
}

func (a *Argument) Get(k string) (interface{}, bool) {
	v, ok := a.arg[k]
	return v, ok
}

func (a *Argument) UnmarshalJSON(buf []byte) error {
	a.arg = make(map[string]interface{})
	if json.Unmarshal(buf, &a.arg) != nil {
		return json.Unmarshal(buf, &a.untypedArg)
	}

	return nil
}

type ControllerConfigVars struct {
	MxCmd      string `json:"mxCmd"`
	HttpServer string `json:"httpserver"`
}

type Controller struct {
	*CommBase
	chEvent    chan interface{}
	strategies map[string]*Strategy // 策略集合
	risk       *RiskManager
	orderMgr   *OrderManager
	ctx        context.Context
	config     *ControllerConfigVars

	//commandSock *zmq4.Socket
	//commandSock protocol.Socket
}

func (c *Controller) Init(config *ControllerConfigVars) {
	c.config = config
	c.strategies = make(map[string]*Strategy)
	c.chEvent = make(chan any)
	//c.initMx()
	c.iniHttpServer()
}

func (c *Controller) httpCmdProcess(w http.ResponseWriter, r *http.Request) {
	//r.URL.Query().Get()

	//if reader, err := r.Body; err != nil {
	//	c.GetLogger().Println("http request body is null !")
	//	return
	//} else {
	if data, err := io.ReadAll(r.Body); err == nil {
		var msg CommandMessage
		var err error
		c.GetLogger().Println("Got CmdMsg:", string(data))
		err = json.Unmarshal([]byte(data), &msg)
		if err != nil {
			c.GetLogger().Println("json decode error:", err.Error())
			return
		}
		c.execCommand(&msg)
	} else {
		c.GetLogger().Println("http request body is null !")
		return
	}
	//}

}

func (c *Controller) iniHttpServer() {
	http.HandleFunc("/cmd", c.httpCmdProcess)
	go func() {
		if err := http.ListenAndServe(c.config.HttpServer, nil); err != nil {
			c.GetLogger().Fatalln(err.Error())
		}
	}()

	c.GetLogger().Println("http server start on :", c.config.HttpServer)
}

//func (c *Controller) initMx() {
//	c.commandSock, _ = zmq4.NewSocket(zmq4.SUB)
//	if err := c.commandSock.SetSubscribe(""); err != nil {
//		c.GetLogger().Fatalln(err.Error())
//	}
//	if err := c.commandSock.Bind(c.config.MxCmd); err != nil {
//		c.GetLogger().Fatalln("bind ipc :", c.config.MxCmd, "error:", err.Error())
//	}
//}

func (c *Controller) SetOrderManager(ordermgr *OrderManager) *Controller {
	c.orderMgr = ordermgr
	return c
}

func (c *Controller) SetRiskManager(rm *RiskManager) *Controller {
	c.risk = rm
	return c
}

func (c Controller) GetChEvent() chan interface{} {
	return c.chEvent
}

//
//func (c *Controller) CommandInteract() {
//	for {
//		if data, err := c.commandSock.Recv(0); err != nil {
//			//if data, err := c.commandSock.Recv(); err != nil {
//			c.GetLogger().Println("CommandInteract Error:", err.Error())
//			break
//		} else {
//			var msg CommandMessage
//			var err error
//			c.GetLogger().Println("Got CmdMsg:", data)
//			err = json.Unmarshal([]byte(data), &msg)
//			if err != nil {
//				c.GetLogger().Println("json decode error:", err.Error())
//				continue
//			}
//			c.execCommand(&msg)
//
//		}
//	}
//}

func (c *Controller) execCommand(message *CommandMessage) {
	if message.Message == "create_order" {
		spot, ok := message.Data.Get("spot")
		if !ok {
			return
		}
		swap, ok := message.Data.Get("swap")
		if !ok {
			return
		}
		amount, ok := message.Data.Get("amount")
		if !ok {
			return
		}
		// {spot:"BTC-USDT,buy",swap:"" , amount:100 }
		var err error
		var req OrderRequest
		req.Swap.BuySell = Buy
		req.Spot.BuySell = Buy

		switch spot.(type) {
		case string:
			fs := strings.Split(spot.(string), ",")
			if len(fs) == 2 {
				req.Spot.Name = fs[0]
				if strings.ToLower(fs[1]) == "sell" {
					req.Spot.BuySell = Sell
				}
			} else {
				c.GetLogger().Println("Spot missing  buysell")
				return
			}
		}

		switch swap.(type) {
		case string:
			fs := strings.Split(swap.(string), ",")
			if len(fs) == 2 {
				req.Swap.Name = fs[0]
				if strings.ToLower(fs[1]) == "sell" {
					req.Swap.BuySell = Sell
				}
			} else {
				c.GetLogger().Println("Swap missing  buysell")
				return
			}
		}

		if req.Amount, err = mathutil.FloatOrErr(amount); err != nil {
			return
		}
		c.orderMgr.CreateOrder(&req)
	}
}

func (c *Controller) Start(ctx context.Context) {
	//c.ctx = context.Background()
	c.ctx = ctx
	//go c.CommandInteract()
	//c.CommandInteract()

	if c.orderMgr != nil {
		go c.orderMgr.Run(c.ctx)
	}
	if c.risk != nil {
		go c.risk.Run(ctx)
	}

	for _, st := range c.strategies {
		go st.Run(ctx)
	}

	if c.broker != nil {
		go c.broker.Run(ctx)
	}

	c.eventLoop(ctx)
}

func (c *Controller) OnSignal(signal *Signal) {
	c.risk.OnSignal(signal)
}

func (c *Controller) eventLoop(ctx context.Context) {
	for {
		select {
		case e := <-c.chEvent:
			//if v, ok := e.(events.BrokerEvent); ok && v == events.BrokerEvent("NetWsDisConnected") {
			//
			//}
			v := reflect.TypeOf(e)
			switch v {
			//case reflect.TypeOf(events.BrokerEvent("")): // lost websocket
			}
		}
	}
}

func (c *Controller) AddStrategy(st *Strategy) *Controller {
	c.strategies[st.Name()] = st
	st.AddUser(c)
	//c.broker.AddSubscribers(st)
	return c
}

func NewController() *Controller {
	return nil
}
