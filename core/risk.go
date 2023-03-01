package core

import (
	"context"
)

type RiskManagerVars struct {
}

type RiskManager struct {
	*CommBase

	chSignal chan *Signal
	broker   *Broker
	ordermgr *OrderManager
}

func (rm *RiskManager) SetOrderManager(mgr *OrderManager) {
	rm.ordermgr = mgr
}

func (rm *RiskManager) OnOrderBook(book *OrderBookEvent) {
	//TODO implement me
	//panic("implement me")
}

func (rm *RiskManager) OnOrderReturn(order *OrderEvent) {
	//TODO implement me
	//panic("implement me")
}

func (rm *RiskManager) OnAccount(acc *AccountEvent) {
	//TODO implement me
	//panic("implement me")
}

func (rm *RiskManager) OnPosition(pos *PositionEvent) {
	//TODO implement me
	//panic("implement me")
}

func (rm *RiskManager) OnBalancePosition(balpos *BalanceAndPositionEvent) {
	//TODO implement me
	//panic("implement me")
}

func (rm *RiskManager) OnBrokerEvent(ev *BrokerEvent) {
	//TODO implement me
	//panic("implement me")
}

func (rm *RiskManager) Init(config *RiskManagerVars) {

}

func (rm *RiskManager) Run(ctx context.Context) {
	for {
		select {
		case sig := <-rm.chSignal:
			rm.onSignal(sig)
		case <-ctx.Done():
			break
		}
	}
}

func (rm *RiskManager) onSignal(sig *Signal) {

}

func (rm *RiskManager) OnSignal(signal *Signal) {
	if rm.chSignal == nil {
		rm.chSignal = make(chan *Signal)
	}

	go func() {
		rm.chSignal <- signal
	}()
}
