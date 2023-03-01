package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/amir-the-h/okex"
	"github.com/amir-the-h/okex/events/public"
	"github.com/amir-the-h/okex/models/market"
	"github.com/amir-the-h/okex/models/publicdata"
	"github.com/amir-the-h/okex/models/trade"
	requests_trade "github.com/amir-the-h/okex/requests/rest/trade"
	trade2 "github.com/amir-the-h/okex/responses/trade"
	"strconv"
	"strings"
	"sync"
	"time"
)

type StopReason int

const (
	TaskFinish = iota
	TaskError
	TaskStop
)

type OrderObject struct {
	Name string // 合约名称
	//Direction Direction // 买卖方向
	BuySell BuySell //
}

type OrderRequest struct {
	Spot   OrderObject // 现货
	Swap   OrderObject // 合约
	Amount float64     // 金额 , +开 ，-平
	Delta  interface{}
	Task   *OrderTask
}

func (req *OrderRequest) Id() string {
	return fmt.Sprintf("%s,%d#%s,%d", req.Spot.Name, req.Spot.BuySell, req.Swap.Name, req.Swap.BuySell)
}

func (req OrderRequest) NameIn(name string) bool {
	if req.Spot.Name == name || req.Swap.Name == name {
		return true
	}
	return false
}

// OrderSlice 交易任务
type OrderSlice struct {
	Task         *OrderTask
	Start        time.Time
	End          time.Time
	Target       float64 // 金额或合约数量
	Present      float64 // 金额或合约数量
	Remain       float64 // 剩余请求数量
	Price        float64
	Size         float64
	OrderBook    public.OrderBook // 报单时的盘口
	BuySell      BuySell
	PlaceOrder   *requests_trade.PlaceOrder // 报价信息
	fillSz       float64                    // 累计成交数量
	filled       bool                       //委托是否全部成交
	orderReturns []*OrderEvent              // 成交回报
	//id           string
	//peerPlaceOrder []*requests_trade.PlaceOrder // 另一腿的成交记录
	uly           *Underlying
	ignored       bool
	ignoredReason string
	tasklog       *OrderTaskLog
}

func (slice *OrderSlice) Id() string {
	return strconv.FormatInt(slice.Start.UnixMilli(), 10)
}

type OrderTaskLog struct {
	Event  string    `json:"event"`
	TaskId string    `json:"taskId"`
	Req    string    `json:"req"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`

	Orders             []*trade.Order      `json:"order"`
	OrbSpot            *market.OrderBookWs `json:"orbSpot"`
	OrbSwap            *market.OrderBookWs `json:"orbSwap"`
	ReqPlaceOrder      *requests_trade.PlaceOrder
	SpotInstrument     *publicdata.Instrument
	SwapInstrument     *publicdata.Instrument
	OrderMgrConfigVars *OrderManagerConfigVars
	CancelOrder        trade2.PlaceOrder
}

func NewTaskLog(event string, task *OrderTask) *OrderTaskLog {
	log := &OrderTaskLog{
		Event:          event,
		TaskId:         task.Id(),
		Req:            task.Request.Id(),
		Start:          time.Now(),
		OrbSpot:        task.spot.book.Books[0],
		OrbSwap:        task.swap.book.Books[0],
		SwapInstrument: task.swap.Instrument,
		SpotInstrument: task.spot.Instrument,
	}
	return log
}

type OrderTask struct {
	Request *OrderRequest
	Manager *OrderManager
	//spotInstrument *publicdata.Instrument
	//swapInstrument *publicdata.Instrument
	spot   *Underlying
	swap   *Underlying
	spread float64 //当时的价差，盘口移动需要追单，价差缩小需要撤单

	Create time.Time
	Start  time.Time
	End    time.Time

	mtx   sync.RWMutex
	delta interface{}

	PendingPriceOffset float64 // 挂单偏移 between ask and bid
	//ReOrderPriceFloat  float64 // 追单盘口偏移

	orderSlices []*OrderSlice

	//chExit      chan *events.TaskEvent
	chOrder      chan *OrderEvent
	chOrderBook  chan *OrderBookEvent
	chTask       chan *OrderTaskEvent
	chOrderSlice chan *OrderSlice

	fxCancel   context.CancelFunc
	stopReason StopReason

	//hisOrderEvents []*events.OrderEvent // 历史成交事件
}

func (task *OrderTask) Id() string {
	return fmt.Sprintf("%s_%s", task.spot.Name, task.swap.Name)
}

// 根据买买方向计算得到当前盘口的报价价格
func (task *OrderTask) getOrderPrice(bs BuySell, uly *Underlying, book public.OrderBook) float64 {
	var price float64
	//TAIL := len(book.Books[0].Asks) - 1
	if bs == Sell {
		// 比卖价低 < ask[0]，不能达到买价 > bid[0]

		spread := book.Books[0].Asks[0].DepthPrice - book.Books[0].Bids[0].DepthPrice
		price = book.Books[0].Asks[0].DepthPrice - spread*task.PendingPriceOffset
		//price = book.Books[0].Asks[TAIL].DepthPrice - spread*1
		// 精度对齐
		task.Manager.GetLogger().Println("--- ask[0]:", book.Books[0].Asks[0].DepthPrice,
			" bid[0]:", book.Books[0].Bids[0].DepthPrice, " spread:", spread, " offset:", task.PendingPriceOffset)

		price = float64(int64(price/float64(uly.Instrument.TickSz))) * float64(uly.Instrument.TickSz)
		// 防止take价格，保持在ask 与bid之间 ， bid的上方
		if price <= book.Books[0].Bids[0].DepthPrice {
			price = book.Books[0].Bids[0].DepthPrice + float64(uly.Instrument.TickSz)
		}
		task.Manager.GetLogger().Println("price:", price)
	} else { //Buy
		// 比买价高 > bid[0]，不能达到卖价 < ask[0]
		spread := book.Books[0].Asks[0].DepthPrice - book.Books[0].Bids[0].DepthPrice
		price = book.Books[0].Bids[0].DepthPrice + spread*task.PendingPriceOffset
		// 精度对齐
		price = float64(int64(price/float64(uly.Instrument.TickSz))) * float64(uly.Instrument.TickSz)
		// 防止take价格，保持在ask 与bid之间 ， bid的上方
		if price >= book.Books[0].Asks[0].DepthPrice {
			price = book.Books[0].Asks[0].DepthPrice - float64(uly.Instrument.TickSz)
		}
	}
	return price
}

func (task *OrderTask) onOrderBook(ev *OrderBookEvent) {
	// 进场 信号 达不到 开仓目标，直接退出
	// 计算 合约数量和现货数量
	// 创建报单order
	defer task.mtx.Unlock()
	task.mtx.Lock()

	// 无效的盘口偏移设定
	if task.Manager.GetConfig().TapePriceDeviation >= 1000 {
		return
	}
	for _, slice := range task.orderSlices {
		if slice.ignored {
			continue
		}
		if slice.uly.Type != UnderlyingSWAP {
			continue
		}

		if task.isOrderFinished(slice) {
			slice.ignored = true
			continue
		}

		var needCancel bool = false
		if slice.BuySell == Sell {
			if slice.uly.GetOrderBookAsk(0).DepthPrice-slice.PlaceOrder.Px > slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation {
				needCancel = true
			}
		} else { // buy
			if slice.uly.GetOrderBookBid(0).DepthPrice-slice.PlaceOrder.Px > slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation {
				needCancel = true
			}
		}
		if needCancel {
			// 撤单
			if res, err := task.Manager.GetBroker().RetreatOrder(slice.uly.Name, slice.Id()); err == nil {
				slice.tasklog.CancelOrder = res
			} else { // 撤单失败如何处理
				task.Manager.GetLogger().Println("retreadOrder error:", err.Error())
			}
			slice.ignored = true
			// 继续报单
			go task.nextOrder(slice)
		}
	}
}

func (task *OrderTask) Run(ctx context.Context) {
	var wait context.Context
	wait, task.fxCancel = context.WithCancel(ctx)
	if task.chOrder == nil {
		task.chOrder = make(chan *OrderEvent)
	}
	if task.chOrderBook == nil {
		task.chOrderBook = make(chan *OrderBookEvent)
	}
	task.onStart()
	timer := time.Tick(time.Second)
EXIT:
	for {
		select {
		case <-wait.Done():
			break EXIT
		case <-timer:
			task.checkSlice()
		case ev := <-task.chOrder:
			task.onOrderEvent(ev)
		case ev := <-task.chOrderBook:
			task.onOrderBook(ev)
		case ev := <-task.chTask:
			if ev.Reason == "stop" { // 停止
				task.onStop()
			}
		case slice := <-task.chOrderSlice:
			task.executeSlice(slice)
		}
	}

	task.onCleanup()
	task.Manager.chTaskEvent <- &OrderTaskEvent{Reason: "task_stopped", Task: task}

}

// 任务结束，两腿金额轧平
func (task *OrderTask) onCleanup() {
	task.Manager.broker.DoSquarePositionByPair(task.Request.Spot.Name, task.Request.Swap.Name)
}

func (task *OrderTask) getOrderBook(uly *Underlying) public.OrderBook {
	var book public.OrderBook
	return book
}

func (task *OrderTask) executeSlice(slice *OrderSlice) {
	task.Manager.GetLogger().Println("execute orderslice trade :", slice)
	slice.Start = time.Now()
	posSide := okex.PositionNetSide
	side := okex.OrderBuy
	if slice.BuySell == Sell {
		side = okex.OrderSell
	}
	tdMode := okex.TradeCrossMode
	ordType := okex.OrderLimit
	if slice.uly.Type == UnderlyingSPOT {
		ordType = okex.OrderMarket
	}
	reqOrder := &requests_trade.PlaceOrder{
		ID:      slice.Id(), //strconv.FormatInt(time.Now().UnixMilli(), 10),
		ClOrdID: slice.Id(),
		Tag:     "",
		InstID:  slice.uly.Name,
		Side:    side,
		TdMode:  tdMode,
		OrdType: ordType,
		Sz:      slice.Target,
		Px:      slice.Price,
		PosSide: posSide,
	}

	slice.PlaceOrder = reqOrder

	task.Manager.GetLogger().Println("------------ sendOrder ---------------")
	otl := slice.tasklog
	if otl != nil {
		otl.ReqPlaceOrder = reqOrder
		otl.OrderMgrConfigVars = task.Manager.config
		if data, err := json.Marshal(otl); err == nil {
			task.Manager.MessageLog(otl.Event, string(data))
		}
	}

	if err := task.Manager.broker.SendOrder(reqOrder); err == nil {
		//t.swapOrderSlices = append(t.swapOrderSlices, slice)
		task.orderSlices = append(task.orderSlices, slice)
	} else {
		task.Manager.GetLogger().Println("sendOrder error:", err.Error())
	}
}

func (task *OrderTask) isOrderFinished(slice *OrderSlice) bool {
	if slice.uly.Type == UnderlyingSWAP {
		return int64(slice.Target) == int64(slice.Present)
	}
	if slice.uly.Type == UnderlyingSPOT {
		// 剩余部分不够发单最小数量则认为全部完成
		left := slice.Target - slice.Present
		if left <= float64(slice.uly.Instrument.MinSz) {
			return true
		}
	}
	return false
}

func (task *OrderTask) checkSlice() {
	// 未设置定时检查，忽略
	if task.Manager.GetConfig().CheckOrderReturnTimer <= 0 {
		return
	}

	for _, slice := range task.orderSlices {
		if slice.ignored {
			continue
		}
		//if slice.Target != slice.Present {
		if !task.isOrderFinished(slice) {
			if time.Since(slice.Start).Seconds() > float64(task.Manager.GetConfig().CheckOrderReturnTimer) {
				// 超时未完成, rest 查询一次报单状态 ，再次检查 slice
				detail := task.Manager.broker.GetOrderDetail(slice.uly.Name, slice.Id())
				if detail != nil {
					slice.Present = float64(detail.Orders[0].AccFillSz)
				} else {
					// 查不到订单
					slice.ignored = true
					continue
				}
				if task.isOrderFinished(slice) {
					slice.ignored = true
					continue
				}
				task.Manager.GetLogger().Println("Wait OrderReturn Timeout, cancel order..")

				// 撤单
				if res, err := task.Manager.GetBroker().RetreatOrder(slice.uly.Name, slice.Id()); err == nil {
					slice.tasklog.CancelOrder = res
				} else { // 撤单失败如何处理
					task.Manager.GetLogger().Println("retreadOrder error:", err.Error())
				}
				slice.ignored = true
				// 继续报单
				go task.nextOrder(slice)
			}

		} else {
			slice.ignored = true
			slice.ignoredReason = ""
		}
	}

}

func (task *OrderTask) onStart() {
	go func() {
		if err := task.firstOrder(); err != nil {
			task.Manager.GetLogger().Println(err.Error())
			task.Abort()
		}
	}()
}

func (task *OrderTask) firstOrder() error {

	task.Manager.GetLogger().Println("task.firstOrder() ..")
	var size int64 = 0
	// 单位转换，计算合约数量
	if strings.Contains(task.Request.Swap.Name, "-USD-") { // 美金计算
		amt := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
		size = int64(task.Request.Amount / amt)
	} else {
		val := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
		amt := task.spot.GetOrderBookAsk(0).DepthPrice
		size = int64(task.Request.Amount / (amt * val))
	}
	//size := t.amount / t.Signal.Pair.Swap.Instrument
	if size <= 0 {
		return errors.New("swap size is 0 , amount too small")
	}
	slice := &OrderSlice{Task: task, Start: time.Now()}
	slice.Target = float64(size)
	slice.uly = task.swap
	slice.OrderBook = *task.swap.GetOrderBook()
	//slice.id = strconv.FormatInt(time.Now().UnixMilli(), 10)
	slice.BuySell = task.Request.Swap.BuySell
	slice.Price = task.getOrderPrice(slice.BuySell, slice.uly, slice.OrderBook)

	slice.tasklog = NewTaskLog("sendOrder(swap)", task)
	task.chOrderSlice <- slice
	return nil
}

// 继续报单
func (task *OrderTask) nextOrder(prev *OrderSlice) {
	var size float64 = 0
	left := prev.Target - prev.Present
	//left := prev.Target - prev.Remain
	if left <= 0 {
		return
	}
	size = left
	//if prev.uly.Type == UnderlyingSWAP {
	//	size = left
	//}

	slice := &OrderSlice{Task: task, Start: time.Now()}
	slice.Target = float64(size)
	slice.uly = prev.uly
	slice.OrderBook = *prev.uly.GetOrderBook()
	//slice.id = strconv.FormatInt(time.Now().UnixMilli(), 10)
	slice.BuySell = prev.BuySell
	slice.Price = task.getOrderPrice(slice.BuySell, slice.uly, slice.OrderBook)

	slice.tasklog = NewTaskLog("sendOrder", task)

	task.chOrderSlice <- slice
}

func (task *OrderTask) onStop() {
	// 平仓
	// 撤单
	task.fxCancel()
}

// OnOrderEvent 订单成交
// https://www.okx.com/docs-v5/zh/#websocket-api-trade-amend-multiple-orders
func (task *OrderTask) onOrderEvent(ev *OrderEvent) {
	// 合约成交回报
	//task.Manager.GetLogger().Println("onOrderEvent")

	//logx := NewTaskLog("onOrderEvent", task)
	//logx.Order = ev.Inner.Orders[0]
	//
	//if data, err := json.Marshal(logx); err == nil {
	//	task.Manager.MessageLog(logx.Event, string(data))
	//}

	//task.Manager.GetLogger().Printf("%s , %s , %+v", task.Id(), ev.Underlying.Name, ev.Inner.Orders[0])

	for _, order := range ev.Inner.Orders {
		if order.State != okex.OrderFilled && order.State != okex.OrderPartiallyFilled {
			continue
		}
		for _, slice := range task.orderSlices {
			if order.ClOrdID == slice.Id() {
				slice.Present = float64(order.AccFillSz)
				if order.FillSz > 0 {

					// log recording
					otl := slice.tasklog
					if otl != nil {
						otl.Orders = append(otl.Orders, order)
						if data, err := json.Marshal(otl); err == nil {
							task.Manager.MessageLog(otl.Event, string(data))
						}
					}

					if slice.uly.Type == UnderlyingSWAP {
						task.Manager.GetLogger().Println("SWAP orderReturn.. , target:", slice.Target, " present:", slice.Present)
						//期货成交回报，立马对手现货报单
						// 计算现货数量
						var amt float64
						// 美元计价
						//amt = float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
						amt = float64(order.FillSz)

						if strings.Contains(task.Request.Swap.Name, "-USD-") {
							if task.Request.Spot.BuySell == Buy {
								amt = amt / task.spot.GetOrderBookAsk(0).DepthPrice
							} else {
								amt = amt / task.spot.GetOrderBookBid(0).DepthPrice
							}
						} else { // USTD ,USTC 币计价
							if task.Request.Spot.BuySell == Buy {
								base := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
								amt = amt * base * task.spot.GetOrderBookAsk(0).DepthPrice
							} else { // Sell

							}
						}
						// ticksize 对齐
						amt = float64(int64(amt/float64(task.spot.Instrument.MinSz))) * float64(task.spot.Instrument.MinSz)

						go task.spotOrder(amt)
					}
					if slice.uly.Type == UnderlyingSPOT {
						// do nothing
						task.Manager.GetLogger().Println("SPOT orderReturn.. , target:", slice.Target, " present:", slice.Present)
					}
				}
			}
		}

	}

}

//现货下单
func (task *OrderTask) spotOrder(size float64) {

	slice := &OrderSlice{Task: task, Start: time.Now()}
	slice.Target = size
	slice.uly = task.spot
	slice.OrderBook = *task.spot.GetOrderBook()
	slice.BuySell = task.Request.Spot.BuySell
	slice.Price = task.getOrderPrice(slice.BuySell, slice.uly, slice.OrderBook)
	slice.tasklog = NewTaskLog("sendOrder(spot)", task)
	task.chOrderSlice <- slice
	task.Manager.GetLogger().Printf("spotOrder:%+v", slice)
}

func (task *OrderTask) Stop() {
	task.stopReason = TaskStop
	task.fxCancel()
}

func (task *OrderTask) Abort() {
	task.stopReason = TaskError
	task.fxCancel()
}
