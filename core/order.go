package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/adoggie/jaguar/utils"
	"github.com/amir-the-h/okex"
	"github.com/amir-the-h/okex/events/public"
	"github.com/amir-the-h/okex/models/market"
	"github.com/amir-the-h/okex/models/publicdata"
	"github.com/amir-the-h/okex/models/trade"
	requests_trade "github.com/amir-the-h/okex/requests/rest/trade"
	trade2 "github.com/amir-the-h/okex/responses/trade"
	"strings"
	"sync"
	"time"
)

type StopReason string

const (
	TaskFinish  = StopReason("finish")
	TaskError   = StopReason("error")
	TaskTimeout = StopReason("timeout")
	TaskStop    = StopReason("stop")
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
	State        okex.OrderState
	Remain       float64 // 剩余请求数量
	Price        float64
	Size         float64
	OrderBook    public.OrderBook // 报单时的盘口
	BuySell      BuySell
	PlaceOrder   *requests_trade.PlaceOrder // 报价信息
	fillSz       float64                    // 累计成交数量
	filled       bool                       //委托是否全部成交
	orderReturns []*OrderEvent              // 成交回报
	orderTraded  map[string]*trade.Order    //key: TradeID

	latestOrder *trade.Order  // 最近的回报记录
	children    []*OrderSlice // slice spot

	//id           string
	//peerPlaceOrder []*requests_trade.PlaceOrder // 另一腿的成交记录
	uly           *Underlying
	ignored       bool
	ignoredReason string
	tasklog       *OrderTaskLog
	mtx           sync.Mutex
	isLast        bool
	peer          *OrderSlice
	ready         bool
	sequence      int64
}

func (slice *OrderSlice) Id() string {
	//id := fmt.Sprintf("%s_%d", strconv.FormatInt(slice.Start.UnixMilli(), 10), slice.sequence)
	id := fmt.Sprintf("%d%d", slice.Start.UnixMilli(), slice.sequence)
	return id
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

func (tasklog OrderTaskLog) Marshall() (result map[string]any) {
	result["Event"] = tasklog.Event
	result["TaskId"] = tasklog.TaskId
	result["Start"] = tasklog.Start

	return
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
	Amount  float64
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

	orderSlices   []*OrderSlice
	orderSliceHis []*OrderSlice // 历史委托记录

	//chExit      chan *events.TaskEvent
	chOrder      chan *OrderEvent
	chOrderBook  chan *OrderBookEvent
	chTask       chan *OrderTaskEvent
	chOrderSlice chan *OrderSlice

	fxCancel            context.CancelFunc
	stopReason          StopReason
	stopMessage         string
	running             bool
	latestOrderBookTime time.Time //map[string]*time.Time
	skewOrderBookTime   float64

	mtxOrderbook        sync.Mutex
	mtxOrderbookReEnter sync.Mutex
	orderbooks          map[string]*OrderBookEvent

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

		if task.Manager.GetConfig().OrderPriceOffsetLimit {
			// 防止take价格，保持在ask 与bid之间 ， bid的上方
			if price <= book.Books[0].Bids[0].DepthPrice {
				price = book.Books[0].Bids[0].DepthPrice + float64(uly.Instrument.TickSz)
			}
		}
		task.Manager.GetLogger().Println("price:", price)
	} else { //Buy
		// 比买价高 > bid[0]，不能达到卖价 < ask[0]
		spread := book.Books[0].Asks[0].DepthPrice - book.Books[0].Bids[0].DepthPrice
		price = book.Books[0].Bids[0].DepthPrice + spread*task.PendingPriceOffset
		// 精度对齐
		price = float64(int64(price/float64(uly.Instrument.TickSz))) * float64(uly.Instrument.TickSz)
		if task.Manager.GetConfig().OrderPriceOffsetLimit {
			// 防止take价格，保持在ask 与bid之间 ， bid的上方
			if price >= book.Books[0].Asks[0].DepthPrice {
				price = book.Books[0].Asks[0].DepthPrice - float64(uly.Instrument.TickSz)
			}
		}
	}
	return price
}

func (task *OrderTask) onOrderBook(ev *OrderBookEvent) {

	if !task.mtxOrderbookReEnter.TryLock() {
		return
	}
	defer task.mtxOrderbookReEnter.Unlock()

	orbTime := time.Time(ev.Inner.Books[0].TS)

	if orbTime.Sub(task.latestOrderBookTime).Seconds() > 5 {
		// 两次orb 间隔时间过长，认为网络断开之后的重连
		// 撤单
		task.Manager.GetLogger().Println("检测网络恢复，超时处理：撤单...")
		_ = task.Manager.GetBroker().RetreatOrderWithInstrument(nil,
			task.Request.Swap.Name, task.Request.Spot.Name)

		// 查询成交记录
		//total := 0.0
		for _, slice := range task.orderSlices {
			// 检查每个报单的运行状态,如果报单未完成fill（Target!=Present)，通过rest进行查询报单状态
			// rest 查询 发现fill了,则 补全 target=present
			if slice.ignored {
				continue
			}
			var orders trade2.OrderList
			var err error
			for n := 0; n < 10; n++ {
				task.Manager.GetLogger().Println("查询成交状态记录")
				orders, err = task.Manager.GetBroker().GetOrderDetail(slice.uly.Name, slice.Id())
				if err != nil {
					task.Manager.GetLogger().Println(err.Error())
					time.Sleep(time.Second * 2)
					continue
				}
				break
			}
			if err != nil {
				task.Manager.GetLogger().Println("查询失败，程序停止..")
				task.Manager.GetLogger().Fatalln(err.Error())
			}

			for _, order := range orders.Orders {
				slice.latestOrder = order
			}
		}
		// 检查 交易苏搜不存在的报单，删除 , (报单没有发出去)
		slices := utils.Filter(task.orderSlices, func(slice *OrderSlice) bool {
			return utils.If(slice.latestOrder == nil, false, true)
		})
		task.mtx.Lock()
		task.orderSlices = slices
		task.mtx.Unlock()
	}

	defer task.mtx.Unlock()
	task.mtx.Lock()

	if len(task.orderSlices) == 0 {
		if slice, err := task.firstOrder(); err != nil {
			task.Manager.GetLogger().Println(err.Error())
			return
		} else {
			task.orderSlices = append(task.orderSlices, slice)
			go func() {
				task.chOrderSlice <- slice
			}()
		}
		return
	}

	var slices []*OrderSlice
	for _, slice := range task.orderSlices {
		if slice.ignored {
			continue
		}
		if slice.latestOrder != nil {
			if slice.latestOrder.State == okex.OrderFilled || slice.latestOrder.State == okex.OrderCancel {
				slice.ignored = true
			}
		}
	}
	slices = utils.Filter(task.orderSlices, func(slice *OrderSlice) bool {
		return slice.ignored
	})

	if len(slices) == len(task.orderSlices) {
		// all slices ignored , means all order traded
		task.Stop(TaskFinish)
		task.Manager.GetLogger().Println("所有报单任务已完成，任务停止..", task)
		return
	}

	// 检查 orderSlice 对象的orderReturn 在指定时间是否有返回，没有则 撤单,清除slice

	// 无效的盘口偏移设定
	if task.Manager.GetConfig().TapePriceDeviation >= 1000 {
		return
	}
	for _, slice := range task.orderSlices {
		if slice.ready == false {
			continue
		}
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
			//fmt.Println("PlacePrice:", slice.PlaceOrder.Px,
			//	" Ask0:", slice.uly.GetOrderBookAsk(0).DepthPrice,
			//	" offset:", slice.uly.GetOrderBookAsk(0).DepthPrice-slice.PlaceOrder.Px,
			//	" Floating Limit:", slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation)
			//盘口往下移动

			// slice.PlaceOrder = nil 异常发生，原因是 创建slice时 ，先加入 task.orderslices ,此刻并未executeSlice ,所以很多状态都没有

			if slice.PlaceOrder.Px-slice.uly.GetOrderBookAsk(0).DepthPrice > slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation {
				//if slice.PlaceOrder.Px-slice.uly.book.Books[0].Asks[0].DepthPrice > slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation {
				needCancel = true
				task.Manager.GetLogger().Println("(Sell) TapePrice Float outside.",
					"PlacePrice:", slice.PlaceOrder.Px,
					" Ask0:", slice.uly.GetOrderBookAsk(0).DepthPrice,
					" offset:", slice.PlaceOrder.Px-slice.uly.GetOrderBookAsk(0).DepthPrice,
					" Floating Limit:", slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation)
			}
		} else { // buy
			if slice.uly.GetOrderBookBid(0).DepthPrice-slice.PlaceOrder.Px > slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation {
				//if slice.uly.book.Books[0].Bids[0].DepthPrice-slice.PlaceOrder.Px > slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation {
				needCancel = true
				task.Manager.GetLogger().Println("(Buy) TapePrice Float outside.",
					"PlacePrice:", slice.PlaceOrder.Px,
					" Bid0:", slice.uly.GetOrderBookBid(0).DepthPrice,
					" offset:", slice.uly.GetOrderBookBid(0).DepthPrice-slice.PlaceOrder.Px,
					" Floating Limit:", slice.PlaceOrder.Px*task.Manager.GetConfig().TapePriceDeviation)
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
			newslice, _ := task.nextOrder(slice)
			task.orderSlices = append(task.orderSlices, newslice)
			go func(slice *OrderSlice) {
				task.chOrderSlice <- slice
			}(newslice)
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
	task.latestOrderBookTime = time.Now()

EXIT:
	for {
		select {
		case <-wait.Done():
			break EXIT
		case <-timer:
			task.checkStatus() // 超时退出
			//task.checkSlice()
		case ev := <-task.chOrder:
			task.onOrderEvent(ev)
		case ev := <-task.chOrderBook:
			if task.orderbooks == nil {
				task.orderbooks = make(map[string]*OrderBookEvent)
			}
			task.mtxOrderbook.Lock()

			task.orderbooks[ev.Underlying.Name] = ev
			task.mtxOrderbook.Unlock()
			go func() {
				task.onOrderBook(ev)
				task.latestOrderBookTime = time.Time(ev.Inner.Books[0].TS)
			}()

		case ev := <-task.chTask:
			if ev.Reason == "stop" { // 停止
				task.onStop()
			}
		case slice := <-task.chOrderSlice:
			task.executeSlice(slice)
		}
	}

	task.onCleanup()
	task.End = time.Now()
	task.Manager.chTaskEvent <- &OrderTaskEvent{Reason: "task_stopped", Task: task}

}

// 检查任务超时则停止
func (task *OrderTask) checkStatus() {
	tll := task.Manager.GetConfig().TaskMaxLifeTimeLimit
	if tll > 0 {
		elapsed := int64(time.Now().Sub(task.Start).Seconds())
		if elapsed > tll {
			task.stopMessage = fmt.Sprintf("交易任务超时:%d ,%s", tll, task.Request.Id())
			task.Stop(TaskTimeout)
		}
	}
}

//func (task *OrderTask) OrderReturn(order *OrderEvent) {
//	go task.onOrderEvent(order)
//}

// 任务结束，两腿金额轧平
func (task *OrderTask) onCleanup() {
	task.Manager.broker.DoSquarePositionByPair(task.Request.Spot.Name, task.Request.Swap.Name)
}

func (task *OrderTask) getOrderBook(uly *Underlying) public.OrderBook {
	var book public.OrderBook
	return book
}

func (task *OrderTask) executeSlice(slice *OrderSlice) {
	defer task.mtx.Unlock()
	task.mtx.Lock()

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

	//task.Manager.GetLogger().Println("------------ sendOrder ---------------")
	//otl := slice.tasklog
	//if otl != nil {
	//	otl.ReqPlaceOrder = reqOrder
	//	otl.OrderMgrConfigVars = task.Manager.config
	//	if data, err := json.Marshal(otl); err == nil {
	//		task.Manager.MessageLog(otl.Event, string(data))
	//	}
	//}

	if err := task.Manager.broker.SendOrder(reqOrder); err == nil {
		//t.swapOrderSlices = append(t.swapOrderSlices, slice)
		//task.orderSlices = append(task.orderSlices, slice)
	} else {
		task.Manager.GetLogger().Println("sendOrder error:", err.Error())
		// 报单失败，直接停止
		task.stopMessage = fmt.Sprintf("send error: %s", err.Error())
		task.Stop(TaskError)
	}
	slice.ready = true
}

func (task *OrderTask) isOrderFinished(slice *OrderSlice) bool {
	if slice.uly.Type == UnderlyingSWAP {
		return int64(slice.Target) == int64(slice.Present) ||
			slice.State == okex.OrderFilled
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

/*
func (task *OrderTask) checkSlice() {
	// 应该 orderbook中处理
	// 取消定时检查

	// 未设置定时检查，忽略
	if task.Manager.GetConfig().CheckOrderReturnTimer <= 0 {
		return
	}
	// task.orderSlices 为空，直接退出 ,撤除所有报单，swap发送就出了问题
	// swap全部完成，spot未报单， 撤除所有报单，重新发送
	//task.mtx.Lock()
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
					if len(detail.Orders) == 0 {
						slice.ignored = true
						continue
					}
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
				if task.isOrderFinished(slice) {
					slice.ignored = true
					continue
				}
				slice.ignored = true
				// 继续报单
				fmt.Println(slice)
				go task.nextOrder(slice)
			}

		} else {
			slice.ignored = true
			slice.ignoredReason = ""
		}
	}
	//task.mtx.Unlock()
}
*/

func (task *OrderTask) onStart() {
	go func() {
		if len(task.orderSlices) == 0 {
			if slice, err := task.firstOrder(); err != nil {
				task.Manager.GetLogger().Println(err.Error())
				return
			} else {
				task.orderSlices = append(task.orderSlices, slice)
				go func() {
					task.chOrderSlice <- slice
				}()
			}
			return
		}
	}()
}

// 发送报单返回错误信息
func (task *OrderTask) OnOrderError(id string, reason string) {
	task.mtx.Lock()
	for _, slice := range task.orderSlices {
		if slice.Id() == id {
			task.stopMessage = reason
			task.Stop(TaskError)
			break
		}
	}
	task.mtx.Unlock()
}
func (task *OrderTask) firstOrder() (*OrderSlice, error) {

	task.Manager.GetLogger().Println("task firstOrder() ..")
	var size int64 = 0
	// 单位转换，计算合约数量
	if strings.Contains(task.Request.Swap.Name, "-USD-") { // 美金计算
		//amt := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
		//size = int64(task.Request.Amount / amt)
		//size = int64(task.Request.Amount / (amt * val))
		val := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
		//amt := task.spot.GetOrderBookAsk(0).DepthPrice
		size = int64(task.Request.Amount / val)
	} else {
		val := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
		amt := task.spot.GetOrderBookAsk(0).DepthPrice
		size = int64(task.Request.Amount / (amt * val))
	}
	//size := t.amount / t.Signal.Pair.Swap.Instrument
	if size <= 0 {
		return nil, errors.New("swap size is 0 , amount too small")
	}
	slice := &OrderSlice{Task: task, Start: time.Now(), sequence: task.Manager.nextSequence()}

	slice.Target = float64(size)
	slice.uly = task.swap
	slice.OrderBook = *task.swap.GetOrderBook()
	//slice.id = strconv.FormatInt(time.Now().UnixMilli(), 10)
	slice.BuySell = task.Request.Swap.BuySell
	slice.Price = task.getOrderPrice(slice.BuySell, slice.uly, slice.OrderBook)

	slice.tasklog = NewTaskLog("sendOrder(swap)", task)
	//task.chOrderSlice <- slice
	return slice, nil
}

// 继续报单
func (task *OrderTask) nextOrder(prev *OrderSlice) (*OrderSlice, error) {
	var size float64 = 0
	if task.isOrderFinished(prev) { // 再次确认,防止在orderreturn函数已经完成所有成交回报
		return nil, nil
	}
	left := prev.Target - prev.Present
	//left := prev.Target - prev.Remain
	if left <= 0 {
		return nil, nil
	}
	size = left
	//if prev.uly.Type == UnderlyingSWAP {
	//	size = left
	//}

	slice := &OrderSlice{Task: task, Start: time.Now(), sequence: task.Manager.nextSequence()}
	slice.Target = float64(size)
	slice.uly = prev.uly
	slice.OrderBook = *prev.uly.GetOrderBook()
	//slice.id = strconv.FormatInt(time.Now().UnixMilli(), 10)
	slice.BuySell = prev.BuySell
	slice.Price = task.getOrderPrice(slice.BuySell, slice.uly, slice.OrderBook)

	slice.tasklog = NewTaskLog("sendOrder", task)

	//task.chOrderSlice <- slice
	return slice, nil
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
	defer task.mtx.Unlock()
	task.mtx.Lock()

	//task.Manager.GetLogger().Println("-------------onOrderEvent--------------")
	//if data, e := json.Marshal(ev.Inner.Orders[0]); e == nil {
	//	log.Println(string(data))
	//}

	for _, order := range ev.Inner.Orders {

		for _, slice := range task.orderSlices {
			if order.ClOrdID == slice.Id() {
				slice.latestOrder = ev.Inner.Orders[0]
				if order.State != okex.OrderFilled && order.State != okex.OrderPartiallyFilled {
					break
				}
				slice.orderReturns = append(slice.orderReturns, ev)

				slice.Present = float64(order.AccFillSz)
				if slice.uly.Type == UnderlyingSPOT {
					// spot.AccFillSz 返回的是成交的币数量，而不是买卖的金额
					slice.Present = float64(order.AccFillSz) * slice.PlaceOrder.Px
				}
				if order.FillSz >= 0 {

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
						isLast := false
						// 记录合约完成的总数
						first := task.orderSlices[0] // swap_slice
						if first != slice {
							first.Present = first.Present + float64(order.FillSz)
						}
						if first.Present == first.Target {
							//合约全部完成
							task.Manager.GetLogger().Println("swap all traded.")
							isLast = true
						}

						//期货成交回报，立马对手现货报单
						// 计算现货数量
						var amt float64
						// 美元计价
						//amt = float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
						amt = float64(order.FillSz)

						if strings.Contains(task.Request.Swap.Name, "-USD-") {
							if task.Request.Spot.BuySell == Buy {
								//amt = amt / task.spot.GetOrderBookAsk(0).DepthPrice
								amt = amt * float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
								//amt = base / task.spot.GetOrderBookAsk(0).DepthPrice
							} else {
								//base := amt * float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
								//amt = base / task.spot.GetOrderBookBid(0).DepthPrice
								amt = amt * float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
							}
						} else { // USDT ,USDC 币计价
							if task.Request.Spot.BuySell == Buy {
								base := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
								amt = amt * base * task.spot.GetOrderBookAsk(0).DepthPrice
							} else { // Sell
								base := float64(task.swap.Instrument.CtMult) * float64(task.swap.Instrument.CtVal)
								amt = amt * base //* task.spot.GetOrderBookBid(0).DepthPrice
							}
						}
						// ticksize 对齐
						amt = float64(int64(amt/float64(task.spot.Instrument.MinSz))) * float64(task.spot.Instrument.MinSz)

						newslice, _ := task.spotOrder(amt, isLast)
						task.orderSlices = append(task.orderSlices, newslice)
						go func(slice *OrderSlice) {
							task.chOrderSlice <- slice
						}(newslice)
					}
					if slice.uly.Type == UnderlyingSPOT {
						// do nothing
						// spot.AccFillSz 返回的是成交的币数量，而不是买卖的金额
						slice.Present = float64(order.AccFillSz) * slice.PlaceOrder.Px
						task.Manager.GetLogger().Println("SPOT orderReturn.. , target:", slice.Target,
							" present:", slice.Present, " fillsz:", float64(order.AccFillSz),
							" place price:", slice.PlaceOrder.Px)
						fmt.Println(slice)

						// 最后一个slice报单完成
						task.Manager.GetLogger().Println("-- SPOT order State:", order.State, " islast:", slice.isLast)
						if order.State == okex.OrderFilled && slice.isLast {
							//if slice.isLast {
							task.Stop(TaskFinish)
							task.Manager.GetLogger().Println("spot finished, task finished.", task)
						}

					}
				}
			}
		}

	}

}

// 现货下单
func (task *OrderTask) spotOrder(size float64, isLast bool) (slice *OrderSlice, err error) {
	err = nil
	slice = &OrderSlice{Task: task, Start: time.Now(), sequence: task.Manager.nextSequence()}
	slice.isLast = isLast
	slice.Target = size
	slice.uly = task.spot
	slice.OrderBook = *task.spot.GetOrderBook()
	slice.BuySell = task.Request.Spot.BuySell
	slice.Price = task.getOrderPrice(slice.BuySell, slice.uly, slice.OrderBook)
	slice.tasklog = NewTaskLog("sendOrder(spot)", task)
	//task.chOrderSlice <- slice
	task.Manager.GetLogger().Printf("spotOrder:%+v", slice)
	return
}

func (task *OrderTask) Stop(reason StopReason) {
	task.stopReason = reason
	task.fxCancel()
}

func (task *OrderTask) Abort() {
	task.stopReason = TaskError
	task.fxCancel()
}
