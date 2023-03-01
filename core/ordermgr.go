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
}

func (mgr *OrderManager) Init(config *OrderManagerConfigVars) bool {
	mgr.config = config
	//mgr.config.CheckOrderReturnTimer = int64(math.Max(5., float64(mgr.config.CheckOrderReturnTimer)))

	mgr.tasks = make(map[string]*OrderTask)
	mgr.chTaskEvent = make(chan *OrderTaskEvent)
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
		Request:            req,
		Manager:            mgr,
		spot:               spot,
		swap:               swap,
		Create:             time.Now(),
		Start:              time.Now(),
		chOrder:            make(chan *OrderEvent),
		chOrderBook:        make(chan *OrderBookEvent),
		chTask:             make(chan *OrderTaskEvent),
		chOrderSlice:       make(chan *OrderSlice),
		PendingPriceOffset: mgr.config.OrderPriceOffset,
	}
	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()
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
		}
	}
}

func (mgr *OrderManager) StopTask(task *OrderTask) {
	task.Stop()
}

func (mgr *OrderManager) StopTaskByPair(spot, swap string) {
	defer mgr.mtx.Unlock()
	mgr.mtx.Lock()
	for _, task := range mgr.tasks {
		if task.Request.Spot.Name == spot && task.Request.Swap.Name == swap {
			task.Stop()
			break
		}
	}
}

func (mgr *OrderManager) GetTaskByPair(spot, swap string) *OrderTask {
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
	defer mgr.mtx.Unlock()
	mgr.mtx.Lock()
	task := event.Task
	if event.Reason == "task_stopped" {
		if _, ok := mgr.tasks[task.Id()]; ok {
			delete(mgr.tasks, task.Id())
		}
		// 发送报单统计信息
	}
}

// OnOrderBook 盘口信息
func (mgr *OrderManager) OnOrderBook(book *OrderBookEvent) {
	go func() {
		for _, task := range mgr.tasks {
			if !task.Request.NameIn(book.Underlying.Name) {
				continue
			}
			task.chOrderBook <- book
		}
	}()
}

// OnOrderReturn 成交回报
func (mgr *OrderManager) OnOrderReturn(order *OrderEvent) {
	go func() {
		for _, task := range mgr.tasks {
			if !task.Request.NameIn(order.Underlying.Name) {
				continue
			}
			task.chOrder <- order
		}
	}()
}
