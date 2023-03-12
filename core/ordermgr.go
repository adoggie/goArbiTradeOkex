package core

import (
	"context"
	"fmt"
	thislog "github.com/adoggie/jaguar/logging"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type OrderManagerConfigVars struct {
	LogFile               string  `json:"logfile,omitempty"`
	Mx                    string  `json:"mx,omitempty"`
	Name                  string  `json:"name"`
	OrderPriceOffset      float64 `json:"order_price_offset"`    // 盘口报价偏移 , ask[0] - bid[0] 价格偏移比率
	TapePriceDeviation    float64 `json:"tape_price_deviation"`  // 盘口移动撤单重发 ask[0]或bid[0] 偏离上次报价的比率
	CheckOrderReturnTimer int64   `json:"checkOrderReturnTimer"` // 报单成交超时检查
	OrderPriceOffsetLimit bool    `json:"order_price_offset_limit"`
	TradeLogFile          string  `json:"trade_log_file"` // 成交记录文件
	TaskMaxLifeTimeLimit  int64   `json:"task_max_lifetime_limit"`
}

type OrderManager struct {
	*CommBase
	chTaskEvent  chan *OrderTaskEvent
	mtx          sync.RWMutex
	tasks        map[string]*OrderTask
	ctx          context.Context
	chMessageLog chan string
	logger       *log.Logger
	config       *OrderManagerConfigVars
	chOrder      chan *OrderEvent
	chOrderBook  chan *OrderBookEvent
	networkOk    bool

	seqId    int64
	mtxSeqId sync.Mutex
	fpTrade  *os.File
}

func (mgr *OrderManager) nextSequence() int64 {
	defer mgr.mtxSeqId.Unlock()
	mgr.mtxSeqId.Lock()
	mgr.seqId += 1
	return mgr.seqId
}

func (mgr *OrderManager) OnAccount(acc *AccountEvent) {

}

func (mgr *OrderManager) OnPosition(pos *PositionEvent) {
	//TODO implement me
	//panic("implement me")
}

func (mgr *OrderManager) OnBalancePosition(balpos *BalanceAndPositionEvent) {
	//TODO implement me
	//panic("implement me")
}

func (mgr *OrderManager) OnBrokerEvent(ev *BrokerEvent) {
	//TODO implement me
	//panic("implement me")
	if ev.Reason == "WsNetDisconnected" {
		mgr.networkOk = false
	}
	if ev.Reason == "orderError" {

		mgr.mtx.Lock()
		for _, task := range mgr.tasks {
			go func(task *OrderTask) {
				if id, ok := ev.Data.(string); ok {
					task.OnOrderError(id, ev.Reason)
				}
			}(task)
		}
		mgr.mtx.Unlock()
	}
}

func (mgr *OrderManager) Init(config *OrderManagerConfigVars) bool {
	var err error
	mgr.config = config
	//mgr.config.CheckOrderReturnTimer = int64(math.Max(5., float64(mgr.config.CheckOrderReturnTimer)))

	mgr.tasks = make(map[string]*OrderTask)
	mgr.chTaskEvent = make(chan *OrderTaskEvent)
	mgr.chOrder = make(chan *OrderEvent)

	if mgr.fpTrade, err = os.OpenFile(mgr.config.TradeLogFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err != nil {
		mgr.GetLogger().Fatalln("Open TradeLogFile Error:", err.Error())
	}

	mgr.chOrderBook = make(chan *OrderBookEvent)
	return true
}

func (mgr *OrderManager) MessageLog(message string, v ...any) {
	SPLIT := " //// "
	mgr.GetLogger().Println(message, SPLIT, v)
}

func (mgr *OrderManager) newLogger() *log.Logger {
	if mgr.logger == nil {
		fn := "orderMgr.log"
		format := "2006-01-02"
		t := time.Now().UTC().Format(format)
		fn = fmt.Sprintf("%s_%s.log", fn, t)

		writers := []io.Writer{os.Stdout}
		if mgr.config.LogFile != "" {
			fn = mgr.config.LogFile
			fp, _ := os.OpenFile(fn, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
			writers = append(writers, fp)
		}
		if mgr.config.Mx != "" {
			zh := thislog.NewZmqHandler(mgr.config.Mx, mgr.config.Name)
			writers = append(writers, zh)
		}
		mgr.logger = log.New(io.MultiWriter(writers...), mgr.config.Name, log.LstdFlags|log.Lshortfile)
	}
	return mgr.logger
}

func (mgr *OrderManager) GetConfig() *OrderManagerConfigVars {
	return mgr.config
}

func (mgr *OrderManager) CreateOrder(req *OrderRequest) *OrderTask {
	//tf.chTaskEvent <- &events.TaskEvent{Reason: "start",Task: task}
	if mgr.networkOk == false {
		return nil
	}

	spot := mgr.broker.GetUnderlying(req.Spot.Name)
	swap := mgr.broker.GetUnderlying(req.Swap.Name)
	if spot == nil || swap == nil {
		mgr.GetLogger().Println("underlying not found ! please check:", req.Spot.Name, " or ", req.Swap.Name)
		return nil
	}
	if spot.GetOrderBook() == nil || swap.GetOrderBook() == nil {
		mgr.GetLogger().Println("either swap or spot orderbook  is nil!")
		return nil

	}
	task := &OrderTask{
		Request:             req,
		Manager:             mgr,
		spot:                spot,
		swap:                swap,
		Create:              time.Now(),
		Start:               time.Now(),
		chOrder:             make(chan *OrderEvent),
		chOrderBook:         make(chan *OrderBookEvent),
		chTask:              make(chan *OrderTaskEvent),
		chOrderSlice:        make(chan *OrderSlice),
		PendingPriceOffset:  mgr.config.OrderPriceOffset,
		latestOrderBookTime: time.Now(),
	}

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()
	if _, ok := mgr.tasks[task.Id()]; ok {
		log.Println("task:", task.Id(), " is running, skipped..")
		return nil
	}
	mgr.tasks[task.Id()] = task
	go func() {
		mgr.GetLogger().Printf("OrderTask start. %+v \n", req)
		task.Run(mgr.ctx)
	}()

	return task
}

func (mgr *OrderManager) Run(ctx context.Context) {
	mgr.ctx = ctx
	for {
		select {
		case <-ctx.Done():
			break
		case ev := <-mgr.chTaskEvent:
			mgr.onTaskEvent(ev)
			/*v := reflect.TypeOf(ev)
			switch v {
			case reflect.TypeOf(events.OrderEvent{}):
			case reflect.TypeOf(events.TaskEvent{}):
				tf.onTaskEvent(ev.(*events.TaskEvent))
			*/
		case ev := <-mgr.chOrder:
			mgr.onOrderReturn(ev)
		case ev := <-mgr.chOrderBook:
			mgr.onOrderBook(ev)
		}

	}
}

func (mgr *OrderManager) StopTask(task *OrderTask) {
	if task == nil {
		return
	}
	mgr.StopTaskByName(task.Request.Spot.Name, task.Request.Swap.Name)
}

func (mgr *OrderManager) StopTaskByName(spot, swap string) {
	if task := mgr.GetTaskByName(spot, swap); task != nil {
		mgr.mtx.Lock()
		if _, ok := mgr.tasks[task.Id()]; ok {
			delete(mgr.tasks, task.Id())
		}
		mgr.mtx.Unlock()
		task.Stop(TaskStop)
	}
}

func (mgr *OrderManager) GetTaskByName(spot, swap string) *OrderTask {
	defer mgr.mtx.Unlock()
	mgr.mtx.Lock()
	for _, task := range mgr.tasks {
		if task.Request.Spot.Name == spot && task.Request.Swap.Name == swap {
			return task
		}
	}
	return nil
}

func (mgr *OrderManager) onTaskEvent(event *OrderTaskEvent) {
	task := event.Task
	if event.Reason == "task_stopped" {
		mgr.GetLogger().Println("task stopped.", task, task.Request)
		mgr.mtx.Lock()
		if _, ok := mgr.tasks[task.Id()]; ok {
			delete(mgr.tasks, task.Id())
		}
		mgr.mtx.Unlock()

		var message any
		message = mgr.FormatTaskInfo(task)
		mgr.GetController().TaskReport(message)
		// 发送报单统计信息
		PrintTask(task, mgr.fpTrade)
	}
}

/*
下单模块输出
task_output ={
    'taskid':'xxxxxx',
    'msg':'xx', # success,stoped,failed,
    'DuijiaRatio': DuijiaRatio,#直接对价价差比例
    'RealRatio': RealRatio,#实际成交价差比例
    'slave_avg_price': slave_avg_price,
    'master_avg_price':master_avg_price,
    'master_dollar_finished':master_dollar_finished,
    'slave_dollar_finished':slave_dollar_finished,
    'swapordernums':len(gvalue.Taoli_Task['master_orderlist'].keys()), # master总订单个数
    'spotordernums':len(gvalue.Taoli_Task['slave_orderlist'].keys()), # slave 总订单个数
    'side':side, # sell buy
}

*/

func (mgr *OrderManager) FormatTaskInfo(task *OrderTask) any {
	msg := map[string]any{
		"message": CmTaskEnd,
		"data": map[string]any{
			"taskid":                 task.Id(),
			"msg":                    task.stopMessage,
			"reason":                 string(task.stopReason),
			"DuijiaRatio":            0,
			"RealRatio":              0,
			"slave_avg_price":        0,
			"master_avg_price":       0,
			"master_dollar_finished": 0,
			"slave_dollar_finished":  0,
			"swapordernums":          0,     //# master总订单个数
			"spotordernums":          0,     // # slave 总订单个数
			"side":                   "buy", // # sell buy
		},
	}
	return msg
}

// OnOrderBook 盘口信息

func (mgr *OrderManager) OnOrderBook(book *OrderBookEvent) {
	mgr.chOrderBook <- book
}

func (mgr *OrderManager) onOrderBook(book *OrderBookEvent) {
	mgr.networkOk = true
	defer mgr.mtx.Unlock()
	mgr.mtx.Lock()
	for _, task := range mgr.tasks {
		if !task.Request.NameIn(book.Underlying.Name) {
			continue
		}
		go func(task *OrderTask) {
			defer func() {
				if recover() != nil {
					mgr.GetLogger().Println("onOrderBook recover: task.chOrderBook <- book ")
				}
			}()
			task.chOrderBook <- book
		}(task)
	}
	//}()
}

// OnOrderReturn 成交回报
func (mgr *OrderManager) OnOrderReturn(order *OrderEvent) {
	mgr.networkOk = true
	mgr.chOrder <- order
}

func (mgr *OrderManager) onOrderReturn(order *OrderEvent) {

	defer mgr.mtx.Unlock()
	mgr.mtx.Lock()
	//go func() {
	for _, task := range mgr.tasks {
		if !task.Request.NameIn(order.Underlying.Name) {
			continue
		}
		go func(task *OrderTask) {
			defer func() {
				if recover() != nil {
					mgr.GetLogger().Println("onOrderReturn recover: task.chOrder <- order ")
				}
			}()

			//task.Manager.GetLogger().Println("--- ordermgr----------onOrderEvent---- task.chOrder <- order ----------")
			//if data, e := json.Marshal(order.Inner.Orders[0]); e == nil {
			//	log.Println(string(data))
			//}
			task.chOrder <- order
		}(task)
	}
	//}()
}
