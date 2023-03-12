package core

import (
	"context"
	"encoding/json"
	"github.com/amir-the-h/okex"
	"github.com/amir-the-h/okex/api"
	events2 "github.com/amir-the-h/okex/events"
	"github.com/amir-the-h/okex/events/private"
	"github.com/amir-the-h/okex/events/public"
	requests "github.com/amir-the-h/okex/requests/rest/public"
	"github.com/amir-the-h/okex/requests/rest/trade"
	ws_private_requests "github.com/amir-the-h/okex/requests/ws/private"
	ws_public_requests "github.com/amir-the-h/okex/requests/ws/public"
	responses "github.com/amir-the-h/okex/responses/trade"
	"github.com/gorilla/websocket"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

type SpotAndSwap struct {
	Spot   string `json:"spot"`
	Swap   string `json:"swap"`
	Enable bool   `json:"enable,omitempty"`
}

type BrokerApiKey struct {
	Id         string `json:"id"`
	ApiKey     string `json:"apiKey,omitempty"`
	SecretKey  string `json:"secretKey,omitempty"`
	PassPhrase string `json:"passPhrase,omitempty"`
	Server     string `json:"server"`
}

type BrokerConfigVars struct {
	Id  string `json:"id,omitempty"`
	Key string `json:"key"`

	BookChannel      string         `json:"bookChannel,omitempty"`
	Underlyings      []*SpotAndSwap `json:"underlyings,omitempty"`
	ApiKey           *BrokerApiKey
	RetreatAllOrders bool `json:"retreat_all_orders"`
}

type BrokerSubscriber interface {
	OnOrderBook(book *OrderBookEvent)
	OnOrderReturn(order *OrderEvent)
	OnAccount(acc *AccountEvent)
	OnPosition(pos *PositionEvent)
	OnBalancePosition(balpos *BalanceAndPositionEvent)
	OnBrokerEvent(ev *BrokerEvent)
}

// 订阅通道
type OkexContext struct {
	errChan  chan *events2.Error
	subChan  chan *events2.Subscribe
	uSubChan chan *events2.Unsubscribe
	loginCh  chan *events2.Login
	orderCh  chan *private.Order // 报单事件
	//iCh := make(chan *public.Instruments)
	orbCh               chan *public.OrderBook // 订单簿
	sucCh               chan *events2.Success
	StructuredEventChan chan interface{}
	RawEventChan        chan *events2.Basic
	DoneChan            chan interface{}
}

type Broker struct {
	*CommBase
	users           []BrokerSubscriber
	config          *BrokerConfigVars
	chEvent         chan *BrokerEvent
	underlyings     map[string]*Underlying
	underlyingPairs []*UnderlyingPair
	client          *api.Client
	ctx             context.Context
	wsCtx           context.Context
	fxWsCancel      context.CancelFunc
	fxExit          context.CancelFunc
	chMsg           chan string
	mtx             sync.Mutex
	okex            OkexContext
	chWsLost        chan *websocket.CloseError
}

func (b *Broker) Id() string {
	return b.config.Id
}

func (b *Broker) GetClient() *api.Client {
	return b.client
}

func (b *Broker) AddSubscribers(subs ...BrokerSubscriber) {
	for _, sub := range subs {
		b.users = append(b.users, sub)
	}
}

func (b *Broker) GetUnderlying(name string) *Underlying {
	if uly, ok := b.underlyings[name]; ok {
		return uly
	}
	return nil
}

func (b *Broker) onBrokerEvent(event *BrokerEvent) {
	for _, user := range b.users {
		go func(user BrokerSubscriber) {
			user.OnBrokerEvent(event)
		}(user)
	}
}

func (b *Broker) onOrderBook(book *public.OrderBook) {
	//b.GetLogger().Println("onOrderBook..")
	//if data, e := json.Marshal(book); e == nil {
	//	log.Println(string(data))
	//}
	for _, user := range b.users {
		go func(user BrokerSubscriber) {
			var instId string
			if v, ok := book.Arg.Get("instId"); ok {
				instId = v.(string)
			} else {
				return
			}
			if underlying, ok := b.underlyings[instId]; !ok {
				return
			} else {

				event := &OrderBookEvent{
					Underlying: underlying,
					Inner:      book}
				underlying.SetOrderBook(book)
				user.OnOrderBook(event)
			}

		}(user)
	}
}

func (b *Broker) onOrderReturn(order *private.Order) {
	//b.GetLogger().Println("onOrderReturn..")
	//if data, e := json.Marshal(order); e == nil {
	//	log.Println(string(data))
	//}
	if len(order.Orders) == 0 {
		b.GetLogger().Println("invalid OrderReturn message.")
		return
	}
	for _, user := range b.users {
		go func(user BrokerSubscriber) {
			var instId string
			instId = order.Orders[0].InstID
			if underlying, ok := b.underlyings[instId]; !ok {
				return
			} else {
				event := &OrderEvent{
					Underlying: underlying,
					Inner:      order}
				user.OnOrderReturn(event)
			}
		}(user)
	}
}

func (b *Broker) onAccount(acc *private.Account) {
	for _, user := range b.users {
		go func(user BrokerSubscriber) {
			var instId string
			if v, ok := acc.Arg.Get("instId"); ok {
				instId = v.(string)
			} else {
				return
			}
			if underlying, ok := b.underlyings[instId]; ok {
				event := &AccountEvent{
					Underlying: underlying,
					Inner:      acc}
				user.OnAccount(event)
			}
		}(user)
	}
}

func (b *Broker) onPosition(pos *private.Position) {
	for _, user := range b.users {
		go func(user BrokerSubscriber) {
			var instId string
			if v, ok := pos.Arg.Get("instId"); ok {
				instId = v.(string)
			} else {
				return
			}
			if underlying, ok := b.underlyings[instId]; ok {
				event := &PositionEvent{
					Underlying: underlying,
					Inner:      pos}
				user.OnPosition(event)
			}
		}(user)
	}
}

func (b *Broker) OnBalancePosition(balpos *private.BalanceAndPosition) {
	for _, user := range b.users {
		go func(user BrokerSubscriber) {
			var instId string
			if v, ok := balpos.Arg.Get("instId"); ok {
				instId = v.(string)
			} else {
				return
			}
			if underlying, ok := b.underlyings[instId]; ok {
				event := &BalanceAndPositionEvent{
					Underlying: underlying,
					Inner:      balpos}
				user.OnBalancePosition(event)
			}
		}(user)
	}
}

func (b *Broker) initOkexContext() {
	b.okex.errChan = make(chan *events2.Error)
	//b.okex.subChan = make(chan *events2.Subscribe)
	//b.okex.uSubChan = make(chan *events2.Unsubscribe)
	//b.okex.loginCh = make(chan *events2.Login)
	b.okex.orderCh = make(chan *private.Order) // 报单事件
	//iCh := make(chan *public.Instruments)
	b.okex.orbCh = make(chan *public.OrderBook) // 订单簿
	//b.okex.sucCh = make(chan *events2.Success)

	//sockCh := make(chan *websocket.CloseError)
	b.okex.StructuredEventChan = make(chan interface{})
	b.okex.RawEventChan = make(chan *events2.Basic)
	b.okex.DoneChan = make(chan interface{})
}

func (b *Broker) Init(config *BrokerConfigVars) bool {
	b.config = config
	b.initOkexContext()
	b.chEvent = make(chan *BrokerEvent)
	b.chWsLost = make(chan *websocket.CloseError)

	b.underlyings = make(map[string]*Underlying)
	for _, ss := range b.config.Underlyings {
		if !ss.Enable {
			continue
		}
		if strings.TrimSpace(ss.Spot) == "" || strings.TrimSpace(ss.Swap) == "" {
			continue
		}
		ulySpot := &Underlying{Name: ss.Spot, Type: UnderlyingSPOT}
		ulySwap := &Underlying{Name: ss.Swap, Type: UnderlyingSWAP}
		ulySpot.Peer = ulySwap
		ulySwap.Peer = ulySpot
		b.underlyings[ulySpot.Name] = ulySpot
		b.underlyings[ulySwap.Name] = ulySwap
		pair := &UnderlyingPair{Spot: ulySpot, Swap: ulySwap}
		b.underlyingPairs = append(b.underlyingPairs, pair)
	}
	b.ctx, b.fxExit = context.WithCancel(context.Background())
	return true
}

func (b *Broker) subscribeOkex(client *api.Client) error {
	b.GetLogger().Println("subscribeOkex..")
	channel := strings.TrimSpace(b.config.BookChannel)
	if channel == "" {
		channel = "books5"
	}
	// 订阅所有交易对的 订单簿
	var err error
	for name, _ := range b.underlyings {
		err = client.Ws.Public.OrderBook(
			ws_public_requests.OrderBook{InstID: name, Channel: channel}, b.okex.orbCh)
		if err != nil {
			return err
		}
	}
	b.GetLogger().Println("subscribe orderBook Okay!")

	// 订阅成交回报事件

	if err = client.Ws.Private.Order(ws_private_requests.Order{InstType: okex.SpotInstrument}, b.okex.orderCh); err != nil {
		return err
	}
	b.GetLogger().Println("subscribe spot orderReturn Okay!")
	if err = client.Ws.Private.Order(ws_private_requests.Order{InstType: okex.SwapInstrument}, b.okex.orderCh); err != nil {
		return err
	}
	b.GetLogger().Println("subscribe swap orderReturn Okay!")
	return err
}

func (b *Broker) initOkexClient() *api.Client {
	b.GetLogger().Println("initOkexClient..")
	var err error
	var client *api.Client
	dest := okex.NormalServer
	if b.config.ApiKey.Server == "test" {
		dest = okex.DemoServer
	}
	b.wsCtx, b.fxWsCancel = context.WithCancel(b.ctx)
	//ctx := context.Background()
	//client, err = api.NewClient(ctx, b.config.ApiKey.ApiKey, b.config.ApiKey.SecretKey, b.config.ApiKey.PassPhrase, dest)
	client, err = api.NewClient(b.wsCtx, b.config.ApiKey.ApiKey, b.config.ApiKey.SecretKey, b.config.ApiKey.PassPhrase, dest)
	//client, err = api.NewClient(b.ctx, "", "",
	//	"", dest)
	if err != nil {
		b.GetLogger().Println(err.Error())
		return nil
	}
	client.Ws.ErrChan = b.okex.errChan
	//client.Ws.SetChannels(b.okex.errChan, b.okex.subChan, b.okex.uSubChan, b.okex.loginCh, b.okex.sucCh)
	//client.Ws.SocketCloseCh = b.chWsLost
	client.Ws.StructuredEventChan = b.okex.StructuredEventChan
	client.Ws.RawEventChan = b.okex.RawEventChan
	client.Ws.DoneChan = b.okex.DoneChan

	if err := b.InitInstruments(client); err != nil {
		b.GetLogger().Println(err.Error())
		return nil
	}
	b.GetLogger().Println("initOkexClient.. Okay!")
	return client
}

func (b *Broker) prepare() {

	b.reset()
	if b.config.RetreatAllOrders {
		client := b.initOkexClient()
		if client == nil {
			b.GetLogger().Fatalln("Broker.InitOkexClient Failed!")
		}
		_ = b.RetreatAllOrder(client)
	}
}

func (b *Broker) Run(ctx context.Context) {
	b.prepare()
	if b.chMsg == nil {
		b.chMsg = make(chan string)
	}

	timer := time.NewTicker(time.Second * 2)
	b.onNetWsDisconnected()
MAIN:
	for {
		select {
		case <-ctx.Done():
			break MAIN
		case <-b.chWsLost:
			b.fxWsCancel() //
			time.Sleep(time.Second * 3)
			b.client = nil
		case <-timer.C: // 定时检查 连接断开，检查仓位是否水平
			b.mtx.Lock()
			if b.client == nil || b.client.Ws.ConnStatus[true] == nil || b.client.Ws.ConnStatus[false] == nil {
				b.onNetWsDisconnected()
				var client *api.Client
				if client = b.initOkexClient(); client != nil {
					// 检查两腿对齐
					if ok := b.DoSquarePosition(client); !ok {
						b.mtx.Unlock()
						continue
					}
				} else {
					b.mtx.Unlock()
					continue
				}
				if err := b.subscribeOkex(client); err == nil {
					//b.mtx.Unlock()
					//time.Sleep(time.Second * 1)
					//continue
				} else {
					b.GetLogger().Println(err.Error())
					b.mtx.Unlock()
					continue
				}
				if b.client != nil {
					// 第二次连接需要等待一会儿
					time.Sleep(time.Second * 10)
				}
				b.client = client
			}
			b.mtx.Unlock()

		case ev := <-b.chEvent:
			if ev.Reason == "WsNetDisconnected" {
				b.mtx.Lock()
				b.client = nil
				b.mtx.Unlock()
				//b.onBrokerEvent(ev)
			}
		case orb := <-b.okex.orbCh: // 订单簿
			b.onOrderBook(orb)
		case order := <-b.okex.orderCh: // 成交信息
			b.onOrderReturn(order)
		case e := <-b.okex.StructuredEventChan:
			//log.Printf("[Event] STRUCTED:\t%+v", e)
			_ = e
		case e := <-b.okex.RawEventChan:
			_ = e

		case e := <-b.okex.DoneChan:
			_ = e
		case e := <-b.okex.errChan:
			b.GetLogger().Println("onError return ..")
			if data, e := json.Marshal(e); e == nil {
				log.Println(string(data))
			}
			if e.Op == "order" {
				//报单发送返回
				e := &BrokerEvent{Reason: "orderError", Data: e.ID}
				b.onBrokerEvent(e)
			}
		}
	}
	b.GetLogger().Println("Broker Exit..")
}

// DoSquarePosition 保证两腿对齐 , 轧平 期货现货仓位
func (b *Broker) DoSquarePosition(client *api.Client) bool {
	return true
}

func (b *Broker) DoSquarePositionByPair(spot, swap string) bool {
	return true
}

func (b *Broker) reset() {

}

func (b *Broker) onNetWsDisconnected() {
	//b.chEvent <- &BrokerEvent{Reason: "WsNetDisconnected"}
	for _, user := range b.users {
		user.OnBrokerEvent(&BrokerEvent{Reason: "WsNetDisconnected"})
	}

}

// SetLeverage 设置杠杆
func (b *Broker) SetLeverage(level float64) {

}

// GetCommission 报单手续费
func (b *Broker) GetCommission(instId string) (take, make float64) {
	take = 8 / 10000
	make = 5 / 10000
	return
}

//func (b *Broker) RetreatOrderWithInstId(instId string) (err error) {
//	req := []trade.CancelOrder{{InstID: instId}}
//	resp, err = b.client.Rest.Trade.CandleOrder(req)
//	return
//}

func (b *Broker) RetreatOrderWithInstrument(client *api.Client, instIds ...string) error {
	b.GetLogger().Println("RetreatOrderWithInstrument:", instIds)
	req := []trade.CancelOrder{}
	var res responses.OrderList
	var err error

	if client == nil {
		client = b.client
	}
	if client == nil {
		b.GetLogger().Println(" client is nil ")
		return nil
	}

	for _, instId := range instIds {
		if res, err = client.Rest.Trade.GetOrderList(trade.OrderList{InstID: instId}); err != nil {
			b.GetLogger().Println("GetOrderList Error:", err.Error())
		}
		for _, order := range res.Orders {
			req = append(req, trade.CancelOrder{InstID: order.InstID, OrdID: order.OrdID, ClOrdID: order.ClOrdID})
		}
		if len(req) != 0 {
			if _, err = client.Rest.Trade.CandleOrder(req); err != nil {
				b.GetLogger().Println("RetreatOrderWithInstrument Request Failed. ", err.Error())
				return err
			}
		}
	}
	return nil
}

func (b *Broker) RetreatOrder(instId string, clOrdId string) (resp responses.PlaceOrder, err error) {
	req := []trade.CancelOrder{{InstID: instId, ClOrdID: clOrdId}}
	resp, err = b.client.Rest.Trade.CandleOrder(req)
	return
}

// RetreatAllOrder 撤除所有委托
func (b *Broker) RetreatAllOrder(client *api.Client) error {
	b.GetLogger().Println("RetreatAllOrder..")
	req := []trade.CancelOrder{}
	var res responses.OrderList
	var err error
	if res, err = client.Rest.Trade.GetOrderList(trade.OrderList{}); err != nil {
		b.GetLogger().Fatalln("GetOrderList Error:", err.Error())
	}
	for _, order := range res.Orders {
		instId := order.InstID
		if _, ok := b.underlyings[instId]; ok {
			req = append(req, trade.CancelOrder{InstID: instId, OrdID: order.OrdID, ClOrdID: order.ClOrdID})
		}
	}
	if len(req) != 0 {
		b.GetLogger().Println(req)
		if _, err := client.Rest.Trade.CandleOrder(req); err != nil {
			b.GetLogger().Fatalln("RetreatAllOrder Request Failed. ", err.Error())
			return err
		}
	}
	return nil
}

func (b *Broker) SendOrder(order *trade.PlaceOrder) (err error) {
	//reqOrder *requests_trade.PlaceOrder)
	b.GetLogger().Printf("sendOrder: %+v\n", order)
	b.mtx.Lock()
	err = b.client.Ws.Trade.PlaceOrder(*order)
	b.mtx.Unlock()
	if err != nil {
		b.GetLogger().Println("Ws send failed, try to rest ..")
		_, err = b.client.Rest.Trade.PlaceOrder([]trade.PlaceOrder{*order})
	}
	//reqOrder := requests_trade.PlaceOrder{
	//	ID:      strconv.FormatInt(time.Now().UnixMilli(), 10),
	//	InstID:  instId,
	//	Side:    okex.OrderBuy,
	//	TdMode:  okex.TradeCashMode,
	//	OrdType: okex.OrderLimit,
	//	Sz:      1,
	//	Px:      float64(10.0),
	//	PosSide: okex.PositionNetSide,
	//}
	return err
}

// GetSpotOrderSize
// 根据金额计算现货报单的数量
// amount ： 金额
func (b *Broker) GetSpotOrderSize(forward ForwardType, spot *Underlying, amount float64) float64 {
	var size = 0.0
	//if forward == ForwardPremium {
	//	size = int(amount / spot.Book.Books[0].Asks[0].DepthPrice)
	//}

	size = amount / spot.GetOrderBookAsk(0).DepthPrice
	size = math.Max(size, float64(spot.Instrument.MinSz))
	return size
}

func (b *Broker) InitInstruments(client *api.Client) error {
	resp, err := client.Rest.PublicData.GetInstruments(requests.GetInstruments{InstType: okex.InstrumentType("SPOT")})
	if err == nil {
		for _, ins := range resp.Instruments {
			if uly, ok := b.underlyings[ins.InstID]; ok {
				uly.Instrument = ins
			}
		}
	} else {
		return err
		//panic("GetInstruments failed!")
	}
	resp, err = client.Rest.PublicData.GetInstruments(requests.GetInstruments{InstType: okex.InstrumentType("SWAP")})
	if err == nil {
		for _, ins := range resp.Instruments {
			if uly, ok := b.underlyings[ins.InstID]; ok {
				uly.Instrument = ins
			}
		}
	}
	b.GetLogger().Println("Ready Instruments:", b.underlyings)
	return err
}

func (b *Broker) GetOrderDetail(instId string, clOrdId string) (res responses.OrderList, err error) {
	req := trade.OrderDetails{InstID: instId, ClOrdID: clOrdId}

	if res, err = b.client.Rest.Trade.GetOrderDetail(req); err == nil {
		return
	}
	return

}
